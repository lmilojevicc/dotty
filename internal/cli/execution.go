package cli

import (
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"

	"github.com/lmilojevicc/dotty/internal/dotty"
)

// RenderExecutionError is the final stderr/exit mapping. Legacy errors retain
// their exact rendering and failure exit; only explicit, structurally valid
// producer reports permit C4 state claims. No filesystem verification occurs here.
func RenderExecutionError(out io.Writer, err error) int {
	if err == nil {
		return 0
	}
	reports := executionReports{remaining: 256}
	reports.inspect(err, false, 0)
	if reports.count == 0 && !reports.invalid {
		renderLegacyError(out, err)
		return 1
	}
	if reports.count != 1 || reports.mixed || reports.invalid {
		fmt.Fprintln(
			out,
			"error: Invalid or conflicting execution report. Transaction state cannot be established from this report.",
		)
		// Wrappers need not include descendants in their messages, and their
		// Error methods may recurse through an invalid graph. Only independently
		// collected fields and non-nil terminal causes are rendered here.
		for _, claim := range reports.claims {
			fmt.Fprintf(out, "  unvalidated report: %s\n", claim)
		}
		for _, cause := range reports.causes {
			fmt.Fprintf(out, "  reported cause: %q\n", cause.Error())
		}
		if reports.omitted {
			fmt.Fprintln(
				out,
				"  error graph omitted in part: nil node or inspection limit; additional diagnostics may be unavailable.",
			)
		}
		fmt.Fprintln(
			out,
			"  recovery: Inspect the supplied paths and resolve the report before retrying; do not assume restoration or commit.",
		)
		return 1
	}

	var message strings.Builder
	code := 1
	if reports.refusal != nil {
		fmt.Fprintln(&message, "error: Refused before mutation. No Dotty changes were made.")
		renderOutcomeDiagnostic(&message, reports.refusal.Diagnostic, reports.refusal.Cause)
	} else {
		r := reports.outcome
		switch r.Report.Outcome {
		case dotty.OutcomeVerifiedRolledBackFailure:
			fmt.Fprintln(
				&message,
				"error: Requested state did not commit. Dotty's changes were reversed and restoration was verified, preserving external edits.",
			)
		case dotty.OutcomeUncertainty:
			fmt.Fprintln(
				&message,
				"error: Restoration could not be proven. Transaction state is uncertain.",
			)
		case dotty.OutcomeCommittedCleanupWarning:
			fmt.Fprintln(
				&message,
				"warning: Requested changes committed; cleanup is incomplete. DO NOT RETRY the operation.",
			)
			code = 0
		}
		renderOutcomeDiagnostic(&message, r.Report.Diagnostic, r.Cause)
	}
	_, _ = io.WriteString(out, message.String())
	return code
}

// Count reports across the graph, rather than using the first errors.As match:
// a warning joined with an unrelated failure cannot authorize a successful exit.
// Single wrappers (including a one-child Join) are transparent. Ordinary causes
// inside a report may be joined; a nested report is always a conflicting claim.
type executionReports struct {
	count     int
	mixed     bool
	invalid   bool
	omitted   bool
	remaining int
	claims    []string
	causes    []error
	outcome   *dotty.OutcomeError
	refusal   *dotty.RefusalError
}

// A depth and total-node budget bound both cycles and non-comparable errors
// without map keys or global deduplication that could hide a sibling failure.
// This inspects observable Unwrap structure, not arbitrary method behavior.
func (r *executionReports) inspect(err error, inCause bool, depth int) {
	if r.remaining == 0 {
		r.invalid, r.omitted = true, true
		return
	}
	r.remaining--
	if err == nil || depth >= 64 {
		r.invalid, r.omitted = true, true
		return
	}
	value := reflect.ValueOf(err)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			r.invalid, r.omitted = true, true
			return
		}
	}
	// Exact node matching is deliberate: errors.As would hide sibling reports.
	isReport := false
	switch report := err.(type) { //nolint:errorlint // Inspect each node, not the first matching descendant.
	case *dotty.OutcomeError:
		isReport = true
		r.count++
		r.outcome = report
		r.claims = append(
			r.claims,
			fmt.Sprintf("outcome=%q %s", report.Report.Outcome, report.Report.Diagnostic.String()),
		)
		if report.Validate() != nil {
			r.invalid = true
		}
		inCause = true
	case *dotty.RefusalError:
		isReport = true
		r.count++
		r.refusal = report
		r.claims = append(r.claims, "refusal "+report.Diagnostic.String())
		if report.Validate() != nil {
			r.invalid = true
		}
		inCause = true
	}
	terminal := true
	switch wrapped := err.(type) { //nolint:errorlint // Unwrap only this node; descendant matching changes graph semantics.
	case interface{ Unwrap() []error }:
		children := wrapped.Unwrap()
		terminal = len(children) == 0
		if len(children) > 1 && !inCause {
			r.mixed = true
		}
		for _, child := range children {
			if r.remaining == 0 {
				r.invalid, r.omitted = true, true
				break
			}
			r.inspect(child, inCause, depth+1)
		}
	case interface{ Unwrap() error }:
		if child := wrapped.Unwrap(); child != nil {
			terminal = false
			r.inspect(child, inCause, depth+1)
		}
	}
	if terminal && !isReport {
		r.causes = append(r.causes, err)
	}
}

func renderOutcomeDiagnostic(out io.Writer, d dotty.OutcomeDiagnostic, cause error) {
	fmt.Fprintf(out, "  summary: %q\n  observed state: %q\n", d.Summary, d.ObservedState)
	for _, group := range []struct {
		label string
		paths []string
	}{
		{"affected", d.AffectedPaths},
		{"uncertain", d.UncertainPaths},
		{"staged", d.StagedPaths},
		{"retained", d.RetainedPaths},
	} {
		paths := slices.Clone(group.paths)
		slices.Sort(paths)
		for _, path := range paths {
			fmt.Fprintf(out, "  %s path: %q\n", group.label, path)
		}
	}
	fmt.Fprintf(out, "  recovery: %q\n", d.RecoveryGuidance)
	if cause != nil {
		fmt.Fprintf(out, "  cause: %q\n", cause.Error())
	}
}
