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
		{name: "partial readiness", boundary: "partial readiness", want: 0},
		{name: "truncated readiness", boundary: "truncated readiness", want: 1, forward: true},
		{name: "invalid readiness", boundary: "invalid readiness", want: 1, forward: true},
		{name: "truncated readiness INT", boundary: "truncated readiness", signal: syscall.SIGINT, want: 130, forward: true},
		{name: "invalid readiness TERM", boundary: "invalid readiness", signal: syscall.SIGTERM, want: 143, forward: true},
		{name: "unterminated readiness INT", boundary: "unterminated readiness", signal: syscall.SIGINT, want: 130, forward: true},
		{name: "unterminated readiness TERM", boundary: "unterminated readiness", signal: syscall.SIGTERM, want: 143, forward: true},
		{name: "ack transition INT", boundary: "ack transition", signal: syscall.SIGINT, want: 130, forward: true},
		{name: "ack transition TERM", boundary: "ack transition", signal: syscall.SIGTERM, want: 143, forward: true},
		{name: "ack write INT", boundary: "ack write", signal: syscall.SIGINT, want: 130, forward: true},
		{name: "ack write TERM", boundary: "ack write", signal: syscall.SIGTERM, want: 143, forward: true},
		{name: "ack write failure", boundary: "ack write failure", want: 1, forward: true},
		{name: "delivered ack failure", boundary: "delivered ack failure", want: 1, forward: true},
		{name: "invalid ack", boundary: "invalid ack", want: 9},
		{name: "ack EOF", boundary: "ack EOF", want: 9},
		{name: "partial ack EOF", boundary: "partial ack EOF", want: 9},
		{name: "partial ack open INT", boundary: "partial ack open", signal: syscall.SIGINT, want: 130, forward: true},
		{name: "partial ack open TERM", boundary: "partial ack open", signal: syscall.SIGTERM, want: 143, forward: true},
		{name: "short invalid ack open INT", boundary: "short invalid ack open", signal: syscall.SIGINT, want: 130, forward: true},
		{name: "short invalid ack open TERM", boundary: "short invalid ack open", signal: syscall.SIGTERM, want: 143, forward: true},
		{name: "early runner exit", boundary: "early runner exit", want: 7},
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
			case "ack transition":
				seam = "stage=runner"
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
			// Inject a malformed read result, not a timing-dependent Bash fault.
			// READY corruption was observed in an instrumented fixture; its
			// cause is unproven. Robustness must cover any malformed frame.
			// Gate after the bad frame has been read so cancellation cannot race
			// ahead of the reproduction. The unterminated case instead signals
			// a real read with an incomplete record still on the wire.
			badReady := tc.boundary == "truncated readiness" || tc.boundary == "invalid readiness"
			if badReady && tc.signal != 0 {
				seam := "case \"$message\" in"
				if strings.Count(text, seam) != 1 {
					t.Fatal("missing status dispatch seam")
				}
				text = strings.Replace(text, seam,
					"if [ \"$stage\" = startup ]; then protocol_gate; fi\n  "+seam, 1)
			}
			openAck := tc.boundary == "partial ack open" || tc.boundary == "short invalid ack open"
			if openAck {
				// Pause on the NEXT status read, after the ACK write seam has
				// returned success and all post-write bookkeeping has completed.
				// Keep control open: only cancellation can wake the ACK reader.
				seam := "read_status() {\n  local part read_code"
				if strings.Count(text, seam) != 1 {
					t.Fatal("missing status read seam")
				}
				text = strings.Replace(text, seam,
					seam+"\n  if [ \"$stage\" = runner ]; then protocol_gate; fi", 1)
			}
			badAck := tc.boundary == "invalid ack" || tc.boundary == "ack EOF" ||
				tc.boundary == "partial ack EOF" || openAck
			ackFailure := tc.boundary == "ack write failure" ||
				tc.boundary == "delivered ack failure"
			if tc.boundary == "ack write" || badAck || ackFailure {
				seam := "printf 'ACK\\n' >&4"
				if strings.Count(text, seam) != 1 {
					t.Fatal("missing ACK write seam")
				}
				replacement := "protocol_gate\n      " + seam
				switch tc.boundary {
				case "ack write failure":
					replacement = "false"
				case "delivered ack failure":
					replacement = "{ " + seam + "; false; }"
				case "invalid ack":
					replacement = "printf 'BAD\\n' >&4"
				case "ack EOF":
					replacement = "exec 4>&-"
				case "partial ack EOF":
					replacement = "printf 'AC' >&4; exec 4>&-"
				case "partial ack open":
					replacement = "printf 'AC' >&4"
				case "short invalid ack open":
					replacement = "printf 'A\\n' >&4"
				}
				text = strings.Replace(text, seam, replacement, 1)
			}
			wrapper := focusedTaskTrace(t, root, text)
			fakeGo := `#!/bin/bash
if [ "$1" = test ]; then
  printf '%s\n' started > "$FOCUSED_PROTOCOL_ROOT/work-started"
  export DOTTY_FOCUSED_CANCEL_ROOT="$FOCUSED_PROTOCOL_ROOT"
  export DOTTY_FOCUSED_CANCEL_FIXTURE=child
  export DOTTY_FOCUSED_CANCEL_BEHAVIOR='retained pipe'
  exec "$FOCUSED_PROTOCOL_BINARY" -test.run='^TestFocusedCancellationProcessFixture$'
fi
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
			if tc.signal == 0 && tc.boundary == "partial readiness" {
				waitProtocolFile(t, filepath.Join(root, "boundary"))
				if _, err := gate.WriteString("continue\n"); err != nil {
					t.Fatal(err)
				}
			}
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
			preAckCancel := tc.signal != 0 && (tc.boundary == "startup" ||
				tc.boundary == "transition" || tc.boundary == "partial readiness" ||
				tc.boundary == "ack transition" || tc.boundary == "unterminated readiness")
			if badReady || badAck || preAckCancel || tc.boundary == "ack write" ||
				tc.boundary == "ack write failure" {
				if _, err := os.Stat(
					filepath.Join(root, "work-started"),
				); !errors.Is(
					err,
					os.ErrNotExist,
				) {
					t.Errorf("work launched without startup acknowledgment: %v", err)
				}
			}
			if tc.boundary == "" || (tc.boundary == "partial readiness" && tc.signal == 0) {
				if _, err := os.Stat(filepath.Join(root, "work-started")); err != nil {
					t.Errorf("valid acknowledged startup did not run: %v", err)
				}
			}
			if badReady || preAckCancel {
				trace, err := os.ReadFile(filepath.Join(root, "wrapper-trace"))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(trace), "+focused: printf 'ACK\\n'") {
					t.Error("acknowledged failed/cancelled startup")
				}
			}
			if openAck {
				trace, err := os.ReadFile(filepath.Join(root, "wrapper-trace"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(trace), "invalid focused wrapper acknowledgment") {
					t.Errorf("missing malformed ACK diagnostic: %s", trace)
				}
			}
			if badReady || openAck || tc.boundary == "unterminated readiness" ||
				(tc.boundary == "partial readiness" && tc.signal != 0) {
				// Readiness fixtures start an outer-group descendant; open-ACK
				// fixtures could start one only through acknowledged child work.
				// Wait beyond its write, not just wrapper exit.
				time.Sleep(2200 * time.Millisecond)
				if _, err := os.Stat(
					filepath.Join(root, "sentinel"),
				); !errors.Is(
					err,
					os.ErrNotExist,
				) {
					t.Errorf("descendant activity after startup abort: %v", err)
				}
			}
			logged, err := os.ReadFile(filepath.Join(root, "signals"))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			if tc.forward && len(logged) == 0 {
				t.Fatal("expected owned-group forwarding")
			}
			if badReady || preAckCancel {
				if !strings.Contains(string(logged), "-TERM -- -") ||
					!strings.Contains(string(logged), "-KILL -- -") {
					t.Errorf("startup abort did not escalate TERM/KILL: %s", logged)
				}
			}
			if (tc.boundary == "ack write" || ackFailure || openAck) &&
				strings.Contains(string(logged), "-KILL -- -") {
				t.Errorf("outer group escalated after ACK could be sent: %s", logged)
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
	if boundary == "partial ack open" || boundary == "short invalid ack open" {
		// Exercise the real handshake and child-start barrier, not a replica.
		return run([]string{"./fixture", "TestPlain"}, os.Stdout, os.Stderr)
	}
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
	badReady := boundary == "truncated readiness" || boundary == "invalid readiness" ||
		boundary == "unterminated readiness"
	if badReady || boundary == "partial readiness" {
		child := exec.Command(os.Args[0], "-test.run=^TestFocusedCancellationProcessFixture$")
		child.Env = append(os.Environ(),
			"DOTTY_FOCUSED_CANCEL_FIXTURE=descendant", "DOTTY_FOCUSED_CANCEL_ROOT="+root,
			"DOTTY_FOCUSED_CANCEL_BEHAVIOR=retained pipe")
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if err := child.Start(); err != nil {
			return 9
		}
		defer func() { _ = child.Wait() }()
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(root, "ready")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				return 9
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if boundary == "partial readiness" || badReady || boundary == "early runner exit" {
		ready := os.NewFile(3, "focused-wrapper-status")
		switch boundary {
		case "partial readiness":
			if _, err := ready.WriteString("RUNNER_"); err != nil {
				return 9
			}
			if err := gate(); err != nil {
				return 9
			}
			if _, err := ready.WriteString("READY\n"); err != nil {
				return 9
			}
		case "truncated readiness":
			if _, err := ready.WriteString("RUNNER_READ\n"); err != nil {
				return 9
			}
		case "invalid readiness":
			if _, err := ready.WriteString("INVALID\n"); err != nil {
				return 9
			}
		case "unterminated readiness":
			if _, err := ready.WriteString("RUNNER_"); err != nil {
				return 9
			}
			if err := gate(); err != nil {
				return 9
			}
		case "early runner exit":
			if _, err := ready.WriteString("RUNNER_READY\n"); err != nil {
				return 9
			}
			return 7
		}
		if err := ready.Close(); err != nil {
			return 9
		}
		if err := awaitWrapperAck(); err != nil {
			return 9
		}
	} else if err := notifyWrapperReady(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 9
	}
	select {
	case sig := <-signals:
		return 128 + int(sig.(syscall.Signal))
	default:
	}
	if err := os.WriteFile(filepath.Join(root, "work-started"), nil, 0o600); err != nil {
		return 9
	}
	if boundary == "signal exit" {
		stop()
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		time.Sleep(5 * time.Second)
		return 9
	}
	if boundary == "startup" || boundary == "transition" || boundary == "ack transition" {
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
