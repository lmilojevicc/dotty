package dotty

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// TransactionOutcome describes a producer's claim, not evidence gathered by this
// type. In particular, legacy Tx callbacks do not establish verified restoration.
// The zero value is invalid. A pre-mutation refusal is not a transaction outcome.
type TransactionOutcome string

const (
	OutcomeSuccess                   TransactionOutcome = "success"
	OutcomeVerifiedRolledBackFailure TransactionOutcome = "verified-rolled-back-failure"
	OutcomeUncertainty               TransactionOutcome = "uncertainty"
	OutcomeCommittedCleanupWarning   TransactionOutcome = "committed-cleanup-warning"
)

// OutcomeDiagnostic retains exact path identities. Producers must supply every
// relevant path and safe recovery guidance; validation checks structure only,
// not filesystem state, completeness, ownership, or the truth of a claim.
type OutcomeDiagnostic struct {
	Summary          string
	ObservedState    string
	RecoveryGuidance string
	AffectedPaths    []string
	UncertainPaths   []string
	StagedPaths      []string
	RetainedPaths    []string
	NoRetry          bool
}

// String quotes all producer-supplied strings, including causes elsewhere in
// this file. Sorting copies of path lists does not clean or rewrite identities.
func (d OutcomeDiagnostic) String() string {
	return fmt.Sprintf(
		"summary=%q observed=%q affected=%q uncertain=%q staged=%q retained=%q recovery=%q no-retry=%t",
		d.Summary,
		d.ObservedState,
		sortedOutcomePaths(d.AffectedPaths),
		sortedOutcomePaths(d.UncertainPaths),
		sortedOutcomePaths(d.StagedPaths),
		sortedOutcomePaths(d.RetainedPaths),
		d.RecoveryGuidance,
		d.NoRetry,
	)
}

func sortedOutcomePaths(paths []string) []string {
	ordered := slices.Clone(paths)
	slices.Sort(ordered)
	return ordered
}

func (d OutcomeDiagnostic) validate() error {
	if strings.TrimSpace(d.Summary) == "" || strings.TrimSpace(d.ObservedState) == "" ||
		strings.TrimSpace(d.RecoveryGuidance) == "" {
		return errors.New("summary, observed state, and recovery guidance are required")
	}
	for _, paths := range [][]string{d.AffectedPaths, d.UncertainPaths, d.StagedPaths, d.RetainedPaths} {
		for _, path := range paths {
			if path == "" {
				return errors.New("diagnostic paths must not be empty")
			}
		}
	}
	return nil
}

func (d OutcomeDiagnostic) hasRecoveryArtifacts() bool {
	return len(d.UncertainPaths) != 0 || len(d.StagedPaths) != 0 || len(d.RetainedPaths) != 0 ||
		d.NoRetry
}

// OutcomeReport is data, including normal success. Validate never verifies a
// rollback; only a future identity-aware producer can justify that claim.
type OutcomeReport struct {
	Outcome    TransactionOutcome
	Diagnostic OutcomeDiagnostic
}

func (r OutcomeReport) Validate() error {
	d := r.Diagnostic
	switch r.Outcome {
	case OutcomeSuccess:
		if d.Summary != "" || d.ObservedState != "" || d.RecoveryGuidance != "" ||
			len(d.AffectedPaths) != 0 ||
			d.hasRecoveryArtifacts() {
			return errors.New("success must not carry failure or recovery diagnostics")
		}
		return nil
	case OutcomeVerifiedRolledBackFailure, OutcomeUncertainty, OutcomeCommittedCleanupWarning:
	default:
		return fmt.Errorf("invalid transaction outcome %q", r.Outcome)
	}
	if err := d.validate(); err != nil {
		return err
	}
	switch r.Outcome {
	case OutcomeVerifiedRolledBackFailure:
		if len(d.AffectedPaths) == 0 || d.hasRecoveryArtifacts() {
			return errors.New(
				"verified rollback requires affected paths and no unresolved recovery artifacts",
			)
		}
	case OutcomeUncertainty:
		if len(d.UncertainPaths)+len(d.StagedPaths) == 0 || len(d.RetainedPaths) != 0 || d.NoRetry {
			return errors.New(
				"uncertainty requires uncertain or staged paths, not committed cleanup fields",
			)
		}
	case OutcomeCommittedCleanupWarning:
		if len(d.RetainedPaths) == 0 || !d.NoRetry ||
			len(d.UncertainPaths)+len(d.StagedPaths) != 0 {
			return errors.New(
				"committed cleanup warning requires retained paths and no-retry, without rollback uncertainty",
			)
		}
	}
	return nil
}

// OutcomeError transports a non-success report without losing the original
// cause. It does not turn an ordinary callback error into a verified outcome.
type OutcomeError struct {
	Report OutcomeReport
	Cause  error
}

func (e *OutcomeError) Validate() error {
	if e == nil {
		return errors.New("nil outcome error")
	}
	if err := e.Report.Validate(); err != nil {
		return err
	}
	if e.Report.Outcome == OutcomeSuccess {
		return errors.New("success is data, not an execution error")
	}
	return nil
}

func (e *OutcomeError) Error() string {
	if e == nil {
		return "nil outcome error"
	}
	message := fmt.Sprintf("outcome report %q: %s", e.Report.Outcome, e.Report.Diagnostic.String())
	if e.Cause != nil {
		message += fmt.Sprintf(" cause=%q", outcomeCauseText(e.Cause))
	}
	return message
}

func (e *OutcomeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// RefusalError is a producer's explicit claim that refusal preceded mutation.
// It must never be inferred from an empty legacy rollback callback list.
type RefusalError struct {
	Diagnostic OutcomeDiagnostic
	Cause      error
}

func (e *RefusalError) Validate() error {
	if e == nil {
		return errors.New("nil refusal error")
	}
	if err := e.Diagnostic.validate(); err != nil {
		return err
	}
	if len(e.Diagnostic.AffectedPaths) == 0 || e.Diagnostic.hasRecoveryArtifacts() {
		return errors.New("pre-mutation refusal requires affected paths and no recovery artifacts")
	}
	return nil
}

func (e *RefusalError) Error() string {
	if e == nil {
		return "nil refusal error"
	}
	message := "refusal report: " + e.Diagnostic.String()
	if e.Cause != nil {
		message += fmt.Sprintf(" cause=%q", outcomeCauseText(e.Cause))
	}
	return message
}

func (e *RefusalError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Bound observable cause graphs before invoking Error. Nested reports are
// serialized as data rather than recursively quoting other OutcomeError or
// RefusalError messages. Unwrap remains unchanged for ordinary errors.Is/As.
// This does not attempt to police arbitrary Error or Unwrap implementations.
func outcomeCauseText(cause error) string {
	remaining := 256
	safe := true
	var claims []string
	var inspect func(error, int)
	inspect = func(err error, depth int) {
		if remaining == 0 {
			safe = false
			return
		}
		remaining--
		if err == nil || depth >= 64 {
			safe = false
			return
		}
		value := reflect.ValueOf(err)
		switch value.Kind() {
		case reflect.Chan,
			reflect.Func,
			reflect.Interface,
			reflect.Map,
			reflect.Pointer,
			reflect.Slice:
			if value.IsNil() {
				safe = false
				return
			}
		}
		switch report := err.(type) { //nolint:errorlint // Inspect exact nodes without recursively searching causes.
		case *OutcomeError:
			safe = false
			claims = append(
				claims,
				fmt.Sprintf(
					"outcome=%q %s",
					report.Report.Outcome,
					report.Report.Diagnostic.String(),
				),
			)
		case *RefusalError:
			safe = false
			claims = append(claims, "refusal "+report.Diagnostic.String())
		}
		switch wrapped := err.(type) { //nolint:errorlint // Bound each observable edge, not errors.As traversal.
		case interface{ Unwrap() []error }:
			for _, child := range wrapped.Unwrap() {
				if remaining == 0 {
					safe = false
					break
				}
				inspect(child, depth+1)
			}
		case interface{ Unwrap() error }:
			if child := wrapped.Unwrap(); child != nil {
				inspect(child, depth+1)
			}
		}
	}
	inspect(cause, 0)
	if safe {
		return cause.Error()
	}
	message := "cause error graph omitted: nested report, nil node, or inspection limit"
	for _, claim := range claims {
		message += "; unvalidated report: " + claim
	}
	return message
}
