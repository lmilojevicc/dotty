package dotty

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func outcomeDiagnosticFixture() OutcomeDiagnostic {
	return OutcomeDiagnostic{
		Summary:          "operation stopped",
		ObservedState:    "target changed",
		RecoveryGuidance: "inspect the listed paths before proceeding",
		AffectedPaths:    []string{"/target"},
	}
}

func TestTransactionOutcomeVocabulary(t *testing.T) {
	for _, outcome := range []TransactionOutcome{
		OutcomeSuccess, OutcomeVerifiedRolledBackFailure, OutcomeUncertainty, OutcomeCommittedCleanupWarning,
	} {
		diagnostic := outcomeDiagnosticFixture()
		switch outcome {
		case OutcomeSuccess:
			diagnostic = OutcomeDiagnostic{}
		case OutcomeUncertainty:
			diagnostic.UncertainPaths = []string{"/target"}
			diagnostic.StagedPaths = []string{"/stage"}
		case OutcomeCommittedCleanupWarning:
			diagnostic.RetainedPaths = []string{"/backup"}
			diagnostic.NoRetry = true
		}
		t.Run(string(outcome), func(t *testing.T) {
			report := OutcomeReport{Outcome: outcome, Diagnostic: diagnostic}
			if err := report.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOutcomeReportsRejectMalformedClaims(t *testing.T) {
	valid := outcomeDiagnosticFixture()
	cases := map[string]OutcomeReport{
		"zero":                        {},
		"unknown":                     {Outcome: TransactionOutcome("future")},
		"success with diagnostic":     {Outcome: OutcomeSuccess, Diagnostic: valid},
		"rollback missing diagnostic": {Outcome: OutcomeVerifiedRolledBackFailure},
		"uncertainty missing paths":   {Outcome: OutcomeUncertainty, Diagnostic: valid},
		"warning missing retained paths": {
			Outcome:    OutcomeCommittedCleanupWarning,
			Diagnostic: valid,
		},
	}
	for _, field := range []string{"summary", "observed", "recovery", "affected", "empty path", "staged rollback", "warning retry"} {
		d := outcomeDiagnosticFixture()
		outcome := OutcomeVerifiedRolledBackFailure
		switch field {
		case "summary":
			d.Summary = " "
		case "observed":
			d.ObservedState = ""
		case "recovery":
			d.RecoveryGuidance = ""
		case "affected":
			d.AffectedPaths = nil
		case "empty path":
			d.AffectedPaths = []string{""}
		case "staged rollback":
			d.StagedPaths = []string{"/stage"}
		case "warning retry":
			outcome = OutcomeCommittedCleanupWarning
			d.RetainedPaths = []string{"/backup"}
		}
		cases[field] = OutcomeReport{Outcome: outcome, Diagnostic: d}
	}
	for name, report := range cases {
		t.Run(name, func(t *testing.T) {
			if report.Validate() == nil {
				t.Fatal("malformed report accepted")
			}
		})
	}
	if (&OutcomeError{Report: OutcomeReport{Outcome: OutcomeSuccess}}).Validate() == nil {
		t.Fatal("success must be data, not an error")
	}
	var missing *OutcomeError
	if missing.Validate() == nil {
		t.Fatal("nil report accepted")
	}
}

func TestRefusalIsSeparateFromTransactionOutcomes(t *testing.T) {
	refusal := &RefusalError{Diagnostic: outcomeDiagnosticFixture()}
	if err := refusal.Validate(); err != nil {
		t.Fatal(err)
	}
	var outcome *OutcomeError
	if errors.As(refusal, &outcome) {
		t.Fatal("refusal is a transaction outcome")
	}
	refusal.Diagnostic.StagedPaths = []string{"/stage"}
	if refusal.Validate() == nil {
		t.Fatal("post-mutation refusal accepted")
	}
	if (&RefusalError{}).Validate() == nil {
		t.Fatal("zero refusal accepted")
	}
	var missing *RefusalError
	if missing.Validate() == nil {
		t.Fatal("nil refusal accepted")
	}
}

func TestOutcomeErrorsRetainWrappedCauses(t *testing.T) {
	cause := &fixtureOutcomeCause{}
	for _, err := range []error{
		&OutcomeError{Report: OutcomeReport{Outcome: OutcomeVerifiedRolledBackFailure, Diagnostic: outcomeDiagnosticFixture()}, Cause: cause},
		&RefusalError{Diagnostic: outcomeDiagnosticFixture(), Cause: cause},
	} {
		wrapped := fmt.Errorf("command: %w", err)
		if !errors.Is(wrapped, cause) {
			t.Fatal("cause lost")
		}
		var got *fixtureOutcomeCause
		if !errors.As(wrapped, &got) || got != cause {
			t.Fatal("typed cause lost")
		}
	}
}

type fixtureOutcomeCause struct{}

func (*fixtureOutcomeCause) Error() string { return "cause" }

func TestOutcomeDiagnosticSerializationQuotesWithoutChangingPaths(t *testing.T) {
	paths := []string{"/z", "/a/../b", "/a\n\x1b\"\\", "/a", "/é", "/\xff", " "}
	before := append([]string(nil), paths...)
	d := outcomeDiagnosticFixture()
	d.AffectedPaths = paths
	d.Summary = "summary\n\x1b"
	d.ObservedState = "state\t"
	d.RecoveryGuidance = "inspect\r"
	err := &OutcomeError{
		Report: OutcomeReport{Outcome: OutcomeVerifiedRolledBackFailure, Diagnostic: d},
		Cause:  errors.New("cause\a"),
	}
	got := err.Error()
	for _, path := range paths {
		if !strings.Contains(got, fmt.Sprintf("%q", path)) {
			t.Fatalf("missing quoted path %q in %s", path, got)
		}
	}
	if strings.ContainsAny(got, "\n\r\t\x1b\a") {
		t.Fatalf("unsafe serialized text: %q", got)
	}
	if !reflect.DeepEqual(paths, before) {
		t.Fatal("path identity or input order changed")
	}
	if strings.Index(got, `"/a"`) > strings.Index(got, `"/z"`) {
		t.Fatal("paths not sorted")
	}
}

type recursiveOutcomeCause struct {
	cause error
}

func (e *recursiveOutcomeCause) Error() string { return e.cause.Error() }
func (e *recursiveOutcomeCause) Unwrap() error { return e.cause }

type nonComparableOutcomeCause []error

func (nonComparableOutcomeCause) Error() string     { return "bundle" }
func (e nonComparableOutcomeCause) Unwrap() []error { return e }

func TestOutcomeErrorsBoundCauseRendering(t *testing.T) {
	d := outcomeDiagnosticFixture()
	self := &OutcomeError{Report: OutcomeReport{Outcome: OutcomeUncertainty, Diagnostic: d}}
	self.Cause = self
	refusal := &RefusalError{Diagnostic: d}
	refusal.Cause = refusal
	cycle := &recursiveOutcomeCause{}
	cycle.cause = cycle
	first, second := &recursiveOutcomeCause{}, &recursiveOutcomeCause{}
	first.cause, second.cause = second, first
	nonComparable := make(nonComparableOutcomeCause, 1)
	nonComparable[0] = nonComparable
	deep := errors.New("leaf")
	for range 10000 {
		deep = &recursiveOutcomeCause{cause: deep}
	}
	var nilWrapper *recursiveOutcomeCause
	for name, cause := range map[string]error{
		"typed self":          self,
		"refusal self":        refusal,
		"wrapper self":        cycle,
		"two node cycle":      first,
		"noncomparable cycle": nonComparable,
		"deep":                deep,
		"typed nil":           nilWrapper,
		"nil child":           nonComparableOutcomeCause{errors.New("leaf"), nil},
	} {
		t.Run(name, func(t *testing.T) {
			for _, err := range []error{
				&OutcomeError{Report: self.Report, Cause: cause},
				&RefusalError{Diagnostic: d, Cause: cause},
			} {
				got := err.Error()
				if !strings.Contains(got, "omitted") || !strings.Contains(got, `"/target"`) {
					t.Fatalf("missing bounded cause notice or path: %q", got)
				}
			}
		})
	}
}

func TestRunAtomicDoesNotProduceOutcomeClaims(t *testing.T) {
	stop := errors.New("stop")
	rollback := errors.New("rollback")
	cleanup := errors.New("cleanup")
	for _, name := range []string{"success", "no callbacks", "rollback nil", "rollback failure", "cleanup failure"} {
		t.Run(name, func(t *testing.T) {
			mutated := false
			err := RunAtomic(func(tx *Tx) error {
				// A callback's mutation is deliberately not inferred from registered callbacks.
				mutated = true
				switch name {
				case "success":
					return nil
				case "rollback nil":
					tx.AddRollback(func() error { return nil })
				case "rollback failure":
					tx.AddRollback(func() error { return rollback })
				case "cleanup failure":
					tx.AddCleanup(func() error { return cleanup })
					return nil
				}
				return stop
			})
			if !mutated {
				t.Fatal("callback not called")
			}
			var outcome *OutcomeError
			var refusal *RefusalError
			if errors.As(err, &outcome) || errors.As(err, &refusal) {
				t.Fatal("legacy callback classified as evidence")
			}
			switch name {
			case "success":
				if err != nil {
					t.Fatal(err)
				}
			case "cleanup failure":
				if !errors.Is(err, cleanup) {
					t.Fatal("cleanup cause lost")
				}
			default:
				if !errors.Is(err, stop) {
					t.Fatal("operation cause lost")
				}
			}
			if name == "rollback failure" && !errors.Is(err, rollback) {
				t.Fatal("rollback cause lost")
			}
		})
	}
}
