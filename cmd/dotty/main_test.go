package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/lmilojevicc/dotty/internal/dotty"
)

// These are synthetic transport fixtures, not command-level filesystem evidence.
func executionFixture(name string) error {
	d := dotty.OutcomeDiagnostic{
		Summary:          "fixture",
		ObservedState:    "fixture state",
		RecoveryGuidance: "inspect fixture paths",
		AffectedPaths:    []string{"fixture-target"},
	}
	switch name {
	case "success":
		return nil
	case "ordinary":
		return errors.New("fixture failure")
	case "refusal":
		return &dotty.RefusalError{Diagnostic: d}
	case "rollback":
		return &dotty.OutcomeError{
			Report: dotty.OutcomeReport{
				Outcome:    dotty.OutcomeVerifiedRolledBackFailure,
				Diagnostic: d,
			},
		}
	case "uncertainty":
		d.UncertainPaths = []string{"fixture-target"}
		d.StagedPaths = []string{"fixture-stage"}
		return &dotty.OutcomeError{
			Report: dotty.OutcomeReport{Outcome: dotty.OutcomeUncertainty, Diagnostic: d},
		}
	case "warning":
		d.RetainedPaths, d.NoRetry = []string{"fixture-backup"}, true
		return &dotty.OutcomeError{
			Report: dotty.OutcomeReport{
				Outcome:    dotty.OutcomeCommittedCleanupWarning,
				Diagnostic: d,
			},
		}
	default:
		return errors.New("unknown test fixture")
	}
}

func TestExecutionSubprocessHelper(*testing.T) {
	// This finite test-only selector never accepts a filesystem path or executes a command.
	name := os.Getenv("DOTTY_TEST_EXECUTION_FIXTURE")
	if name == "" {
		return
	}
	os.Exit(runExecution(func() error { return executionFixture(name) }, os.Stderr))
}

func TestExecutionEntrypointSubprocessMapping(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const details = "  summary: \"fixture\"\n  observed state: \"fixture state\"\n  affected path: \"fixture-target\"\n"
	const recovery = "  recovery: \"inspect fixture paths\"\n"
	for _, tt := range []struct {
		name   string
		code   int
		stderr string
	}{
		{"success", 0, ""},
		{"ordinary", 1, "error: fixture failure\n"},
		{"refusal", 1, "error: Refused before mutation. No Dotty changes were made.\n" + details + recovery},
		{"rollback", 1, "error: Requested state did not commit. Dotty's changes were reversed and restoration was verified, preserving external edits.\n" + details + recovery},
		{"uncertainty", 1, "error: Restoration could not be proven. Transaction state is uncertain.\n" + details + "  uncertain path: \"fixture-target\"\n  staged path: \"fixture-stage\"\n" + recovery},
		{"warning", 0, "warning: Requested changes committed; cleanup is incomplete. DO NOT RETRY the operation.\n" + details + "  retained path: \"fixture-backup\"\n" + recovery},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			cmd := exec.Command(executable, "-test.run=^TestExecutionSubprocessHelper$")
			cmd.Dir = root
			cmd.Env = []string{
				"HOME=" + root,
				"XDG_CONFIG_HOME=" + filepath.Join(root, "config"),
				"DOTTY_REPO=" + filepath.Join(root, "repo"),
				"TMPDIR=" + root,
				"NO_COLOR=1", "TERM=dumb",
				"DOTTY_TEST_EXECUTION_FIXTURE=" + tt.name,
			}
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			code := 0
			if err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatal(err)
				}
				code = exitErr.ExitCode()
			}
			if code != tt.code || stdout.String() != "" || stderr.String() != tt.stderr {
				t.Fatalf(
					"exit=%d stdout=%q stderr=%q; want exit=%d stderr=%q",
					code,
					stdout.String(),
					stderr.String(),
					tt.code,
					tt.stderr,
				)
			}
		})
	}
}

func TestRunExecutionPreservesCallerOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runExecution(func() error {
		_, _ = io.WriteString(&stdout, "caller result\n")
		return executionFixture("warning")
	}, &stderr)
	if code != 0 || stdout.String() != "caller result\n" {
		t.Fatal("caller output changed")
	}
}
