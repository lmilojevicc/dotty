package cli

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/lmilojevicc/dotty/internal/dotty"
)

func executionDiagnosticFixture() dotty.OutcomeDiagnostic {
	return dotty.OutcomeDiagnostic{
		Summary:          "operation stopped (use --force)",
		ObservedState:    "target changed",
		RecoveryGuidance: "inspect listed paths",
		AffectedPaths:    []string{"/z", "/a\n\x1b\"\\", "/a/../b"},
	}
}

const executionDetailsGolden = "  summary: \"operation stopped (use --force)\"\n" +
	"  observed state: \"target changed\"\n" +
	"  affected path: \"/a\\n\\x1b\\\"\\\\\"\n" +
	"  affected path: \"/a/../b\"\n" +
	"  affected path: \"/z\"\n"

func TestRenderExecutionErrorOutcomes(t *testing.T) {
	d := executionDiagnosticFixture()
	uncertain := d
	uncertain.UncertainPaths = []string{"/target-z", "/target-a"}
	uncertain.StagedPaths = []string{"/stage-z", "/stage-a"}
	warning := d
	warning.RetainedPaths = []string{"/backup-z", "/backup-a"}
	warning.NoRetry = true
	for _, tt := range []struct {
		name string
		err  error
		code int
		want string
	}{
		{"success", nil, 0, ""},
		{"ordinary", errors.New("operation failed"), 1, "error: operation failed\n"},
		{
			"refusal", &dotty.RefusalError{Diagnostic: d}, 1,
			"error: Refused before mutation. No Dotty changes were made.\n" + executionDetailsGolden +
				"  recovery: \"inspect listed paths\"\n",
		},
		{
			"rollback", &dotty.OutcomeError{Report: dotty.OutcomeReport{Outcome: dotty.OutcomeVerifiedRolledBackFailure, Diagnostic: d}}, 1,
			"error: Requested state did not commit. Dotty's changes were reversed and restoration was verified, preserving external edits.\n" + executionDetailsGolden +
				"  recovery: \"inspect listed paths\"\n",
		},
		{
			"uncertainty", &dotty.OutcomeError{Report: dotty.OutcomeReport{Outcome: dotty.OutcomeUncertainty, Diagnostic: uncertain}}, 1,
			"error: Restoration could not be proven. Transaction state is uncertain.\n" + executionDetailsGolden +
				"  uncertain path: \"/target-a\"\n  uncertain path: \"/target-z\"\n" +
				"  staged path: \"/stage-a\"\n  staged path: \"/stage-z\"\n" +
				"  recovery: \"inspect listed paths\"\n",
		},
		{
			"warning", &dotty.OutcomeError{Report: dotty.OutcomeReport{Outcome: dotty.OutcomeCommittedCleanupWarning, Diagnostic: warning}}, 0,
			"warning: Requested changes committed; cleanup is incomplete. DO NOT RETRY the operation.\n" + executionDetailsGolden +
				"  retained path: \"/backup-a\"\n  retained path: \"/backup-z\"\n" +
				"  recovery: \"inspect listed paths\"\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if got := RenderExecutionError(&stderr, tt.err); got != tt.code {
				t.Fatalf("exit: %d, want %d", got, tt.code)
			}
			if got := stripANSI(stderr.String()); got != tt.want {
				t.Fatalf("stderr:\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestRenderExecutionErrorLegacyGoldens(t *testing.T) {
	for _, tt := range []struct{ message, want string }{
		{"usage: dotty add <path> <package>", "error: invalid arguments\n  usage: dotty add <path> <package>\n"},
		{"unknown package \"tmux\" (run `dotty list` to see packages)", "error: unknown package \"tmux\"\nhint: run `dotty list` to see packages\n"},
		{"manifest missing; run `dotty init /repo`", "error: manifest missing\nhint: run `dotty init /repo`\n"},
		{"operation failed (rollback failed: restore failed)", "error: operation failed (rollback failed: restore failed)\n"},
		{"operation failed (no changes made)", "error: operation failed\nhint: no changes made\n"},
	} {
		t.Run(tt.message, func(t *testing.T) {
			var out bytes.Buffer
			if RenderExecutionError(&out, errors.New(tt.message)) != 1 {
				t.Fatal("untyped error succeeded")
			}
			if got := stripANSI(out.String()); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderExecutionErrorWrappedWarningAndCause(t *testing.T) {
	d := executionDiagnosticFixture()
	d.RetainedPaths, d.NoRetry = []string{"/backup"}, true
	cause := errors.New("cleanup\nfailed (use --force)")
	warning := &dotty.OutcomeError{
		Report: dotty.OutcomeReport{Outcome: dotty.OutcomeCommittedCleanupWarning, Diagnostic: d},
		Cause:  cause,
	}
	for _, err := range []error{warning, fmt.Errorf("command\ncontext: %w", warning), errors.Join(warning)} {
		var out bytes.Buffer
		if RenderExecutionError(&out, err) != 0 {
			t.Fatal("transparent wrapper lost warning")
		}
		if !errors.Is(err, cause) {
			t.Fatal("cause lost")
		}
		if !strings.Contains(out.String(), "  cause: \"cleanup\\nfailed (use --force)\"\n") {
			t.Fatalf("cause not safely rendered: %q", out.String())
		}
		if strings.Contains(out.String(), "hint:") {
			t.Fatal("typed report entered legacy hint parser")
		}
		var compatibility bytes.Buffer
		RenderError(&compatibility, err)
		if compatibility.String() != out.String() {
			t.Fatal("rendering-only API bypassed typed transport")
		}
	}
}

// Error deliberately hides descendants: invalid rendering must inspect fields,
// not rely on a wrapper's message to preserve recovery evidence.
type executionGraphWrapper struct {
	children []error
}

func (*executionGraphWrapper) Error() string     { return "bundle" }
func (e *executionGraphWrapper) Unwrap() []error { return e.children }

type executionTerminalWrapper struct{}

func (*executionTerminalWrapper) Error() string { return "bundle" }
func (*executionTerminalWrapper) Unwrap() error { return nil }

func TestRenderExecutionErrorTerminalWrappersRetainOrdinaryCauses(t *testing.T) {
	d := dotty.OutcomeDiagnostic{
		Summary:          "cleanup",
		ObservedState:    "backup retained",
		RecoveryGuidance: "inspect backup",
		AffectedPaths:    []string{"/target"},
		RetainedPaths:    []string{"/backup"},
		NoRetry:          true,
	}
	warning := &dotty.OutcomeError{Report: dotty.OutcomeReport{
		Outcome: dotty.OutcomeCommittedCleanupWarning, Diagnostic: d,
	}}
	want := "error: Invalid or conflicting execution report. Transaction state cannot be established from this report.\n" +
		"  unvalidated report: outcome=\"committed-cleanup-warning\" summary=\"cleanup\" observed=\"backup retained\" affected=[\"/target\"] uncertain=[] staged=[] retained=[\"/backup\"] recovery=\"inspect backup\" no-retry=true\n" +
		"  reported cause: \"bundle\"\n" +
		"  recovery: Inspect the supplied paths and resolve the report before retrying; do not assume restoration or commit.\n"
	for name, terminal := range map[string]error{
		"single nil":  &executionTerminalWrapper{},
		"empty multi": &executionGraphWrapper{},
	} {
		t.Run(name, func(t *testing.T) {
			var standalone bytes.Buffer
			if code := RenderExecutionError(&standalone, terminal); code != 1 {
				t.Fatalf("standalone exit: %d, want 1", code)
			}
			if got := stripANSI(standalone.String()); got != "error: bundle\n" {
				t.Fatalf("standalone rendering: %q", got)
			}
			for _, joined := range []error{errors.Join(warning, terminal), errors.Join(terminal, warning)} {
				var out bytes.Buffer
				if code := RenderExecutionError(&out, joined); code != 1 {
					t.Fatalf("mixed warning exit: %d, want 1", code)
				}
				if got := out.String(); got != want {
					t.Fatalf("got %q, want %q", got, want)
				}
			}
		})
	}
}

type recursiveExecutionWrapper struct {
	cause error
}

func (e *recursiveExecutionWrapper) Error() string { return e.cause.Error() }
func (e *recursiveExecutionWrapper) Unwrap() error { return e.cause }

type nonComparableExecutionErrors []error

func (nonComparableExecutionErrors) Error() string     { return "noncomparable bundle" }
func (e nonComparableExecutionErrors) Unwrap() []error { return e }

func TestRenderExecutionErrorInvalidGraphsRetainIndependentEvidence(t *testing.T) {
	d := executionDiagnosticFixture()
	d.RetainedPaths, d.NoRetry = []string{"/backup\n\x1b", "/a/../backup"}, true
	warning := &dotty.OutcomeError{
		Report: dotty.OutcomeReport{Outcome: dotty.OutcomeCommittedCleanupWarning, Diagnostic: d},
	}
	sentinel := errors.New("independent\nfailure")
	self := &dotty.OutcomeError{Report: warning.Report}
	self.Cause = self
	cycle := &recursiveExecutionWrapper{}
	cycle.cause = cycle
	first, second := &recursiveExecutionWrapper{}, &recursiveExecutionWrapper{}
	first.cause, second.cause = second, first
	safeCycle := &executionGraphWrapper{}
	safeCycle.children = []error{safeCycle}
	nonComparableCycle := make(nonComparableExecutionErrors, 1)
	nonComparableCycle[0] = nonComparableCycle
	var deep error = warning
	for range 10000 {
		deep = &executionGraphWrapper{children: []error{deep}}
	}
	wide := make(nonComparableExecutionErrors, 1000)
	for i := range wide {
		wide[i] = sentinel
	}
	sharedCauseWarning := &dotty.OutcomeError{Report: warning.Report, Cause: sentinel}
	malformed := &dotty.OutcomeError{Report: warning.Report}
	malformed.Report.Diagnostic.NoRetry = false
	var nilWrapper *executionGraphWrapper
	for name, graph := range map[string]error{
		"typed self cause":       self,
		"recursive wrapper":      cycle,
		"two node cycle":         first,
		"safe message cycle":     safeCycle,
		"deep chain":             deep,
		"node budget":            &dotty.OutcomeError{Report: warning.Report, Cause: wide},
		"noncomparable cycle":    nonComparableCycle,
		"opaque mixed":           &executionGraphWrapper{children: []error{warning, sentinel}},
		"opaque malformed":       &executionGraphWrapper{children: []error{malformed}},
		"nil child":              &executionGraphWrapper{children: []error{warning, nil}},
		"nil wrapper":            nilWrapper,
		"shared unknown sibling": errors.Join(sharedCauseWarning, sentinel),
		"noncomparable mixed":    nonComparableExecutionErrors{warning, sentinel},
	} {
		t.Run(name, func(t *testing.T) {
			var direct bytes.Buffer
			if RenderExecutionError(&direct, graph) != 1 ||
				!strings.Contains(direct.String(), "Invalid or conflicting") {
				t.Fatalf("invalid graph accepted directly: %q", direct.String())
			}
			switch name {
			case "typed self cause",
				"recursive wrapper",
				"two node cycle",
				"safe message cycle",
				"deep chain",
				"node budget",
				"noncomparable cycle",
				"nil child",
				"nil wrapper":
				if !strings.Contains(direct.String(), "omitted") {
					t.Fatalf("missing incomplete-graph notice: %q", direct.String())
				}
			}
			// Keep independently reachable evidence even if another branch is unsafe.
			err := &executionGraphWrapper{children: []error{graph, warning, sentinel}}
			var out bytes.Buffer
			if RenderExecutionError(&out, err) != 1 {
				t.Fatal("invalid graph succeeded")
			}
			got := out.String()
			for _, want := range []string{
				"error: Invalid or conflicting execution report.",
				"unvalidated", fmt.Sprintf("%q", "/backup\n\x1b"),
				fmt.Sprintf("%q", "/a/../backup"), fmt.Sprintf("%q", d.RecoveryGuidance),
				fmt.Sprintf("%q", sentinel.Error()),
			} {
				if !strings.Contains(got, want) {
					t.Fatalf("missing %q in %q", want, got)
				}
			}
			for _, claim := range []string{"No Dotty changes", "restoration was verified", "Requested changes committed", "hint:"} {
				if strings.Contains(got, claim) {
					t.Fatalf("invalid graph made claim %q: %q", claim, got)
				}
			}
		})
	}
}

func TestRenderExecutionErrorNilChildCannotAuthorizeWarning(t *testing.T) {
	d := executionDiagnosticFixture()
	d.RetainedPaths, d.NoRetry = []string{"/backup"}, true
	warning := &dotty.OutcomeError{Report: dotty.OutcomeReport{
		Outcome: dotty.OutcomeCommittedCleanupWarning, Diagnostic: d,
	}}
	for name, err := range map[string]error{
		"nil child after warning":  &executionGraphWrapper{children: []error{warning, nil}},
		"nil child before warning": &executionGraphWrapper{children: []error{nil, warning}},
		"nil child inside cause": &dotty.OutcomeError{
			Report: warning.Report,
			Cause:  &executionGraphWrapper{children: []error{errors.New("cleanup failed"), nil}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if RenderExecutionError(&out, err) != 1 ||
				!strings.Contains(out.String(), "Invalid or conflicting") {
				t.Fatalf("invalid graph accepted: %q", out.String())
			}
		})
	}
	var out bytes.Buffer
	if RenderExecutionError(&out, errors.Join(warning)) != 0 {
		t.Fatal("one-child Join lost valid warning")
	}
}

func TestRenderExecutionErrorMalformedAndMixedReportsFailClosed(t *testing.T) {
	d := executionDiagnosticFixture()
	d.RetainedPaths, d.NoRetry = []string{"/backup"}, true
	warning := &dotty.OutcomeError{
		Report: dotty.OutcomeReport{Outcome: dotty.OutcomeCommittedCleanupWarning, Diagnostic: d},
	}
	refusal := &dotty.RefusalError{Diagnostic: executionDiagnosticFixture()}
	invalidWarning := *warning
	invalidWarning.Report.Diagnostic.NoRetry = false
	invalidOutcome := *warning
	invalidOutcome.Report.Outcome = dotty.TransactionOutcome("unrecognized\nvalue")
	var missing *dotty.OutcomeError
	var missingRefusal *dotty.RefusalError
	for name, err := range map[string]error{
		"zero":                     &dotty.OutcomeError{},
		"unknown outcome":          &invalidOutcome,
		"warning without no-retry": &invalidWarning,
		"nil report":               missing,
		"nil refusal":              missingRefusal,
		"success error":            &dotty.OutcomeError{Report: dotty.OutcomeReport{Outcome: dotty.OutcomeSuccess}},
		"missing recovery":         &dotty.OutcomeError{Report: dotty.OutcomeReport{Outcome: dotty.OutcomeUncertainty}},
		"warning then unknown":     errors.Join(warning, errors.New("other failure")),
		"unknown then warning":     errors.Join(errors.New("other failure"), warning),
		"conflicting":              errors.Join(warning, refusal),
		"multiple warnings":        errors.Join(warning, warning),
		"wrapped join":             fmt.Errorf("outer: %w", errors.Join(warning, errors.New("other failure"))),
		"multiwrap unknown":        fmt.Errorf("first: %w; second: %w", warning, errors.New("other failure")),
		"nested report cause":      &dotty.OutcomeError{Report: warning.Report, Cause: refusal},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if RenderExecutionError(&out, err) != 1 {
				t.Fatal("malformed or mixed report succeeded")
			}
			got := out.String()
			if !strings.HasPrefix(
				got,
				"error: Invalid or conflicting execution report. Transaction state cannot be established from this report.\n",
			) {
				t.Fatalf("not fail-closed: %q", got)
			}
			for _, claim := range []string{"No Dotty changes", "restoration was verified", "Requested changes committed", "hint:"} {
				if strings.Contains(got, claim) {
					t.Fatalf("reassuring or parsed claim %q: %q", claim, got)
				}
			}
			if name == "warning then unknown" || name == "unknown then warning" ||
				name == "multiwrap unknown" ||
				name == "wrapped join" {
				if !strings.Contains(got, "other failure") || !strings.Contains(got, "/backup") {
					t.Fatal("failure or retained path discarded")
				}
			}
		})
	}
}
