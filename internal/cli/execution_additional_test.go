package cli

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/lmilojevicc/dotty/internal/dotty"
)

func TestRenderExecutionErrorQuotesEveryDiagnosticField(t *testing.T) {
	paths := []string{"/z", "/a/../b", "/a\n\x1b\"\\", "/é", "/\xff", " "}
	before := append([]string(nil), paths...)
	d := executionDiagnosticFixture()
	d.Summary = "summary\n\x1b"
	d.ObservedState = "observed\r\t"
	d.RecoveryGuidance = "recovery\a\v"
	d.AffectedPaths = paths
	d.UncertainPaths = paths
	d.StagedPaths = paths
	cause := errors.New("cause\n\x1b")
	err := &dotty.OutcomeError{
		Report: dotty.OutcomeReport{Outcome: dotty.OutcomeUncertainty, Diagnostic: d},
		Cause:  cause,
	}
	var out bytes.Buffer
	if RenderExecutionError(&out, err) != 1 {
		t.Fatal("uncertainty succeeded")
	}
	got := out.String()
	for _, text := range []string{d.Summary, d.ObservedState, d.RecoveryGuidance, cause.Error()} {
		if !strings.Contains(got, fmt.Sprintf("%q", text)) {
			t.Fatalf("missing quoted field %q: %q", text, got)
		}
	}
	for _, path := range paths {
		for _, label := range []string{"affected", "uncertain", "staged"} {
			if !strings.Contains(got, fmt.Sprintf("  %s path: %q\n", label, path)) {
				t.Fatalf("missing %s path %q", label, path)
			}
		}
	}
	if !reflect.DeepEqual(paths, before) {
		t.Fatal("renderer changed supplied paths")
	}
	if strings.ContainsAny(got, "\x1b\r\t\a\v") {
		t.Fatalf("raw control character: %q", got)
	}
}

func TestRenderExecutionErrorInvalidReportGolden(t *testing.T) {
	var out bytes.Buffer
	if RenderExecutionError(&out, &dotty.OutcomeError{}) != 1 {
		t.Fatal("zero report succeeded")
	}
	want := "error: Invalid or conflicting execution report. Transaction state cannot be established from this report.\n" +
		"  unvalidated report: outcome=\"\" summary=\"\" observed=\"\" affected=[] uncertain=[] staged=[] retained=[] recovery=\"\" no-retry=false\n" +
		"  recovery: Inspect the supplied paths and resolve the report before retrying; do not assume restoration or commit.\n"
	if got := out.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderExecutionErrorLegacyCallbacksStayUntyped(t *testing.T) {
	for _, name := range []string{"no callbacks", "rollback nil", "rollback failure", "cleanup failure"} {
		t.Run(name, func(t *testing.T) {
			err := dotty.RunAtomic(func(tx *dotty.Tx) error {
				switch name {
				case "rollback nil":
					tx.AddRollback(func() error { return nil })
				case "rollback failure":
					tx.AddRollback(func() error { return errors.New("restore failed") })
				case "cleanup failure":
					tx.AddCleanup(func() error { return errors.New("cleanup failed") })
					return nil
				}
				return errors.New("operation failed")
			})
			var out bytes.Buffer
			if RenderExecutionError(&out, err) != 1 {
				t.Fatal("legacy error gained successful exit")
			}
			want := "error: operation failed\n"
			if name == "rollback failure" {
				want = "error: operation failed (rollback failed: restore failed)\n"
			}
			if name == "cleanup failure" {
				want = "error: cleanup failed\n"
			}
			if got := stripANSI(out.String()); got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

func TestRenderExecutionErrorJoinedCleanupCauses(t *testing.T) {
	d := executionDiagnosticFixture()
	d.RetainedPaths, d.NoRetry = []string{"/backup"}, true
	first, second := errors.New("first cleanup"), errors.New("second cleanup")
	err := &dotty.OutcomeError{
		Report: dotty.OutcomeReport{Outcome: dotty.OutcomeCommittedCleanupWarning, Diagnostic: d},
		Cause:  errors.Join(first, second),
	}
	var out bytes.Buffer
	if RenderExecutionError(&out, err) != 0 {
		t.Fatal("ordinary cleanup causes conflict with their enclosing report")
	}
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatal("joined causes lost")
	}
	if !strings.Contains(out.String(), "  cause: \"first cleanup\\nsecond cleanup\"\n") {
		t.Fatalf("missing causes: %q", out.String())
	}
}
