//go:build darwin || linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Signal the runner/wrapper PID, not the terminal's process group. The fake Go
// driver owns a stubborn descendant, so merely killing the direct child fails.
func TestFocusedCancellation(t *testing.T) {
	source, err := os.ReadFile("task.sh")
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		stage    string
		behavior string
	}{
		{"runner", "retained pipe"},
		{"wrapper runner", "retained pipe"},
		{"wrapper build", "retained pipe"},
		{"runner", "early close"},
		{"wrapper runner", "early close"},
		{"runner", "driver exits"},
		{"wrapper runner", "driver exits"},
	} {
		stage := tc.stage
		for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
			t.Run(fmt.Sprintf("%s/%s/%s", stage, tc.behavior, sig), func(t *testing.T) {
				root := t.TempDir()
				fakeGo := `#!/bin/bash
if [ "$1" = build ]; then
  if [ "$DOTTY_FOCUSED_CANCEL_STAGE" = 'wrapper build' ]; then
    export DOTTY_FOCUSED_CANCEL_FIXTURE=child
    exec "$DOTTY_FOCUSED_TEST_BINARY" -test.run='^TestFocusedCancellationProcessFixture$'
  fi
  printf '%s\n' '#!/bin/bash' 'export DOTTY_FOCUSED_CANCEL_FIXTURE=runner' 'exec "$DOTTY_FOCUSED_TEST_BINARY" -test.run="^TestFocusedCancellationProcessFixture$"' > "$3"
  chmod 700 "$3"
  exit
fi
export DOTTY_FOCUSED_CANCEL_FIXTURE=child
exec "$DOTTY_FOCUSED_TEST_BINARY" -test.run='^TestFocusedCancellationProcessFixture$'
`
				goPath := filepath.Join(root, "go")
				if err := os.WriteFile(goPath, []byte(fakeGo), 0o700); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command(binary, "-test.run=^TestFocusedCancellationProcessFixture$")
				if stage != "runner" {
					wrapper := focusedTaskTrace(t, root, string(source))
					cmd = exec.Command("bash", wrapper, "./fixture", "TestPlain")
				}
				cmd.Env = append(os.Environ(),
					"PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"),
					"TMPDIR="+root,
					"DOTTY_FOCUSED_CANCEL_FIXTURE=runner",
					"DOTTY_FOCUSED_CANCEL_STAGE="+stage,
					"DOTTY_FOCUSED_CANCEL_BEHAVIOR="+tc.behavior,
					"DOTTY_FOCUSED_TEST_BINARY="+binary,
					"DOTTY_FOCUSED_CANCEL_ROOT="+root,
				)
				var output bytes.Buffer
				cmd.Stdout, cmd.Stderr = &output, &output
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				done := make(chan error, 1)
				go func() { done <- cmd.Wait() }()
				t.Cleanup(func() { _ = cmd.Process.Kill() })
				deadline := time.Now().Add(5 * time.Second)
				for {
					if _, err := os.Stat(filepath.Join(root, "ready")); err == nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("descendant did not become ready")
					}
					time.Sleep(10 * time.Millisecond)
				}
				if stage == "wrapper build" {
					waitFocusedBuildStatusRead(t, root)
				}
				if tc.behavior == "early close" {
					// Give the decoder time to observe EOF if no anchor owns stdout.
					time.Sleep(100 * time.Millisecond)
				}
				focusedCancellationEvent(
					root,
					fmt.Sprintf("sending %s to wrapper/runner %d", sig, cmd.Process.Pid),
				)
				if err := cmd.Process.Signal(sig); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-done:
					focusedCancellationEvent(root, fmt.Sprintf("wrapper/runner returned %v", err))
					var exitErr *exec.ExitError
					if !errors.As(err, &exitErr) || exitErr.ExitCode() != 128+int(sig) {
						t.Errorf("exit = %v, want %d; output: %s", err, 128+int(sig), &output)
					}
				case <-time.After(4 * time.Second):
					t.Fatal("cancellation did not finish within bound")
				}
				artifacts, err := filepath.Glob(filepath.Join(root, "dotty-focused.*"))
				if err != nil || len(artifacts) != 0 {
					t.Errorf("temporary artifacts at return = %v, error = %v", artifacts, err)
				}
				// Observe past the descendant's scheduled write, even if Wait
				// returned immediately because only the runner was terminated.
				time.Sleep(2200 * time.Millisecond)
				if _, err := os.Stat(
					filepath.Join(root, "sentinel"),
				); !errors.Is(
					err,
					os.ErrNotExist,
				) {
					t.Errorf("descendant activity after cancellation: %v", err)
				}
			})
		}
	}
}

// The second status read follows BUILDING and waits for the still-running
// fixture's BUILD_STATUS. Its trace is emitted after any pre-read cancellation
// check, so signalling here covers the blocked/entering-read window even when
// Bash resumes read after the trap. Descendant readiness alone does not prove
// that the wrapper has consumed BUILDING.
func waitFocusedBuildStatusRead(t *testing.T, root string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(filepath.Join(root, "wrapper-trace"))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(data), "+focused: read -r part\n") >= 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("wrapper did not enter BUILD_STATUS read")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Diagnostics only: record identity/timing without probing or signalling any
// other process. The sentinel remains the cancellation regression's oracle.
func focusedCancellationEvent(root, event string) {
	f, err := os.OpenFile(
		filepath.Join(root, "process-events"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0o600,
	)
	if err != nil {
		return
	}
	pgid, groupErr := syscall.Getpgid(0)
	_, _ = fmt.Fprintf(
		f,
		"%d pid=%d pgid=%d group-error=%v %s\n",
		time.Now().UnixNano(),
		os.Getpid(),
		pgid,
		groupErr,
		event,
	)
	_ = f.Close()
}

func TestFocusedCancellationProcessFixture(_ *testing.T) {
	mode := os.Getenv("DOTTY_FOCUSED_CANCEL_FIXTURE")
	if mode != "" {
		focusedCancellationEvent(os.Getenv("DOTTY_FOCUSED_CANCEL_ROOT"), "starting "+mode)
	}
	switch mode {
	case "runner":
		os.Exit(run([]string{"./fixture", "TestPlain"}, os.Stdout, os.Stderr))
	case "child":
		behavior := os.Getenv("DOTTY_FOCUSED_CANCEL_BEHAVIOR")
		signals := make(chan os.Signal, 1)
		if behavior == "driver exits" {
			signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
		} else {
			signal.Ignore(syscall.SIGINT, syscall.SIGTERM)
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestFocusedCancellationProcessFixture$")
		cmd.Env = append(os.Environ(), "DOTTY_FOCUSED_CANCEL_FIXTURE=descendant")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			os.Exit(9)
		}
		if behavior == "early close" {
			if err := os.Stdout.Close(); err != nil {
				os.Exit(9)
			}
			root := os.Getenv("DOTTY_FOCUSED_CANCEL_ROOT")
			if err := os.WriteFile(filepath.Join(root, "child-closed"), nil, 0o600); err != nil {
				os.Exit(9)
			}
		}
		if behavior == "driver exits" {
			<-signals
			os.Exit(0)
		}
		if err := cmd.Wait(); err != nil {
			os.Exit(9)
		}
		os.Exit(0)
	case "descendant":
		signal.Ignore(syscall.SIGINT, syscall.SIGTERM)
		root := os.Getenv("DOTTY_FOCUSED_CANCEL_ROOT")
		if os.Getenv("DOTTY_FOCUSED_CANCEL_BEHAVIOR") == "early close" {
			if err := os.Stdout.Close(); err != nil {
				os.Exit(9)
			}
			// Publish readiness only after both driver and descendant have
			// closed stdout. Neither can keep the runner's decoder waiting.
			deadline := time.Now().Add(4 * time.Second)
			for {
				if _, err := os.Stat(filepath.Join(root, "child-closed")); err == nil {
					break
				}
				if time.Now().After(deadline) {
					os.Exit(9)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		focusedCancellationEvent(root, "descendant ready")
		if err := os.WriteFile(filepath.Join(root, "ready"), []byte("ready"), 0o600); err != nil {
			os.Exit(9)
		}
		time.Sleep(2 * time.Second)
		focusedCancellationEvent(root, "descendant writing sentinel")
		if err := os.WriteFile(
			filepath.Join(root, "sentinel"),
			[]byte("orphan activity"),
			0o600,
		); err != nil {
			os.Exit(9)
		}
		os.Exit(0)
	}
}

func TestFocusedChildInvocationContext(t *testing.T) {
	root := t.TempDir()
	script := `#!/bin/bash
[ . -ef "$FOCUSED_EXPECTED_DIR" ] || exit 9
[ "$#" -eq 2 ] && [ "$1" = 'space ; $HOME " quote' ] && [ -z "$2" ] || exit 9
IFS= read -r input
[ "$input" = 'stdin preserved' ] || exit 9
exit 7
`
	path := filepath.Join(root, "driver with spaces")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{path, "./driver with spaces"} {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(name, `space ; $HOME " quote`, "")
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "FOCUSED_EXPECTED_DIR="+root)
			cmd.Stdin = strings.NewReader("stdin preserved\n")
			var out bytes.Buffer
			if code := execute(cmd, "TestPlain", &out, &out); code != 7 {
				t.Fatalf("exit = %d, want 7: %s", code, &out)
			}
		})
	}
}

func TestFocusedChildStartErrors(t *testing.T) {
	t.Run("existing command error", func(t *testing.T) {
		cmd := exec.Command("go")
		original := errors.New("original lookup error")
		cmd.Err = original
		if err := prepareChild(cmd); !errors.Is(err, original) || !errors.Is(cmd.Err, original) {
			t.Fatalf("prepare error = %v, cmd.Err = %v; want original error", err, cmd.Err)
		}
	})
	for _, name := range []string{"missing", "not executable"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if name == "not executable" {
				if err := os.WriteFile(
					filepath.Join(root, name),
					[]byte("exit 7\n"),
					0o600,
				); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("./" + name)
			cmd.Dir = root
			var out bytes.Buffer
			if code := execute(cmd, "TestPlain", &out, &out); code != 1 {
				t.Fatalf("exit = %d, want startup failure 1: %s", code, &out)
			}
		})
	}
}

func TestFocusedChildSignalExit(t *testing.T) {
	cmd := exec.Command("bash", "-c", "kill -TERM $$")
	var out bytes.Buffer
	if code := execute(cmd, "TestPlain", &out, &out); code != 143 {
		t.Fatalf("exit = %d, want 143: %s", code, strings.TrimSpace(out.String()))
	}
}
