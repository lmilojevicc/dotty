//go:build darwin || linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Pause at protocol boundaries in a private copy, not through production test
// switches. Log every wrapper signal: a completed runner PID is never a target.
func TestFocusedTaskProtocol(t *testing.T) {
	for _, tc := range []struct {
		name     string
		boundary string
		signal   syscall.Signal
		status   int
		want     int
		forward  bool
	}{
		{name: "success", status: 0, want: 0},
		{name: "exit7", status: 7, want: 7},
		{name: "build failure", boundary: "build failure", want: 7},
		{name: "startup failure", boundary: "startup failure", want: 7},
		{name: "signal status", boundary: "signal exit", want: 143},
		{name: "before anchor INT", boundary: "before anchor", signal: syscall.SIGINT, want: 130, forward: true},
		{name: "before anchor TERM", boundary: "before anchor", signal: syscall.SIGTERM, want: 143, forward: true},
		{name: "build readiness INT", boundary: "build readiness", signal: syscall.SIGINT, want: 130, forward: true},
		{name: "build readiness TERM", boundary: "build readiness", signal: syscall.SIGTERM, want: 143, forward: true},
		{name: "transition INT", boundary: "transition", signal: syscall.SIGINT, want: 130, forward: true},
		{name: "transition TERM", boundary: "transition", signal: syscall.SIGTERM, want: 143, forward: true},
		{name: "startup INT", boundary: "startup", signal: syscall.SIGINT, want: 130, forward: true},
		{name: "startup TERM", boundary: "startup", signal: syscall.SIGTERM, want: 143, forward: true},
		{name: "partial readiness INT", boundary: "partial readiness", signal: syscall.SIGINT, want: 130, forward: true},
		{name: "partial readiness TERM", boundary: "partial readiness", signal: syscall.SIGTERM, want: 143, forward: true},
		{name: "completion INT", boundary: "completion", signal: syscall.SIGINT, status: 7, want: 130, forward: true},
		{name: "completion TERM", boundary: "completion", signal: syscall.SIGTERM, status: 7, want: 143, forward: true},
		{name: "finished INT", boundary: "finished", signal: syscall.SIGINT, status: 7, want: 7},
		{name: "finished TERM", boundary: "finished", signal: syscall.SIGTERM, status: 7, want: 7},
		{name: "interrupted waits INT", boundary: "waiting", signal: syscall.SIGINT, status: 7, want: 130, forward: true},
		{name: "interrupted waits TERM", boundary: "waiting", signal: syscall.SIGTERM, status: 7, want: 143, forward: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			source, err := os.ReadFile("task.sh")
			if err != nil {
				t.Fatal(err)
			}
			text := string(source)
			helpers := `
kill() {
  # A trap can run inside IFS= read; never join signal arguments with $*.
  printf '%s %s %s\n' "$@" >> "$FOCUSED_PROTOCOL_ROOT/signals"
  printf '%s\n' "$anchor" > "$FOCUSED_PROTOCOL_ROOT/anchor"
  builtin kill "$@"
}
protocol_gate() {
  printf '%s\n' "$anchor" > "$FOCUSED_PROTOCOL_ROOT/anchor"
  printf '%s\n' ready > "$FOCUSED_PROTOCOL_ROOT/boundary"
  while ! IFS= read -r gate < "$FOCUSED_PROTOCOL_ROOT/gate"; do :; done
}
`
			text = strings.Replace(text, "cancelled=0", helpers+"\ncancelled=0", 1)
			text = strings.Replace(
				text,
				"cancel() {",
				"cancel() {\n  printf '%s\\n' observed > \"$FOCUSED_PROTOCOL_ROOT/observed\"",
				1,
			)
			var seam string
			switch tc.boundary {
			case "before anchor":
				seam = "set -m"
			case "build readiness":
				seam = "exec 4> \"$tmp/control\""
			case "transition":
				seam = "stage=startup"
			case "completion":
				seam = "code=${message#RUNNER_DONE }"
			case "finished":
				seam = "stage=finished"
			}
			if seam != "" {
				if strings.Count(text, seam) != 1 {
					t.Fatalf("missing unique seam %q", seam)
				}
				text = strings.Replace(text, seam, seam+"\nprotocol_gate", 1)
			}
			wrapper := focusedTaskTrace(t, root, text)
			fakeGo := `#!/bin/bash
[ "$1" = build ] || exit 9
[ "$FOCUSED_PROTOCOL_BOUNDARY" = 'build failure' ] && exit 7
printf '%s\n' '#!/bin/bash' 'exec "$FOCUSED_PROTOCOL_BINARY" -test.run="^TestFocusedTaskProtocolFixture$"' > "$3"
chmod 700 "$3"
`
			if err := os.WriteFile(filepath.Join(root, "go"), []byte(fakeGo), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(filepath.Join(root, "gate"), 0o600); err != nil {
				t.Fatal(err)
			}
			// O_RDWR on this test-only gate avoids an open blocked by a trapped
			// signal. Production uses paired one-way descriptors and real EOF.
			gate, err := os.OpenFile(filepath.Join(root, "gate"), os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer gate.Close()
			cmd := exec.Command("bash", wrapper, "./fixture", "TestPlain")
			cmd.Env = append(
				os.Environ(),
				"PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"),
				"TMPDIR="+root,
				"FOCUSED_PROTOCOL_ROOT="+root,
				"FOCUSED_PROTOCOL_BINARY="+binary,
				"FOCUSED_PROTOCOL_BOUNDARY="+tc.boundary,
				"FOCUSED_PROTOCOL_STATUS="+strconv.Itoa(tc.status),
			)
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			if tc.signal != 0 {
				waitProtocolFile(t, filepath.Join(root, "boundary"))
				if tc.boundary == "completion" || tc.boundary == "finished" {
					data, err := os.ReadFile(filepath.Join(root, "anchor"))
					if err != nil {
						t.Fatal(err)
					}
					pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
					if err != nil {
						t.Fatal(err)
					}
					// The runner has already exited, but the anchor must still be
					// live at this deterministic pre-release boundary. This probe
					// is a test assertion, not the production ownership mechanism.
					if err := syscall.Kill(pid, 0); err != nil {
						t.Fatalf("anchor released early: %v", err)
					}
				}
				if err := cmd.Process.Signal(tc.signal); err != nil {
					t.Fatal(err)
				}
				waitProtocolFile(t, filepath.Join(root, "observed"))
				if tc.boundary == "before anchor" || tc.boundary == "build readiness" {
					if data, err := os.ReadFile(
						filepath.Join(root, "signals"),
					); !errors.Is(
						err,
						os.ErrNotExist,
					) {
						t.Fatalf("forwarded before BUILDING acknowledgment: %q, %v", data, err)
					}
				}
				// A second signal must interrupt waits without releasing ownership
				// or mistaking Bash's interrupted-wait status for RUNNER_DONE.
				if tc.boundary == "waiting" {
					waitProtocolFile(t, filepath.Join(root, "received"))
					if err := cmd.Process.Signal(tc.signal); err != nil {
						t.Fatal(err)
					}
					waitProtocolFile(t, filepath.Join(root, "received-again"))
				}
				if _, err := gate.WriteString("continue\n"); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case err := <-done:
				code := 0
				if err != nil {
					var exitErr *exec.ExitError
					if !errors.As(err, &exitErr) {
						t.Fatal(err)
					}
					code = exitErr.ExitCode()
				}
				if code != tc.want {
					t.Fatalf("exit = %d, want %d; output: %s", code, tc.want, &output)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("wrapper did not complete protocol")
			}
			artifacts, err := filepath.Glob(filepath.Join(root, "dotty-focused.*"))
			if err != nil || len(artifacts) != 0 {
				t.Errorf("cleanup before return: %v, %v", artifacts, err)
			}
			logged, err := os.ReadFile(filepath.Join(root, "signals"))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			if tc.forward && len(logged) == 0 {
				t.Fatal("expected owned-group forwarding")
			}
			anchor, err := os.ReadFile(filepath.Join(root, "anchor"))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			if tc.boundary == "finished" && len(logged) != 0 {
				t.Fatalf("forwarded after finish: %s", logged)
			}
			for _, line := range strings.Split(strings.TrimSpace(string(logged)), "\n") {
				if line == "" {
					continue
				}
				fields := strings.Fields(line)
				if len(fields) != 3 || fields[1] != "--" ||
					fields[2] != "-"+strings.TrimSpace(string(anchor)) {
					t.Errorf("signal was not to an owned group: %q", line)
				}
			}
		})
	}
}

// Trace only the private fixture wrapper, never the calling shell or its
// environment. Failure output is bounded even if a protocol loop misbehaves.
func focusedTaskTrace(t *testing.T, root, source string) string {
	t.Helper()
	prefix := `exec 2> "${0%/*}/wrapper-trace"
printf 'Bash %s\n' "$BASH_VERSION" >&2
PS4='+focused: '
set -x
`
	text := strings.Replace(source, "#!/usr/bin/env bash\n", "#!/usr/bin/env bash\n"+prefix, 1)
	if text == source {
		t.Fatal("missing wrapper trace seam")
	}
	wrapper := filepath.Join(root, "task.sh")
	if err := os.WriteFile(wrapper, []byte(text), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		for _, name := range []string{"wrapper-trace", "process-events"} {
			data, err := os.ReadFile(filepath.Join(root, name))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				t.Logf("read %s: %v", name, err)
				continue
			}
			const tail = 16 * 1024
			if len(data) > tail {
				data = data[len(data)-tail:]
			}
			t.Logf("%s (bounded tail):\n%s", name, data)
		}
	})
	return wrapper
}

func waitProtocolFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("protocol boundary not reached: %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestFocusedTaskProtocolFixture(_ *testing.T) {
	root := os.Getenv("FOCUSED_PROTOCOL_ROOT")
	if root == "" {
		return
	}
	// A failing protocol regression must not strand its fixture indefinitely.
	time.AfterFunc(8*time.Second, func() { os.Exit(9) })
	os.Exit(runFocusedTaskProtocolFixture(root))
}

func runFocusedTaskProtocolFixture(root string) int {
	boundary := os.Getenv("FOCUSED_PROTOCOL_BOUNDARY")
	if boundary == "startup failure" {
		return 7
	}
	gate := func() error {
		if err := os.WriteFile(filepath.Join(root, "boundary"), nil, 0o600); err != nil {
			return err
		}
		f, err := os.Open(filepath.Join(root, "gate"))
		if err != nil {
			return err
		}
		var b [1]byte
		_, err = f.Read(b[:])
		_ = f.Close()
		return err
	}
	if boundary == "startup" {
		if err := gate(); err != nil {
			return 9
		}
	}
	signals, stop := cancellationSignals()
	defer stop()
	if boundary == "partial readiness" {
		ready := os.NewFile(3, "focused-wrapper-status")
		if _, err := ready.WriteString("RUNNER_"); err != nil {
			return 9
		}
		if err := gate(); err != nil {
			return 9
		}
		if _, err := ready.WriteString("READY\n"); err != nil {
			return 9
		}
		if err := ready.Close(); err != nil {
			return 9
		}
	} else if err := notifyWrapperReady(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 9
	}
	if boundary == "signal exit" {
		stop()
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		time.Sleep(5 * time.Second)
		return 9
	}
	if boundary == "startup" || boundary == "transition" || boundary == "partial readiness" {
		sig := <-signals
		return 128 + int(sig.(syscall.Signal))
	}
	if boundary == "waiting" {
		if err := os.WriteFile(filepath.Join(root, "boundary"), nil, 0o600); err != nil {
			return 9
		}
		<-signals
		if err := os.WriteFile(filepath.Join(root, "received"), nil, 0o600); err != nil {
			return 9
		}
		<-signals
		if err := os.WriteFile(filepath.Join(root, "received-again"), nil, 0o600); err != nil {
			return 9
		}
		if err := gate(); err != nil {
			return 9
		}
	}
	code, err := strconv.Atoi(os.Getenv("FOCUSED_PROTOCOL_STATUS"))
	if err != nil {
		return 9
	}
	return code
}
