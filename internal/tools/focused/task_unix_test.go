//go:build darwin || linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Tests-first PULSE REVISION checkpoint: source inventory, NOT runtime evidence.
// Independent source/launcher review and Darwin Bash3.2 + Linux prerequisites
// precede execution. All 42 TestFocusedTaskProtocol cases and the existing real
// blocking-read, ACK, interrupted-wait and cancellation assertions remain gates.
// Required roots and explicit subcases are maintained below:
// RecordFraming, Publication, WakeClassification, DescriptorClosure,
// SolePulseWriter, PulseWake, NoPulseNegative, PulseCancellation,
// PendingContinuation, PulseLifecycle, IdentityRecords, OuterIdentity,
// AuthorizationHealth, NegativeRetention, FatalOwner, FatalOwnerFixture,
// StartFailure, ParentAbort, OwnerOnce, BoundedCapture, CleanupUncertainty,
// CaptureEOF, EventTermination, EventByteBudget, EventChannel (all prefixed TestFocusedProtocol). No selected-case waiver.
func TestFocusedProtocolRecordFraming(t *testing.T) {
	frame := protocolFrame("before anchor", "epoch-1", "absent", "continue")
	for _, tc := range []struct {
		name  string
		data  string
		valid bool
	}{
		{"exact", frame, true},
		{"empty", "", false},
		{"partial", frame[:len(frame)-1], false},
		{"malformed", "continue", false},
		{"stale", protocolFrame("before anchor", "epoch-0", "absent", "continue"), false},
		{"wrong phase", protocolFrame("transition", "epoch-1", "absent", "continue"), false},
		{"wrong epoch", protocolFrame("before anchor", "epoch-2", "absent", "continue"), false},
		{"wrong anchor", protocolFrame("before anchor", "epoch-1", "123", "continue"), false},
		{"wrong action", protocolFrame("before anchor", "epoch-1", "absent", "cancel"), false},
		{"newline", frame + "\n", false},
		{"newlines", frame + "\n\n", false},
		{"trailing bytes", frame + "extra", false},
		{"NUL suffix", frame + "\x00", false},
		{"NUL prefix", "\x00" + frame, false},
		{"NUL middle", frame[:8] + "\x00" + frame[8:], false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := protocolExact([]byte(tc.data), frame); got != tc.valid {
				t.Fatalf("Go exact reader = %v, want %v", got, tc.valid)
			}
			// Exercise the SAME finite, untimed shell reader used by the gate.
			protocolCheckShellRecord(t, tc.data, frame, tc.valid)
		})
	}
}

func TestFocusedProtocolPublication(t *testing.T) {
	root := protocolPrivateRoot(t)
	path := filepath.Join(root, "record")
	if err := publishProtocolRecord(path, "complete"); err != nil {
		t.Fatal(err)
	}
	if err := publishProtocolRecord(path, "replacement"); err == nil {
		t.Fatal("record overwritten")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "complete" {
		t.Fatalf("immutable record: %q, %v", data, err)
	}
	if err := publishProtocolRecord(filepath.Join(root, "missing", "record"), "data"); err == nil {
		t.Fatal("publication into absent directory succeeded")
	}
	if err := os.WriteFile(filepath.Join(root, "partial"), []byte("com"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readProtocolRecord(filepath.Join(root, "partial"), "complete"); err == nil {
		t.Fatal("published partial record treated as pending")
	}
	if ready, err := readProtocolRecord(
		filepath.Join(root, "absent"),
		"complete",
	); err != nil ||
		ready {
		t.Fatalf("missing is pending: %v, %v", ready, err)
	}
	for _, kind := range []string{"directory", "FIFO", "symlink"} {
		path := filepath.Join(root, kind)
		var err error
		switch kind {
		case "directory":
			err = os.Mkdir(path, 0o700)
		case "FIFO":
			err = syscall.Mkfifo(path, 0o600)
		case "symlink":
			err = os.Symlink(filepath.Join(root, "record"), path)
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := readProtocolRecord(path, "complete"); err == nil {
			t.Fatalf("accepted nonregular %s", kind)
		}
	}
	t.Run("initial absence boundary", protocolCheckRecordAbsence)
}

func protocolCheckRecordAbsence(t *testing.T) {
	t.Run("initial absence is pending", func(t *testing.T) {
		root := protocolPrivateRoot(t)
		path := filepath.Join(root, "missing")
		_, err := readProtocolRegular(path, 16)
		if !errors.Is(err, os.ErrNotExist) || !protocolRecordIsPending(err) {
			t.Fatalf("missing initial observation not retained: %v", err)
		}
		if ready, err := readProtocolRecord(path, "complete"); ready || err != nil {
			t.Fatalf("initial absence not pending: %v,%v", ready, err)
		}
	})
	t.Run("wrapped or joined absence is fatal", func(t *testing.T) {
		root := protocolPrivateRoot(t)
		_, absent := readProtocolRegular(filepath.Join(root, "missing"), 16)
		if !protocolRecordIsPending(absent) {
			t.Fatal(absent)
		}
		for _, err := range []error{fmt.Errorf("later context: %w", absent), errors.Join(absent, syscall.EIO), &os.PathError{Op: "lstat", Path: "unmarked", Err: syscall.ENOENT}} {
			if !errors.Is(err, os.ErrNotExist) || protocolRecordIsPending(err) {
				t.Fatalf("non-initial failure became pending: %v", err)
			}
		}
	})
	t.Run("observed disappearance is fatal", func(t *testing.T) {
		root := protocolPrivateRoot(t)
		path := filepath.Join(root, "record")
		if err := publishProtocolRecord(path, "complete"); err != nil {
			t.Fatal(err)
		}
		hits := 0
		ready, err := readProtocolRecordChecked(path, "complete", func() {
			hits++
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		})
		if hits != 1 || ready || err == nil || !errors.Is(err, os.ErrNotExist) ||
			protocolRecordIsPending(err) {
			t.Fatalf(
				"disappearance was retried as initial absence: hits=%d ready=%v error=%v",
				hits,
				ready,
				err,
			)
		}
		if err := publishProtocolRecord(path, "complete"); err != nil {
			t.Fatal(err)
		}
		if protocolRecordIsPending(err) {
			t.Fatal("recreation changed the prior fatal classification")
		}
		joined := errors.Join(err, syscall.EIO)
		if !errors.Is(joined, os.ErrNotExist) || !errors.Is(joined, syscall.EIO) ||
			protocolRecordIsPending(joined) {
			t.Fatalf("joined post-observation causes suppressed: %v", joined)
		}
	})
}

func TestFocusedProtocolWakeClassification(t *testing.T) {
	for _, tc := range []struct {
		name      string
		code      int
		data      string
		cancelled int
		want      string
	}{
		{"benign empty wake", 0, "", 0, "wake"},
		{"old timeout rejected", 142, "", 0, "failure"},
		{"interruption", 130, "", 130, "interrupted"},
		{"TERM interruption", 143, "", 143, "interrupted"},
		{"data", 0, "unexpected", 0, "failure"},
		{"EOF", 1, "", 0, "failure"},
		{"unexplained", 2, "", 0, "failure"},
		{"unobserved interruption", 130, "", 0, "failure"},
		{"partial data", 142, "unexpected", 0, "failure"},
	} {
		t.Run(
			tc.name,
			func(t *testing.T) { protocolCheckWakeClassification(t, tc.code, tc.data, tc.cancelled, tc.want) },
		)
	}
	t.Run("finished interruption is observed but ignored", func(t *testing.T) {
		root := protocolPrivateRoot(t)
		script := "stage=finished\ncancelled=0\nprotocol_cancel_count=1\ngate_code=130\ngate_data=\nprotocol_classify\n[ \"$protocol_class\" = interrupted ]\n"
		protocolRequireExit(t, protocolScript(t, root, script), 0)
	})
	protocolCheckErrorLatch(t)
	t.Run("actual pipe EOF latches", protocolCheckPulseClosure)
}

func TestFocusedProtocolDescriptorClosure(t *testing.T) { protocolCheckDescriptor(t) }
func TestFocusedProtocolSolePulseWriter(t *testing.T)   { protocolCheckSolePulseWriter(t) }

func TestFocusedProtocolPulseWake(
	t *testing.T,
) {
	protocolSensitivity(t, "before anchor", 0, false)
}

func TestFocusedProtocolNoPulseNegative(t *testing.T) {
	protocolSensitivity(t, "before anchor", 0, true)
}

func TestFocusedProtocolPulseCancellation(t *testing.T) {
	for _, phase := range []string{"before anchor", "transition"} {
		for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
			if !t.Run(
				fmt.Sprintf("%s/%s", phase, sig),
				func(t *testing.T) { protocolSensitivity(t, phase, sig, false) },
			) {
				return
			}
		}
	}
}
func TestFocusedProtocolParentAbort(t *testing.T) { protocolCheckParentAbort(t) }
func TestFocusedProtocolOwnerOnce(t *testing.T)   { protocolCheckOwnerOnce(t) }
func TestFocusedProtocolBoundedCapture(t *testing.T) {
	for _, tc := range []struct {
		name   string
		writes []int
		lost   bool
	}{
		{"limit minus one", []int{protocolCaptureLimit - 1}, false},
		{"limit", []int{protocolCaptureLimit}, false},
		{"limit plus one", []int{protocolCaptureLimit + 1}, true},
		{"cumulative exact", []int{1, protocolCaptureLimit - 1}, false},
		{"cumulative overflow", []int{1, protocolCaptureLimit}, true},
		{"empty at capacity", []int{protocolCaptureLimit, 0}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var capture protocolCapture
			total := 0
			for _, n := range tc.writes {
				total += n
				if got, err := capture.Write(bytes.Repeat([]byte("x"), n)); err != nil || got != n {
					t.Fatalf("write=%d,%v", got, err)
				}
			}
			data, truncated := capture.snapshot()
			if len(data) != min(total, protocolCaptureLimit) || truncated != tc.lost {
				t.Fatalf("capture=%d truncated=%v", len(data), truncated)
			}
		})
	}
}

// Regression inventory added before the second-review implementation below.
// These are source-only tests-first changes; no failing/passing run is claimed.
func TestFocusedProtocolCaptureEOF(t *testing.T)       { protocolCheckCaptureEOF(t) }
func TestFocusedProtocolEventTermination(t *testing.T) { protocolCheckEventTermination(t) }
func TestFocusedProtocolEventByteBudget(t *testing.T) {
	for _, total := range []int{protocolEventLimit - 1, protocolEventLimit, protocolEventLimit + 1} {
		t.Run(strconv.Itoa(total), func(t *testing.T) {
			root := protocolPrivateRoot(t)
			events := &protocolEvents{root: root, done: make(chan struct{})}
			events.drain(strings.NewReader(strings.Repeat("x", total)))
			if err := events.health.failure(); err == nil ||
				err.Error() != "oversized event frame" {
				t.Fatalf("first sticky error lost: %v", err)
			}
			if total <= protocolEventLimit {
				if events.drainErr != nil {
					t.Fatalf("overflow before byte budget exceeded: %v", events.drainErr)
				}
			} else if events.drainErr == nil || events.drainErr.Error() != "event journal overflow" {
				t.Fatalf(
					"continued discard failed to record distinct terminal overflow: %v",
					events.drainErr,
				)
			}
		})
	}
}

func TestFocusedProtocolPendingContinuation(t *testing.T) {
	for _, name := range []string{"queued wakes hold", "queued barrier abort", "grant during later pending read", "interruption skips grant"} {
		t.Run(name, func(t *testing.T) { protocolCheckPendingContinuation(t, name) })
	}
}

func TestFocusedProtocolPulseLifecycle(t *testing.T) {
	for _, name := range []string{"sole LF bytes", "queued bytes are not grants", "budget", "deadline", "backpressure", "EPIPE before close event", "validated close", "cancel join", "cancel blocked write joins"} {
		t.Run(name, func(t *testing.T) { protocolCheckPulseLifecycle(t, name) })
	}
}

func TestFocusedProtocolIdentityRecords(t *testing.T) {
	for _, name := range []string{"exact", "FIFO", "directory", "symlink", "replacement", "oversized", "malformed", "NUL", "trailing newline"} {
		t.Run(name, func(t *testing.T) { protocolCheckIdentityRecord(t, name) })
	}
}

func TestFocusedProtocolOuterIdentity(t *testing.T) {
	for _, name := range []string{"startup", "runner", "descendant", "wrong group", "publication failure prevents spawn", "spawn-intent failure prevents spawn"} {
		t.Run(name, func(t *testing.T) { protocolCheckOuterIdentity(t, name) })
	}
}

func TestFocusedProtocolAuthorizationHealth(t *testing.T) {
	for _, name := range []string{"malformed after cancellation", "overflow after cancellation", "lone return", "wrong anchor", "wrong sequence", "wrong action", "pulse failure", "EOF after cancellation", "EOF with outstanding entry", "empty channel loss"} {
		t.Run(name, func(t *testing.T) { protocolCheckAuthorizationHealth(t, name) })
	}
}
func TestFocusedProtocolNegativeRetention(t *testing.T) { protocolCheckNegativeRetention(t) }
func TestFocusedProtocolFatalOwner(t *testing.T)        { protocolCheckFatalOwner(t) }
func TestFocusedProtocolStartFailure(t *testing.T)      { protocolCheckStartFailure(t) }

// Private fatal path in the existing test binary, not a helper executable.
func TestFocusedProtocolFatalOwnerFixture(t *testing.T) {
	root := os.Getenv("FOCUSED_PROTOCOL_FATAL_ROOT")
	if root == "" {
		return
	}
	var owner *protocolOwner
	t.Cleanup(func() {
		if owner == nil {
			t.Error("owner absent")
			return
		}
		calls := -1
		if _, waited := owner.wait(0); waited {
			calls = owner.waitCalls
		}
		record := fmt.Sprintf(
			"wait-calls=%d;complete=%v;retained=%v",
			calls,
			owner.complete,
			owner.retain,
		)
		if err := publishProtocolRecord(filepath.Join(root, "fatal-unwind"), record); err != nil {
			t.Error(err)
		}
	})
	owner = protocolScript(t, root, "protocol_gate\nexit 9\n")
	state := protocolState{"reader", filepath.Base(root), "absent"}
	if err := waitProtocolWake(owner, state, time.Second); err != nil {
		t.Fatal(err)
	}
	t.Fatal("intentional private fatal owner unwind")
}
func TestFocusedProtocolCleanupUncertainty(t *testing.T) { protocolCheckCleanupUncertainty(t) }
func TestFocusedProtocolEventChannel(t *testing.T) {
	protocolCheckEventChannel(t)
	t.Run("Go fixture boundary does not start pulses", protocolCheckFixtureBoundary)
}

// Pause at protocol boundaries in a private copy, not through production test
// switches. Log every wrapper signal: a completed runner PID is never a target.
type protocolCase struct {
	name     string
	boundary string
	signal   syscall.Signal
	status   int
	want     int
	forward  bool
}

func TestFocusedTaskProtocol(t *testing.T) {
	for _, tc := range []protocolCase{
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
		if !t.Run(tc.name, func(t *testing.T) { runProtocolCase(t, tc, false, false) }) {
			return
		}
	}
}

func runProtocolCase(t *testing.T, tc protocolCase, sensitivity, noPulse bool) {
	root := protocolPrivateRoot(t)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("task.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	text = instrumentProtocolWrapper(t, text)
	// Builtin events use FD9; Go publishes completed immutable records.
	// Production-derived job-control state is unchanged.

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
	cmd := exec.Command("bash", wrapper, "./fixture", "TestPlain")
	cmd.Env = protocolPrivateEnv(
		root,
		"PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FOCUSED_PROTOCOL_ROOT="+root,
		"FOCUSED_PROTOCOL_BINARY="+binary,
		"FOCUSED_PROTOCOL_BOUNDARY="+tc.boundary,
		"FOCUSED_PROTOCOL_STATUS="+strconv.Itoa(tc.status),
		"FOCUSED_PROTOCOL_EPOCH="+filepath.Base(root),
		"FOCUSED_PROTOCOL_STRICT=1",
	)
	owner := startProtocolOwner(t, root, cmd, protocolPolicy{retain: true, noPulse: noPulse})
	if sensitivity {
		state := waitProtocolBoundary(t, root, tc.boundary)
		owner.noAnchor = state.anchor == "absent"
		err := waitProtocolWake(owner, state, 2500*time.Millisecond)
		if noPulse {
			var missing *protocolMissingWake
			if !errors.As(err, &missing) {
				t.Fatalf("negative control expected missing wake, got %v", err)
			}
			assertProtocolHeld(t, root)
			if _, err := owner.pulse.writer.Stat(); err != nil {
				t.Fatalf("no-pulse writer was not held open: %v", err)
			}
			owner.events.health.mu.Lock()
			sent := owner.pulse.sent
			owner.events.health.mu.Unlock()
			if sent != 0 {
				t.Fatal("no-pulse negative sent bytes")
			}
			owner.retain = true // Expected typed failure, not a failing Go assertion.
			owner.expectedAbort = true
			owner.cleanup()
			if !owner.complete {
				t.Fatalf("negative cleanup uncertain: %s", owner.reason)
			}
			t.Logf("expected missing-wake negative; journal retained: %s", root)
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		assertProtocolHeld(t, root)
		if tc.signal == 0 {
			owner.expectedAbort = true
			owner.cleanup()
			if !owner.complete {
				t.Fatalf("wake-only teardown uncertain: %s", owner.reason)
			}
			return
		}
	}
	if tc.signal == 0 && tc.boundary == "partial readiness" {
		state := waitProtocolBoundary(t, root, tc.boundary)
		continueProtocol(t, owner, state)
	}
	if tc.signal != 0 {
		state := waitProtocolBoundary(t, root, tc.boundary)
		owner.noAnchor = state.anchor == "absent"
		if tc.boundary == "before anchor" || tc.boundary == "transition" {
			if err := waitProtocolWake(owner, state, 2500*time.Millisecond); err != nil {
				t.Fatal(err)
			}
			assertProtocolHeld(t, root)
		}
		if tc.boundary == "completion" || tc.boundary == "finished" {
			data, err := readProtocolRegular(filepath.Join(root, "anchor"), 32)
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
		if err := owner.events.health.failure(); err != nil {
			t.Fatal(err)
		}
		if err := cmd.Process.Signal(tc.signal); err != nil {
			t.Fatal(err)
		}
		cancelled := 128 + int(tc.signal)
		if tc.boundary == "finished" {
			cancelled = 0
		}
		waitProtocolExact(
			t,
			filepath.Join(root, "cancel-1"),
			protocolFrame(
				tc.boundary,
				filepath.Base(root),
				state.anchor,
				"cancelled="+strconv.Itoa(cancelled),
			),
		)
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
			waitProtocolExact(
				t,
				filepath.Join(root, "received"),
				protocolFrame(tc.boundary, filepath.Base(root), state.anchor, "received"),
			)
			if err := cmd.Process.Signal(tc.signal); err != nil {
				t.Fatal(err)
			}
			waitProtocolExact(
				t,
				filepath.Join(root, "received-again"),
				protocolFrame(tc.boundary, filepath.Base(root), state.anchor, "received-again"),
			)
		}
		// startup/transition/bad READY teardown must not need continuation.
		// Before BUILDING, pending cancellation instead needs startup to
		// reach its production ownership acknowledgment. Runner/finished
		// gates release only after their completed cancellation record.
		if protocolNeedsContinuation(tc.boundary) {
			continueProtocol(t, owner, state)
			owner.noAnchor = false
		}
	}
	if err, waited := owner.wait(5 * time.Second); waited {
		code := 0
		if err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatal(err)
			}
			code = exitErr.ExitCode()
		}
		if code != tc.want {
			t.Fatalf(
				"exit = %d, want %d; output (not final until cleanup): %s",
				code,
				tc.want,
				owner.outputText(),
			)
		}
	} else {
		t.Fatal("wrapper did not complete protocol")
	}
	owner.cleanup()
	if !owner.complete {
		t.Fatalf("cleanup uncertain; retained %s: %s", root, owner.reason)
	}
	if tc.boundary == "before anchor" {
		waitProtocolExact(
			t,
			filepath.Join(root, "closed"),
			protocolFrame(tc.boundary, filepath.Base(root), "absent", "closed"),
		)
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
	anchor, err := readProtocolRegular(filepath.Join(root, "anchor"), 32)
	if err != nil && !protocolRecordIsPending(err) {
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
		label := "incomplete: writer quiescence not established"
		if ready, err := readProtocolRecord(
			filepath.Join(root, "cleanup-complete"),
			"quiescent",
		); err == nil &&
			ready {
			label = "final bounded tail"
		}
		for _, name := range []string{"wrapper-trace", "process-events"} {
			data, err := protocolReadTail(filepath.Join(root, name))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				t.Logf("read %s: %v", name, err)
				continue
			}
			t.Logf("%s (%s):\n%s", name, label, data)
		}
	})
	return wrapper
}

func protocolReadTail(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	if info.Size() > protocolCaptureLimit {
		if _, err := f.Seek(-protocolCaptureLimit, io.SeekEnd); err != nil {
			return nil, errors.Join(err, f.Close())
		}
	}
	data, readErr := io.ReadAll(io.LimitReader(f, protocolCaptureLimit))
	return data, errors.Join(readErr, f.Close())
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
	// The parent publishes before permitting production START. The runner
	// derives its actual outer group, validates that immutable record, then
	// exports the value BEFORE any descendant Start (not in the parent shell).
	if err := protocolBindOuter(root); err != nil {
		return 9
	}
	if err := protocolFixtureIdentity(root, "protocol-runner", 0); err != nil {
		return 9
	}
	boundary := os.Getenv("FOCUSED_PROTOCOL_BOUNDARY")
	if boundary == "partial ack open" || boundary == "short invalid ack open" {
		// Exercise the real handshake and child-start barrier, not a replica.
		// This is a SOURCE-BACKED return milestone, not direct Start/Wait
		// instrumentation. Only this exact readiness diagnostic proves the
		// unique pre-Start refusal path in execute; every other gap is unknown.
		var diagnostic protocolCapture
		code := run(
			[]string{"./fixture", "TestPlain"},
			os.Stdout,
			io.MultiWriter(os.Stderr, &diagnostic),
		)
		data, truncated := diagnostic.snapshot()
		ack := "ACST"
		if boundary == "short invalid ack open" {
			ack = "A\nST"
		}
		expected := fmt.Sprintf(
			"focused: wrapper readiness: invalid focused wrapper acknowledgment %q\n",
			ack,
		)
		if code == 1 && !truncated && string(data) == expected {
			// One complete witness already proves real run return=1 AND its
			// unique pre-Start path. Do not require a second publication that
			// an in-flight signal can prevent after this witness commits.
			if err := publishProtocolRecord(
				filepath.Join(root, "pre-start-refusal"),
				expected,
			); err != nil {
				return 9
			}
			return code
		}
		if err := publishProtocolRecord(
			filepath.Join(root, "real-run-return"),
			strconv.Itoa(code),
		); err != nil {
			return 9
		}
		return code
	}
	if boundary == "startup failure" {
		return 7
	}
	state := protocolState{
		boundary,
		os.Getenv("FOCUSED_PROTOCOL_EPOCH"),
		os.Getenv("FOCUSED_PROTOCOL_ANCHOR"),
	}
	gate := func() error {
		ready := protocolFrame(state.phase, state.epoch, state.anchor, "ready")
		if boundary != "waiting" {
			if err := publishProtocolRecord(filepath.Join(root, "boundary"), ready); err != nil {
				return err
			}
		}
		for {
			ready, err := readProtocolRecord(
				filepath.Join(root, "continue"),
				protocolFrame(state.phase, state.epoch, state.anchor, "continue"),
			)
			if err != nil {
				return err
			}
			if ready {
				return nil
			}
			time.Sleep(10 * time.Millisecond)
		}
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
		if err := protocolFixtureIdentity(root, "protocol-spawn-intent", 0); err != nil {
			return 9
		}
		if err := child.Start(); err != nil {
			return 9
		}
		defer func() { _ = child.Wait() }()
		if err := protocolFixtureIdentity(root, "protocol-spawn", child.Process.Pid); err != nil {
			return 9
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			ready, err := readProtocolRecord(
				filepath.Join(root, "ready"),
				protocolFrame(state.phase, state.epoch, state.anchor, "descendant-ready"),
			)
			if err != nil {
				return 9
			}
			if ready {
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
		if err := publishProtocolRecord(
			filepath.Join(root, "boundary"),
			protocolFrame(state.phase, state.epoch, state.anchor, "ready"),
		); err != nil {
			return 9
		}
		<-signals
		if err := publishProtocolRecord(
			filepath.Join(root, "received"),
			protocolFrame(state.phase, state.epoch, state.anchor, "received"),
		); err != nil {
			return 9
		}
		<-signals
		if err := publishProtocolRecord(
			filepath.Join(root, "received-again"),
			protocolFrame(state.phase, state.epoch, state.anchor, "received-again"),
		); err != nil {
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

// No command substitution, timeout, or newline stripping is used for framing.
// read -d ” succeeds at NUL (reject); EOF must assign ALL bytes and return 1.
// Those Bash3.2 semantics are an unexecuted native prerequisite, not an inference
// from the host's version string. FD8 has exactly one Go producer of LF bytes.
// Bash may discard NUL: this is not an arbitrary malformed-byte detector.
//
//nolint:dupword // Wire tuples intentionally repeat event kind and exact payload.
const protocolShell = `
protocol_frame() {
  protocol_expected="phase=$FOCUSED_PROTOCOL_BOUNDARY
 epoch=$FOCUSED_PROTOCOL_EPOCH
 anchor=${anchor:-absent}
 action=$1"
}
protocol_event() {
  printf '%s|%s|%s|%s|%s\n' "$1" "$FOCUSED_PROTOCOL_BOUNDARY" "$FOCUSED_PROTOCOL_EPOCH" "${anchor:-absent}" "$2" >&9
}
protocol_record() {
  local record record_code
  record=
  [ -f "$1" ] && [ ! -L "$1" ] || return 1
  IFS= read -r -d '' record < "$1"
  record_code=$?
  [ "$record_code" -eq 1 ] && [ -n "$record" ] && [ "$record" = "$2" ]
}
protocol_classify() {
  protocol_class=failure
  if [ -z "$gate_data" ]; then
    case "$gate_code:$cancelled" in
      0:*) protocol_class=wake ;;
      130:130|143:143)
        if [ "${protocol_cancel_count:-0}" -gt "${protocol_read_cancel_count:-0}" ]; then protocol_class=interrupted; fi ;;
      130:0|143:0)
        if [ "$stage" = finished ] && [ "${protocol_cancel_count:-0}" -gt "${protocol_read_cancel_count:-0}" ]; then protocol_class=interrupted; fi
        ;;
    esac
  fi
}
protocol_park() {
  # Never return into production after a latched failure. No tight EOF loop.
  # sleep is foreground work; forced termination during it is uncertain.
  while :; do sleep 1; done
}
protocol_gate() {
  [ "${protocol_used:-0}" -eq 0 ] || return 0
  protocol_used=1
  protocol_failed=0
  protocol_iteration=0
  if ! { : <&8; } 2>/dev/null; then protocol_event failure open; protocol_park; fi
  protocol_event boundary ready || protocol_park
  while :; do
    protocol_iteration=$((protocol_iteration + 1))
    [ "$protocol_iteration" -le 128 ] || { protocol_event failure budget; protocol_park; }
    protocol_read_cancel_count=${protocol_cancel_count:-0}
    protocol_event "read-enter-$protocol_iteration" "stage=$stage;cancelled=$cancelled" || protocol_park
    gate_data=
    gate_code=
    IFS= read -r gate_data <&8
    gate_code=$?
    protocol_classify
    protocol_event "read-return-$protocol_iteration" "stage=$stage;cancelled=$cancelled;code=$gate_code;class=$protocol_class" || protocol_park
    if [ "$protocol_class" = interrupted ]; then continue; fi
    if [ "$protocol_class" != wake ]; then
      protocol_failed=1
      protocol_event failure read || :
      protocol_park
    fi
    if [ -e "$FOCUSED_PROTOCOL_ROOT/continue" ]; then
      protocol_frame continue
      if ! protocol_record "$FOCUSED_PROTOCOL_ROOT/continue" "$protocol_expected"; then
        protocol_failed=1
        protocol_event failure record || :
        protocol_park
      fi
      [ "$protocol_failed" -eq 0 ] || protocol_park
      if [ "$FOCUSED_PROTOCOL_BOUNDARY" = 'before anchor' ]; then
        [ "$cancelled" -ne 0 ] || { protocol_event failure early-continue; protocol_park; }
      fi
      exec 8<&-
      if { : <&8; } 2>/dev/null; then protocol_event failure descriptor; protocol_park; fi
      protocol_event closed closed || protocol_park
      return 0
    fi
  done
}
protocol_cancel_event() {
  protocol_cancel_count=$((${protocol_cancel_count:-0} + 1))
  protocol_event "cancel-$protocol_cancel_count" "cancelled=$cancelled" || protocol_park
}
kill() {
  # Retain the original forwarding oracle; diagnostic errors now latch.
  printf '%s %s %s\n' "$@" >> "$FOCUSED_PROTOCOL_ROOT/signals" || protocol_park
  printf '%s\n' "$anchor" > "$FOCUSED_PROTOCOL_ROOT/anchor" || protocol_park
  builtin kill "$@"
}
`

func instrumentProtocolWrapper(t *testing.T, text string) string {
	t.Helper()
	replace := func(old, next string) {
		if strings.Count(text, old) != 1 {
			t.Fatalf("missing unique protocol seam %q", old)
		}
		text = strings.Replace(text, old, next, 1)
	}
	replace("cancelled=0", protocolShell+"\ncancelled=0")
	replace(
		"if [ \"$cancelled\" -eq 0 ]; then cancelled=$1; fi",
		"if [ \"$cancelled\" -eq 0 ]; then cancelled=$1; fi\n  protocol_cancel_event",
	)
	replace(
		"[ \"$stage\" = finished ] && return",
		"[ \"$stage\" = finished ] && { protocol_cancel_event; return; }",
	)
	// The anchor and all its descendants inherit neither private endpoint.
	// The wrapper retains FD8 for later gates; before-anchor closes on grant.
	// No monitor-state change or late parent export is introduced.
	replace(
		") &\nanchor=$!",
		") 8<&- 9>&- &\nanchor=$!\nprotocol_event anchor started || protocol_park\nprintf '%s\\n' \"$anchor\" > \"$FOCUSED_PROTOCOL_ROOT/anchor\" || protocol_park",
	)
	//nolint:dupword // The two words are the event kind and its exact payload.
	replace("\nfinish\nif", "\nfinish\nprotocol_event reaped reaped || protocol_park\nif")
	// stop_build/startup exit inside finish's caller, not the final path.
	replace(
		"# EXIT removes only this invocation's private directory, synchronously.",
		"protocol_event anchor-waited waited || protocol_park\n  # EXIT removes only this invocation's private directory, synchronously.",
	)
	return text
}

func protocolFrame(phase, epoch, anchor, action string) string {
	return fmt.Sprintf("phase=%s\n epoch=%s\n anchor=%s\n action=%s", phase, epoch, anchor, action)
}

func protocolExact(data []byte, expected string) bool { return bytes.Equal(data, []byte(expected)) }

// Link, unlike Rename, cannot replace an existing final name. Temporary files
// are retained as publication evidence; no consumer accepts them as records.
func publishProtocolRecord(path, record string) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".publication-")
	if err != nil {
		return err
	}
	n, writeErr := io.WriteString(f, record)
	closeErr := f.Close()
	if n != len(record) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return os.Link(f.Name(), path)
}

// No-follow/nonblocking open prevents a swapped FIFO from hanging cleanup.
// Descriptor and path checks bracket a bounded read. This is not an atomic
// snapshot against uncooperative same-inode writers or ancestor replacement.
func readProtocolRegular(path string, limit int) ([]byte, error) {
	return readProtocolRegularChecked(path, limit, nil)
}

// Only this bare marker means the first pathname observation found no record.
// Native causes remain unwrap-able for diagnostics, but post-observation loss,
// wrapping or joining may never acquire permission to poll/retry a replacement.
type protocolInitialRecordAbsence struct{ cause error }

func (e *protocolInitialRecordAbsence) Error() string { return e.cause.Error() }
func (e *protocolInitialRecordAbsence) Unwrap() error { return e.cause }
func protocolRecordIsPending(err error) bool {
	//nolint:errorlint // Only our bare initial-observation marker permits polling, not wrapped/joined failures.
	absent, ok := err.(*protocolInitialRecordAbsence)
	return ok && absent != nil
}

func readProtocolRegularChecked(path string, limit int, afterOpen func()) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, &protocolInitialRecordAbsence{cause: err}
		}
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular record: %s", path)
	}
	fd, err := syscall.Open(
		path,
		syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("record open after observation %q: %w", path, err)
	}
	f := os.NewFile(uintptr(fd), path)
	if afterOpen != nil {
		afterOpen()
	} // Private deterministic replacement test seam.
	check := func() error {
		info, statErr := f.Stat()
		current, pathErr := os.Lstat(path)
		if err := errors.Join(statErr, pathErr); err != nil {
			return fmt.Errorf("record check after open %q: %w", path, err)
		}
		if !info.Mode().IsRegular() || !current.Mode().IsRegular() || !os.SameFile(before, info) ||
			!os.SameFile(info, current) ||
			info.Size() != before.Size() ||
			!info.ModTime().Equal(before.ModTime()) {
			return fmt.Errorf("record identity/type changed: %s", path)
		}
		return nil
	}
	if err := check(); err != nil {
		return nil, errors.Join(err, f.Close())
	}
	data, readErr := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	checkErr := check()
	if err := errors.Join(readErr, checkErr, f.Close()); err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("oversized record: %s", path)
	}
	return data, nil
}

func readProtocolRecord(path, expected string) (bool, error) {
	return readProtocolRecordChecked(path, expected, nil)
}

func readProtocolRecordChecked(path, expected string, afterOpen func()) (bool, error) {
	data, err := readProtocolRegularChecked(path, len(expected), afterOpen)
	if protocolRecordIsPending(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !protocolExact(data, expected) {
		return false, fmt.Errorf("record %s: got %q, want %q", path, data, expected)
	}
	return true, nil
}

func waitProtocolExact(t *testing.T, path, expected string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		ready, err := readProtocolRecord(path, expected)
		if err != nil {
			t.Fatal(err)
		}
		if ready {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("completed record not reached: %s; want %q", path, expected)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type protocolState struct{ phase, epoch, anchor string }

func waitProtocolBoundary(t *testing.T, root, phase string) protocolState {
	t.Helper()
	state := protocolState{phase, filepath.Base(root), "absent"}
	if phase != "before anchor" {
		deadline := time.Now().Add(5 * time.Second)
		for {
			data, err := readProtocolRegular(filepath.Join(root, "anchor-record"), 512)
			if err == nil {
				prefix := fmt.Sprintf("phase=%s\n epoch=%s\n anchor=", phase, state.epoch)
				suffix := "\n action=started"
				if !strings.HasPrefix(string(data), prefix) ||
					!strings.HasSuffix(string(data), suffix) {
					t.Fatalf("malformed completed anchor record %q", data)
				}
				raw := string(data[len(prefix) : len(data)-len(suffix)])
				pid, parseErr := strconv.Atoi(raw)
				if parseErr != nil || pid <= 1 || strconv.Itoa(pid) != raw {
					t.Fatalf("invalid completed anchor identity %q", data)
				}
				state.anchor = raw
				// Parsing only discovers the dynamic field; acceptance below
				// compares the ENTIRE regular record, not a prefix or trimmed PID.
				break
			}
			if !protocolRecordIsPending(err) {
				t.Fatal(err)
			}
			if time.Now().After(deadline) {
				t.Fatal("anchor start record absent")
			}
			time.Sleep(10 * time.Millisecond)
		}
		waitProtocolExact(
			t,
			filepath.Join(root, "anchor-record"),
			protocolFrame(phase, state.epoch, state.anchor, "started"),
		)
		pid, _ := strconv.Atoi(state.anchor)
		if group, err := syscall.Getpgid(pid); err != nil || group != pid {
			t.Fatalf(
				"current anchor group not validated: pid=%d group=%d error=%v",
				pid,
				group,
				err,
			)
		}
	}
	waitProtocolExact(
		t,
		filepath.Join(root, "boundary"),
		protocolFrame(phase, state.epoch, state.anchor, "ready"),
	)
	return state
}

func continueProtocol(t *testing.T, owner *protocolOwner, state protocolState) {
	t.Helper()
	if err := owner.authorize(state); err != nil {
		t.Fatal(err)
	}
}

func (o *protocolOwner) authorize(state protocolState, pendingRead ...int) (err error) {
	// Linearizes with observed event and pulse failures, not future/unread bytes.
	o.events.health.mu.Lock()
	defer o.events.health.mu.Unlock()
	defer func() { o.events.health.failLocked(err) }()
	if o.events.health.err != nil {
		return o.events.health.err
	}
	if o.events.health.eventEnded {
		return errors.New("event observations ended")
	}
	if o.pulse.completed {
		return errors.New("wrapper already completed")
	}
	if o.pulse.stopped {
		return errors.New("pulse owner stopped")
	}
	if len(pendingRead) != 0 {
		// Private pending-read test assertion, NOT a per-return grant protocol.
		// Under this same mutex no writer can begin a new send. Prior paired
		// returns consumed every sent LF; no in-flight/queued byte can end the
		// next read before this publication. Entry is a source-level milestone,
		// not a claim to observe entry into the kernel's read syscall.
		n := pendingRead[0]
		if o.pulse.writing || o.pulse.sent != n-1 || o.events.entries != n ||
			o.events.returns != n-1 {
			return errors.New("pending-read publication window not established")
		}
	}
	root := o.root
	if state.phase != o.events.phase || state.epoch != o.events.epoch ||
		state.anchor != o.events.currentAnchor() {
		return errors.New("continuation state mismatch")
	}
	if ready, err := readProtocolRecord(
		filepath.Join(root, "boundary"),
		protocolFrame(state.phase, state.epoch, state.anchor, "ready"),
	); err != nil ||
		!ready {
		return errors.Join(
			fmt.Errorf("continuation boundary not exact: %q", filepath.Join(root, "boundary")),
			err,
		)
	}
	if o.events.closed {
		return errors.New("gate already closed")
	}
	// Other phases may publish while their first read is pending. The next
	// benign wake checks the record; no per-return acknowledgment is needed.
	if state.phase == "before anchor" && (o.events.wakes == 0 || o.pulse.sent == 0) {
		return errors.New("before-anchor wake not consumed")
	}
	if state.phase == "before anchor" {
		// Go cannot authorize early startup: require exact post-assignment
		// cancellation evidence independently of the shell's own check.
		data, err := readProtocolRegular(filepath.Join(root, "cancel-1"), 512)
		if err != nil {
			return err
		}
		if !protocolExact(
			data,
			protocolFrame(state.phase, state.epoch, state.anchor, "cancelled=130"),
		) &&
			!protocolExact(
				data,
				protocolFrame(state.phase, state.epoch, state.anchor, "cancelled=143"),
			) {
			return fmt.Errorf("no completed pending cancellation: %q", data)
		}
	}
	return publishProtocolRecord(
		filepath.Join(root, "continue"),
		protocolFrame(state.phase, state.epoch, state.anchor, "continue"),
	)
}

func protocolNeedsContinuation(phase string) bool {
	switch phase {
	case "before anchor",
		"build readiness",
		"ack transition",
		"ack write",
		"partial ack open",
		"short invalid ack open",
		"completion",
		"finished",
		"waiting":
		return true
	default:
		// startup, transition, malformed/partial/unterminated READY: the
		// production startup trap aborts without releasing any fixture gate.
		return false
	}
}

type protocolMissingWake struct{ path string }

func (e *protocolMissingWake) Error() string { return "missing actual pulse/read wake: " + e.path }

func waitProtocolWake(o *protocolOwner, state protocolState, bound time.Duration) error {
	path := filepath.Join(o.root, "read-return-1")
	stage := protocolGateStage(state.phase)
	entry := protocolFrame(state.phase, state.epoch, state.anchor, "stage="+stage+";cancelled=0")
	expected := protocolFrame(
		state.phase,
		state.epoch,
		state.anchor,
		"stage="+stage+";cancelled=0;code=0;class=wake",
	)
	deadline := time.Now().Add(bound)
	for {
		o.events.health.mu.Lock()
		err, sent := o.events.health.err, o.pulse.sent
		o.events.health.mu.Unlock()
		if err != nil {
			return err
		}
		entered, err := readProtocolRecord(filepath.Join(o.root, "read-enter-1"), entry)
		if err != nil {
			return err
		}
		ready, err := readProtocolRecord(path, expected)
		if err != nil {
			return err
		}
		if entered && ready && sent > 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return &protocolMissingWake{path}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func assertProtocolHeld(t *testing.T, root string) {
	t.Helper()
	for _, name := range []string{"continue", "closed", "work-started", "signals", "failure"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("gate advanced/failed before authorization: %s: %v", name, err)
		}
	}
	trace, err := os.ReadFile(filepath.Join(root, "wrapper-trace"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(trace), "+focused: printf 'ACK\\n'") {
		t.Fatal("ACK before gate release")
	}
}

// 120 one-byte pulses at most, one per 50ms with no catch-up; 8s total
// deadline and 50ms per Write. Shell caps reads at 128 (120 wakes + at most
// 8 evidenced interruptions). At most 270 frames of <=512 bytes plus LF fit
// the 256KiB drain budget, including 8 cancellation and 6 lifecycle/failure
// frames. Reframed immutable records are <=540 bytes each (<146KiB total).
// Queued wakes can burst but cannot grant. Event temporary+final hardlinks
// share the same bounded payload. Wrapper trace is bounded by these iterations
// and the owner's teardown lifetime, not represented as bounded capture.
const (
	protocolPulseBudget = 120
	protocolEventLimit  = 256 * 1024
)

type protocolHealth struct {
	mu         sync.Mutex
	err        error
	eventEnded bool // Terminal observation loss, including clean EOF; not framing failure.
}

func (h *protocolHealth) failLocked(err error) {
	if err != nil && h.err == nil {
		h.err = err
	}
}
func (h *protocolHealth) failure() error { h.mu.Lock(); defer h.mu.Unlock(); return h.err }

type protocolPulse struct {
	writer                         *os.File // Never passed through ExtraFiles or any child's environment.
	health                         *protocolHealth
	done, cancel                   chan struct{}
	startOnce, stopOnce, closeOnce sync.Once
	closeErr                       error
	sent                           int  // Under health.mu; successful enqueueing, NOT consumed reads.
	writing                        bool // Also under health.mu; bounds/close coverage for pending Write.
	closed, completed, stopped     bool // Validated gate closure / consumed Wait / owner stop.
	disabled                       bool
	period, lifetime, writeBound   time.Duration
	budget                         int
}

func newProtocolPulse(w *os.File, health *protocolHealth, disabled bool) *protocolPulse {
	return &protocolPulse{
		writer:     w,
		health:     health,
		done:       make(chan struct{}),
		cancel:     make(chan struct{}),
		disabled:   disabled,
		period:     50 * time.Millisecond,
		lifetime:   8 * time.Second,
		writeBound: 50 * time.Millisecond,
		budget:     protocolPulseBudget,
	}
}

func (p *protocolPulse) closeWriter() error {
	p.closeOnce.Do(func() { p.closeErr = p.writer.Close() })
	return p.closeErr
}

func (p *protocolPulse) start() {
	if p.disabled {
		return
	}
	p.startOnce.Do(func() { go p.run() })
}

func (p *protocolPulse) run() {
	defer close(p.done)
	deadline := time.Now().Add(p.lifetime)
	for i := 0; i < p.budget; i++ {
		// Restart the timer after each write: slow consumers never cause
		// catch-up sends. Even a full pipe has an independent write deadline.
		interval := p.period
		if remaining := time.Until(deadline); remaining < interval {
			interval = max(remaining, 0)
		}
		timer := time.NewTimer(interval)
		select {
		case <-p.cancel:
			timer.Stop()
			return
		case <-timer.C:
		}
		p.health.mu.Lock()
		stop := p.closed || p.completed || p.stopped || p.health.err != nil || p.health.eventEnded
		if !stop {
			p.writing = true
		}
		p.health.mu.Unlock()
		if stop {
			return
		}
		bound := time.Now().Add(p.writeBound)
		if deadline.Before(bound) {
			bound = deadline
		}
		err := p.writer.SetWriteDeadline(bound)
		n := 0
		if err == nil {
			n, err = p.writer.Write([]byte{'\n'})
		}
		p.health.mu.Lock()
		p.writing = false
		if err == nil && n != 1 {
			err = io.ErrShortWrite
		}
		if err == nil {
			p.sent++
		}
		// No optimistic EPIPE exception. If FD9 close evidence is still in
		// flight, this remains a sticky unknown even after that event arrives.
		if err != nil && !p.closed && !p.completed && !p.stopped {
			p.health.failLocked(fmt.Errorf("pulse producer: %w", err))
		}
		p.health.mu.Unlock()
		if err != nil {
			return
		}
	}
	p.health.mu.Lock()
	if !p.closed && !p.completed && !p.stopped {
		p.health.failLocked(errors.New("pulse total budget exhausted"))
	}
	p.health.mu.Unlock()
}

func (p *protocolPulse) stopAndJoin(bound time.Duration) error {
	p.health.mu.Lock()
	p.stopped = true
	p.health.mu.Unlock()
	p.stopOnce.Do(func() { close(p.cancel) })
	closeErr := p.closeWriter() // Interrupts any pending pollable-pipe Write.
	// Also closes done when no shell boundary ever started a producer.
	p.startOnce.Do(func() { close(p.done) })
	select {
	case <-p.done:
		return closeErr
	case <-time.After(bound):
		return errors.Join(closeErr, &protocolCleanupUncertain{"pulse producer did not join"})
	}
}

const protocolCaptureLimit = 16 * 1024

type protocolCapture struct {
	mu                         sync.Mutex
	data                       []byte
	truncated                  bool
	reader, writer             *os.File
	done                       chan struct{}
	naturalEOF, forced, joined bool
	drainErr, closeErr         error
	finishOnce                 sync.Once
	finishErr                  error
}

func (c *protocolCapture) start() error {
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	c.reader, c.writer, c.done = r, w, make(chan struct{})
	go func() {
		_, err := io.Copy(c, r) // Independent of cmd.Wait and its primary exit error.
		c.mu.Lock()
		c.drainErr = err
		c.naturalEOF = err == nil && !c.forced
		c.mu.Unlock()
		close(c.done)
	}()
	return nil
}

func (c *protocolCapture) finish() error {
	const bound = time.Second
	c.finishOnce.Do(func() {
		var closeErr error
		joined := false
		select {
		case <-c.done:
			joined = true
			closeErr = c.reader.Close()
		case <-time.After(bound):
			c.mu.Lock()
			c.forced = true
			c.mu.Unlock()
			closeErr = c.reader.Close()
			select {
			case <-c.done:
				joined = true
			case <-time.After(bound):
			}
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		c.joined, c.closeErr = joined, closeErr
		var reasons []string
		if c.forced {
			reasons = append(reasons, "capture reader forced closed")
		}
		if !joined {
			reasons = append(reasons, "capture drainer unjoined")
		}
		if !c.naturalEOF {
			reasons = append(reasons, "capture natural EOF not proven")
		}
		if c.drainErr != nil {
			reasons = append(reasons, "capture read: "+c.drainErr.Error())
		}
		if closeErr != nil {
			reasons = append(reasons, "capture close: "+closeErr.Error())
		}
		if c.truncated {
			reasons = append(reasons, "captured output exceeded bound")
		}
		if len(reasons) != 0 {
			c.finishErr = &protocolCleanupUncertain{strings.Join(reasons, "; ")}
		}
	})
	return c.finishErr // Sticky: late writer/group absence cannot restore bytes.
}

func (c *protocolCapture) proof() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	complete := c.joined && c.naturalEOF && !c.forced && c.drainErr == nil && !c.truncated &&
		c.finishErr == nil
	return fmt.Sprintf(
		"capture-complete=%v;natural-EOF=%v;forced-close=%v;joined=%v;read-error=%v",
		complete,
		c.naturalEOF,
		c.forced,
		c.joined,
		c.drainErr,
	)
}

func (c *protocolCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(p)
	if len(p) >= protocolCaptureLimit {
		c.truncated = c.truncated || len(c.data)+len(p) > protocolCaptureLimit
		c.data = append(c.data[:0], p[len(p)-protocolCaptureLimit:]...)
	} else {
		if extra := len(c.data) + len(p) - protocolCaptureLimit; extra > 0 {
			c.data = c.data[extra:]
			c.truncated = true
		}
		c.data = append(c.data, p...)
	}
	return n, nil
}

func (c *protocolCapture) snapshot() ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.data...), c.truncated
}

type protocolEvents struct {
	root, phase, epoch                                        string
	done                                                      chan struct{}
	health                                                    protocolHealth
	drainErr                                                  error // Terminal I/O/overflow errors; read only after done.
	pulse                                                     *protocolPulse
	anchor                                                    string // State below is protected by health.mu until done.
	gated, closed, waited                                     bool
	entries, returns, wakes, cancels, entryCancels, cancelled int
	lastClass                                                 string
}

// Drain begins before Start and continues bounded discard after EVERY error.
// The mutex linearizes observed health, sequence validation and publication
// with authorization. It cannot authenticate future/unobserved pipe bytes.
func (e *protocolEvents) drain(r io.Reader) {
	defer close(e.done)
	var chunk [1024]byte
	var frame []byte
	total := 0
	for {
		n, err := r.Read(chunk[:])
		for _, b := range chunk[:n] {
			e.health.mu.Lock()
			if total <= protocolEventLimit {
				total++
			}
			if total > protocolEventLimit && e.drainErr == nil {
				e.drainErr = errors.New("event journal overflow")
				e.health.failLocked(e.drainErr)
			}
			if e.health.err == nil {
				if b == '\n' {
					e.health.failLocked(e.publishLocked(frame))
					frame = frame[:0]
				} else {
					frame = append(frame, b)
					if len(frame) > 512 {
						e.health.failLocked(errors.New("oversized event frame"))
						frame = nil
					}
				}
			}
			e.health.mu.Unlock()
		}
		if err != nil {
			e.health.mu.Lock()
			e.health.eventEnded = true
			if err != io.EOF {
				e.drainErr = errors.Join(e.drainErr, err)
				e.health.failLocked(err)
			}
			if len(frame) != 0 {
				e.health.failLocked(errors.New("partial event at EOF"))
			}
			e.health.mu.Unlock()
			return
		}
	}
}

// Observation termination forbids grants immediately, but is not itself a
// cleanup failure: EOF can beat Wait scheduling, and startup traps need not
// emit a read-return. Ownership/topology checks remain separate in cleanup.
func (e *protocolEvents) reconcileEnd(wrapperCompleted bool) error {
	e.health.mu.Lock()
	defer e.health.mu.Unlock()
	if !e.health.eventEnded {
		return &protocolCleanupUncertain{"event observations not joined/ended"}
	}
	if !e.closed && !wrapperCompleted {
		return &protocolCleanupUncertain{
			"event observations ended without gate closure or wrapper completion",
		}
	}
	return nil
}

func protocolGateStage(phase string) string {
	switch phase {
	case "transition", "truncated readiness", "invalid readiness":
		return "startup"
	case "ack transition", "ack write", "partial ack open", "short invalid ack open", "completion":
		return "runner"
	case "finished":
		return "finished"
	default:
		return "build"
	}
}

func (e *protocolEvents) currentAnchor() string {
	if e.anchor == "" {
		return "absent"
	}
	return e.anchor
}

func protocolEventNumber(name, prefix string) int {
	raw, ok := strings.CutPrefix(name, prefix)
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 || strconv.Itoa(n) != raw {
		return 0
	}
	return n
}

func (e *protocolEvents) publishLocked(frame []byte) error {
	if bytes.IndexByte(frame, 0) >= 0 {
		return errors.New("NUL in event")
	}
	fields := strings.Split(string(frame), "|")
	if len(fields) != 5 || fields[1] != e.phase || fields[2] != e.epoch {
		return fmt.Errorf("invalid event %q", frame)
	}
	name, anchor, action := fields[0], fields[3], fields[4]
	if name == "anchor" {
		pid, err := strconv.Atoi(anchor)
		if err != nil || pid <= 1 || strconv.Itoa(pid) != anchor || e.anchor != "" ||
			(e.gated && !e.closed) ||
			action != "started" {
			return errors.New("invalid anchor transition")
		}
		if group, err := syscall.Getpgid(pid); err != nil || group != pid {
			return errors.Join(
				fmt.Errorf("current anchor group not validated: pid=%d group=%d", pid, group),
				err,
			)
		}
	} else if anchor != e.currentAnchor() {
		return errors.New("event anchor changed")
	}
	valid := false
	stage := protocolGateStage(e.phase)
	readAction := fmt.Sprintf("stage=%s;cancelled=%d", stage, e.cancelled)
	switch {
	case name == "boundary":
		valid = action == "ready" && !e.gated && !e.closed
		if e.phase != "reader" && e.phase != "before anchor" && e.anchor == "" {
			valid = false
		}
		if valid {
			e.gated = true
		}
	case name == "anchor":
		valid = true
	case name == "anchor-waited":
		valid = action == "waited" && e.anchor != "" && !e.waited
		if valid {
			e.waited = true
		}
	case name == "reaped":
		valid = action == "reaped" && e.waited
	case name == "closed":
		valid = action == "closed" && e.gated && !e.closed && e.entries == e.returns &&
			e.lastClass == "wake"
	case name == "failure":
		switch action {
		case "open", "read", "record", "early-continue", "descriptor", "budget":
			valid = true
		}
	case protocolEventNumber(name, "read-enter-") != 0:
		n := protocolEventNumber(name, "read-enter-")
		valid = e.gated && !e.closed && n == e.entries+1 && n <= 128 && e.entries == e.returns &&
			action == readAction
		if valid {
			e.entries = n
			e.entryCancels = e.cancels
		}
	case protocolEventNumber(name, "read-return-") != 0:
		n := protocolEventNumber(name, "read-return-")
		parts := strings.Split(action, ";")
		valid = e.gated && !e.closed && n == e.entries && n == e.returns+1 && len(parts) == 4
		if valid {
			raw := strings.TrimPrefix(parts[2], "code=")
			code, err := strconv.Atoi(raw)
			class := strings.TrimPrefix(parts[3], "class=")
			valid = err == nil && code >= 0 && code <= 255 && strconv.Itoa(code) == raw &&
				action == fmt.Sprintf("%s;code=%d;class=%s", readAction, code, class)
			switch class {
			case "wake":
				valid = valid && code == 0
			case "interrupted":
				valid = valid && (code == 130 || code == 143) && e.cancels > e.entryCancels &&
					(e.cancelled == code || stage == "finished")
			case "failure": // Observed nonempty bytes can fail even at status 0.
			default:
				valid = false
			}
			if valid {
				e.returns = n
				e.lastClass = class
				if class == "wake" {
					e.wakes++
				}
			}
		}
	case protocolEventNumber(name, "cancel-") != 0:
		n := protocolEventNumber(name, "cancel-")
		value := e.cancelled
		if stage == "finished" {
			value = 0
		} else if value == 0 {
			if action == "cancelled=130" {
				value = 130
			} else {
				value = 143
			}
		}
		valid = n == e.cancels+1 && n <= 8 && action == "cancelled="+strconv.Itoa(value)
		if valid {
			e.cancels = n
			e.cancelled = value
		}
	}
	if !valid {
		return fmt.Errorf("invalid event kind/action/sequence %q", frame)
	}
	published := name
	if name == "anchor" {
		published = "anchor-record"
	}
	if err := publishProtocolRecord(
		filepath.Join(e.root, published),
		protocolFrame(e.phase, e.epoch, anchor, action),
	); err != nil {
		return err
	}
	switch name {
	case "anchor":
		e.anchor = anchor
	case "boundary":
		if e.pulse != nil {
			e.pulse.start()
		}
	case "closed":
		e.closed = true
		if e.pulse != nil {
			e.pulse.closed = true
		}
	case "failure":
		return &protocolGateFailure{"shell gate failure: " + action}
	}
	if strings.HasPrefix(name, "read-return-") && e.lastClass == "failure" {
		// Failure-return is the final accepted evidence. Subsequent bytes,
		// including the shell's diagnostic failure frame, are discarded.
		return &protocolGateFailure{"shell read failed"}
	}
	return nil
}

func protocolPrivateRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "dotty-protocol-")
	if err != nil {
		t.Fatal(err)
	}
	// No automatic deletion: even a pre-Start setup failure preserves evidence.
	t.Logf("private protocol evidence: %s", root)
	return root
}

type protocolOwner struct {
	root                                           string
	cmd                                            *exec.Cmd
	pulse                                          *protocolPulse
	expectedGateFailure                            bool
	nestedRoot                                     string // Private fatal test-binary child owns one separate wrapper.
	capture                                        protocolCapture
	events                                         *protocolEvents
	eventReader                                    *os.File
	queueWriter                                    *os.File // Only the one-time queued-consumption fixture owns FD10.
	queueOnce                                      sync.Once
	queueErr                                       error
	captureDuplicate                               *os.File // Only the retained-writer negative fixture.
	duplicateOnce                                  sync.Once
	duplicateErr                                   error
	captureErr                                     error
	expectedCaptureIncomplete, processesQuiescent  bool
	done                                           chan struct{}
	waitErr                                        error
	waitCalls                                      int // Access only after done; maintained by the sole waiter.
	waitRecordErr                                  error
	cleanupOnce                                    sync.Once
	noAnchor, innerPossible, expectedAbort, retain bool
	builtinOnly                                    bool // Standalone builtins/private test fixtures; no separate groups.
	wrapperGroupValidated                          bool
	outerDescendantRequired                        bool
	complete                                       bool
	reason                                         string
}

func protocolPrivateEnv(root string, extra ...string) []string {
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "FOCUSED_PROTOCOL_") || strings.HasPrefix(key, "XDG_") {
			continue
		}
		switch key {
		case "HOME",
			"DOTTY_REPO",
			"TMPDIR",
			"TMP",
			"TEMP",
			"BASH_ENV",
			"ENV",
			"SHELLOPTS",
			"BASHOPTS",
			"CDPATH":
			continue
		}
		env = append(env, entry)
	}
	env = append(
		env,
		"HOME="+root,
		"DOTTY_REPO="+root,
		"TMPDIR="+root,
		"TMP="+root,
		"TEMP="+root,
		"XDG_CONFIG_HOME="+root,
		"XDG_DATA_HOME="+root,
		"XDG_STATE_HOME="+root,
		"XDG_CACHE_HOME="+root,
		"XDG_RUNTIME_DIR="+root,
	)
	return append(env, extra...)
}

func protocolEnv(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if value, ok := strings.CutPrefix(env[i], key+"="); ok {
			return value
		}
	}
	return ""
}

type protocolPolicy struct {
	noAnchor, retain, noPulse, builtinOnly, expectedGateFailure bool
	nestedRoot                                                  string
	pulsePeriod                                                 time.Duration // Private pending-read coverage; default is 50ms.
	queueBeforeRead                                             bool          // One-time fixture barrier, never a normal/per-return ACK.
	captureDuplicate, expectedCaptureIncomplete                 bool          // Retained-writer negative only.
}

func startProtocolOwner(
	t *testing.T,
	root string,
	cmd *exec.Cmd,
	policy protocolPolicy,
) *protocolOwner {
	t.Helper()
	owner, err := tryStartProtocolOwner(t, root, cmd, policy)
	if err != nil {
		t.Fatalf("Start/setup failed; retained %s: %v", root, err)
	}
	return owner
}

func tryStartProtocolOwner(
	t *testing.T,
	root string,
	cmd *exec.Cmd,
	policy protocolPolicy,
) (*protocolOwner, error) {
	t.Helper()
	phase := protocolEnv(cmd.Env, "FOCUSED_PROTOCOL_BOUNDARY")
	owner := &protocolOwner{
		root:                      root,
		cmd:                       cmd,
		done:                      make(chan struct{}),
		noAnchor:                  policy.noAnchor,
		retain:                    policy.retain,
		builtinOnly:               policy.builtinOnly,
		expectedGateFailure:       policy.expectedGateFailure,
		nestedRoot:                policy.nestedRoot,
		expectedCaptureIncomplete: policy.expectedCaptureIncomplete,
	}
	// Freeze possible topologies BEFORE Start, including fatal setup paths.
	owner.innerPossible = phase == "partial ack open" || phase == "short invalid ack open"
	owner.outerDescendantRequired = phase == "truncated readiness" ||
		phase == "invalid readiness" ||
		phase == "unterminated readiness" ||
		phase == "partial readiness"
	r, w, err := os.Pipe()
	if err != nil {
		return owner, err
	}
	pulseRead, pulseWrite, err := os.Pipe()
	if err != nil {
		return owner, errors.Join(err, r.Close(), w.Close())
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return owner, errors.Join(err, r.Close(), w.Close(), pulseRead.Close(), pulseWrite.Close())
	}
	if err := owner.capture.start(); err != nil {
		return owner, errors.Join(
			err,
			r.Close(),
			w.Close(),
			pulseRead.Close(),
			pulseWrite.Close(),
			null.Close(),
		)
	}
	var queueRead *os.File
	closeSetup := func() error {
		var queueReadErr error
		if queueRead != nil {
			queueReadErr = queueRead.Close()
		}
		return errors.Join(
			r.Close(),
			w.Close(),
			pulseRead.Close(),
			pulseWrite.Close(),
			null.Close(),
			queueReadErr,
			owner.releaseQueue(),
			owner.closeCaptureDuplicate(),
			owner.capture.writer.Close(),
			owner.capture.finish(),
		)
	}
	if policy.queueBeforeRead {
		queueRead, owner.queueWriter, err = os.Pipe()
		if err != nil {
			return owner, errors.Join(err, closeSetup())
		}
	}
	if policy.captureDuplicate {
		// Diagnostic-only endpoint; the original parent writer still closes
		// after Start. No normal wrapper gets a retained duplicate.
		fd, err := syscall.Dup(int(owner.capture.writer.Fd()))
		if err != nil {
			return owner, errors.Join(err, closeSetup())
		}
		syscall.CloseOnExec(fd)
		owner.captureDuplicate = os.NewFile(uintptr(fd), "retained-capture-writer")
	}
	// FD8 read endpoint only. The Go writer is never inherited. Production
	// replaces placeholders 3--5; FD9 remains event-channel ExtraFiles[6].
	cmd.ExtraFiles = []*os.File{null, null, null, null, null, pulseRead, w}
	if queueRead != nil {
		cmd.ExtraFiles = append(cmd.ExtraFiles, queueRead)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// *os.File avoids os/exec's opaque copier/WaitDelay error precedence.
	cmd.Stdout, cmd.Stderr = owner.capture.writer, owner.capture.writer
	owner.events = &protocolEvents{
		root:  root,
		phase: protocolEnv(cmd.Env, "FOCUSED_PROTOCOL_BOUNDARY"),
		epoch: protocolEnv(cmd.Env, "FOCUSED_PROTOCOL_EPOCH"),
		done:  make(chan struct{}),
	}
	owner.eventReader = r
	owner.pulse = newProtocolPulse(pulseWrite, &owner.events.health, policy.noPulse)
	owner.events.pulse = owner.pulse
	if policy.pulsePeriod != 0 {
		owner.pulse.period = policy.pulsePeriod
	}
	go owner.events.drain(r)
	if err := cmd.Start(); err != nil {
		var queueReadErr error
		if queueRead != nil {
			queueReadErr = queueRead.Close()
		}
		closeErr := errors.Join(
			w.Close(),
			null.Close(),
			pulseRead.Close(),
			queueReadErr,
			owner.releaseQueue(),
			owner.closeCaptureDuplicate(),
			owner.capture.writer.Close(),
			owner.capture.finish(),
			owner.pulse.stopAndJoin(time.Second),
			finishProtocolEvents(owner.events, r, time.Second),
		)
		recordErr := publishProtocolRecord(
			filepath.Join(root, "start-failure"),
			fmt.Sprintf("Start=%v;endpoints-and-joins=%v;no-child", err, closeErr),
		)
		return owner, errors.Join(err, closeErr, recordErr)
	}
	// Register the SINGLE owner before ANY fallible post-Start operation.
	t.Cleanup(func() {
		owner.cleanup()
		// The sole expected capture-negative exception requires every other
		// cleanup proof and the exact sticky forced-capture reason. It never
		// marks capture/cleanup complete and never permits root removal.
		owner.capture.mu.Lock()
		expectedForcedClose := owner.capture.forced && owner.capture.joined &&
			!owner.capture.naturalEOF &&
			!owner.capture.truncated &&
			owner.capture.closeErr == nil &&
			errors.Is(owner.capture.drainErr, os.ErrClosed)
		owner.capture.mu.Unlock()
		expectedIncomplete := owner.expectedCaptureIncomplete && owner.retain &&
			owner.processesQuiescent &&
			expectedForcedClose &&
			owner.captureErr != nil &&
			owner.reason == owner.captureErr.Error()
		if !owner.complete && !expectedIncomplete {
			t.Errorf("cleanup-uncertain; retained %s: %s", root, owner.reason)
		}
		outcome := "Wait incomplete"
		if err, waited := owner.wait(0); waited {
			outcome = fmt.Sprintf("Wait result=%v", err)
		}
		t.Logf(
			"protocol root=%s phase=%q epoch=%s outcome=%s cleanup-complete=%v retained=%v; identities are historical observations, never later signal authority",
			root,
			owner.events.phase,
			owner.events.epoch,
			outcome,
			owner.complete,
			owner.retain || t.Failed() || !owner.complete,
		)
		if owner.complete {
			data, truncated := owner.capture.snapshot()
			t.Logf("preserved final stdout/stderr root=%s truncated=%v:\n%s", root, truncated, data)
		}
		if owner.complete && !t.Failed() && !owner.retain {
			if err := os.RemoveAll(root); err != nil {
				t.Errorf("remove verified private root %s: %v", root, err)
			}
		} else {
			t.Logf("retained protocol evidence %s; output-complete=%v", root, owner.complete)
		}
	})
	// Validate while the direct wrapper is still unreaped; never retrieve an
	// arbitrary identity from a historical diagnostic log.
	pgid, groupErr := syscall.Getpgid(cmd.Process.Pid)
	owner.wrapperGroupValidated = groupErr == nil && pgid == cmd.Process.Pid
	go func() {
		owner.waitCalls++
		owner.waitErr = cmd.Wait()
		owner.events.health.mu.Lock()
		owner.pulse.completed = true
		owner.events.health.mu.Unlock()
		owner.waitRecordErr = publishProtocolRecord(
			filepath.Join(root, "wrapper-wait"),
			fmt.Sprintf("pid=%d;result=%v", cmd.Process.Pid, owner.waitErr),
		)
		close(owner.done)
	}()
	var queueReadErr error
	if queueRead != nil {
		queueReadErr = queueRead.Close()
	}
	if err := errors.Join(
		pulseRead.Close(),
		w.Close(),
		null.Close(),
		queueReadErr,
		owner.capture.writer.Close(),
	); err != nil {
		return owner, err
	}
	if groupErr != nil || pgid != cmd.Process.Pid {
		return owner, errors.Join(
			fmt.Errorf("wrapper group not validated: pgid=%d", pgid),
			groupErr,
		)
	}
	if err := publishProtocolRecord(
		filepath.Join(root, "wrapper-start"),
		fmt.Sprintf("pid=%d;group=%d", cmd.Process.Pid, pgid),
	); err != nil {
		return owner, err
	}
	return owner, nil
}

func (o *protocolOwner) releaseQueue() error {
	o.queueOnce.Do(func() {
		if o.queueWriter != nil {
			o.queueErr = o.queueWriter.Close()
		}
	})
	return o.queueErr
}

func (o *protocolOwner) closeCaptureDuplicate() error {
	o.duplicateOnce.Do(func() {
		if o.captureDuplicate != nil {
			o.duplicateErr = o.captureDuplicate.Close()
		}
	})
	return o.duplicateErr
}

func (o *protocolOwner) wait(bound time.Duration) (error, bool) {
	select {
	case <-o.done:
		return o.waitErr, true
	default:
	}
	if bound <= 0 {
		return nil, false
	}
	select {
	case <-o.done:
		return o.waitErr, true // Cached result; no second Wait/consume.
	case <-time.After(bound):
		return nil, false
	}
}

func (o *protocolOwner) outputText() string {
	data, truncated := o.capture.snapshot()
	return fmt.Sprintf("%s [bounded-tail=%v, quiescence-proven=%v]", data, truncated, o.complete)
}

func (o *protocolOwner) cleanup() {
	o.cleanupOnce.Do(func() {
		defer func() {
			o.events.health.mu.Lock()
			pulseRecord := fmt.Sprintf(
				"sent=%d;closed=%v;wrapper-completed=%v;stopped=%v;health=%v",
				o.pulse.sent,
				o.pulse.closed,
				o.pulse.completed,
				o.pulse.stopped,
				o.events.health.err,
			)
			o.events.health.mu.Unlock()
			if err := publishProtocolRecord(
				filepath.Join(o.root, "pulse-result"),
				pulseRecord,
			); err != nil {
				o.complete = false
				o.reason += "; pulse result publication: " + err.Error()
			}
			data, truncated := o.capture.snapshot()
			if truncated {
				o.complete = false
				o.reason += "; captured output exceeded bound"
			}
			record := fmt.Sprintf(
				"quiescence-proven=%v;processes-quiescent=%v;truncated=%v;%s\n%s",
				o.complete,
				o.processesQuiescent,
				truncated,
				o.capture.proof(),
				data,
			)
			if err := publishProtocolRecord(
				filepath.Join(o.root, "captured-output"),
				record,
			); err != nil {
				o.complete = false
				o.reason += "; output publication: " + err.Error()
			}
			if o.complete {
				if err := publishProtocolRecord(
					filepath.Join(o.root, "cleanup-complete"),
					"quiescent",
				); err != nil {
					o.complete = false
					o.reason = "cleanup evidence publication: " + err.Error()
				}
			}
		}()
		_, waited := o.wait(0)
		forced := false
		var journalErr error
		if !waited {
			// Failure/expected-negative teardown, NEVER a passing test-signal
			// retry. os.Process is the current owned wrapper handle only.
			if !o.expectedAbort {
				o.retain = true
			}
			termErr := o.cmd.Process.Signal(syscall.SIGTERM)
			journalErr = publishProtocolRecord(
				filepath.Join(o.root, "teardown-TERM"),
				fmt.Sprintf(
					"owned-wrapper=%d;result=%v;not-test-signal",
					o.cmd.Process.Pid,
					termErr,
				),
			)
			_, waited = o.wait(2 * time.Second)
			if !waited {
				forced = true
				killErr := o.cmd.Process.Kill()
				journalErr = errors.Join(
					journalErr,
					publishProtocolRecord(
						filepath.Join(o.root, "teardown-KILL"),
						fmt.Sprintf(
							"owned-wrapper=%d;result=%v;not-test-signal",
							o.cmd.Process.Pid,
							killErr,
						),
					),
				)
				_, waited = o.wait(time.Second)
			}
		}
		endpointErr := errors.Join(o.releaseQueue(), o.closeCaptureDuplicate())
		pulseErr := o.pulse.stopAndJoin(time.Second)
		eventErr := finishProtocolEvents(o.events, o.eventReader, time.Second)
		o.captureErr = o.capture.finish()
		if o.captureErr != nil {
			o.retain = true
		}
		if !waited {
			o.reason = fmt.Sprintf(
				"wrapper Wait did not complete; endpoints=%v; pulse=%v; events=%v; capture=%v",
				endpointErr,
				pulseErr,
				eventErr,
				o.captureErr,
			)
			return
		}
		//nolint:errorlint // Only the bare expected error is allowed; joined/wrapped failures must remain.
		if _, expected := eventErr.(*protocolGateFailure); expected && o.expectedGateFailure &&
			o.builtinOnly {
			eventErr = nil
		}
		if err := errors.Join(
			endpointErr,
			pulseErr,
			eventErr,
			o.events.reconcileEnd(waited),
		); err != nil {
			o.reason = err.Error()
			return
		}
		if err := errors.Join(o.waitRecordErr, journalErr); err != nil {
			o.reason = "owner journal incomplete: " + err.Error()
			return
		}
		if _, err := os.Lstat(
			filepath.Join(o.root, "failure"),
		); !errors.Is(err, os.ErrNotExist) &&
			!o.builtinOnly {
			o.reason = "latched gate failure; possible foreground park utility"
			return
		}
		if o.noAnchor && o.events.anchor != "" {
			o.reason = "anchor created at proven no-anchor gate"
			return
		}
		if o.noAnchor && !o.builtinOnly {
			// The real before-anchor seam follows synchronous setup utilities.
			// A fatal path before that milestone cannot prove utility absence.
			if o.nestedRoot == "" && (!o.events.gated || o.events.phase != "before anchor") {
				o.reason = "no no-anchor utility completion milestone"
				return
			}
		}
		if !o.noAnchor {
			if forced {
				o.reason = "forced wrapper exit with possible unrecorded utility/group"
				return
			}
			var exitErr *exec.ExitError
			if errors.As(o.waitErr, &exitErr) && exitErr.ExitCode() < 0 {
				o.reason = "signal termination without synchronous utility completion"
				return
			}
			if o.events.anchor == "" {
				o.reason = "possible unrecorded outer anchor"
				return
			}
			expected := protocolFrame(o.events.phase, o.events.epoch, o.events.anchor, "waited")
			if ready, err := readProtocolRecord(
				filepath.Join(o.root, "anchor-waited"),
				expected,
			); err != nil ||
				!ready {
				o.reason = fmt.Sprintf("no source-backed outer wait milestone: %v", err)
				return
			}
			pid, _ := strconv.Atoi(o.events.anchor)
			if o.outerDescendantRequired {
				if err := protocolAccountOuterDescendant(o.root, pid); err != nil {
					o.reason = err.Error()
					return
				}
			}
			if err := protocolGroupAbsent(pid, 500*time.Millisecond); err != nil {
				o.reason = "outer group: " + err.Error()
				return
			}
			if o.innerPossible {
				if err := protocolAccountInner(o.root); err != nil {
					o.reason = err.Error()
					return
				}
			}
		}
		if o.nestedRoot != "" {
			// The private fatal fixture emits this only after its registered
			// owner cleanup has joined Wait/pulse/drain and proven group absence.
			for name, expected := range map[string]string{"cleanup-complete": "quiescent", "fatal-unwind": "wait-calls=1;complete=true;retained=true"} {
				if ready, err := readProtocolRecord(
					filepath.Join(o.nestedRoot, name),
					expected,
				); err != nil ||
					!ready {
					o.reason = fmt.Sprintf("fatal child topology uncertain: %s: %v", name, err)
					return
				}
			}
		}
		if !o.wrapperGroupValidated {
			o.reason = "wrapper group was not validated while unreaped"
			return
		}
		if err := protocolGroupAbsent(o.cmd.Process.Pid, 500*time.Millisecond); err != nil {
			o.reason = "wrapper group: " + err.Error()
			return
		}
		o.processesQuiescent = true
		if o.captureErr != nil {
			o.reason = o.captureErr.Error()
			return
		}
		o.complete = true
	})
}

// Only call for a current-invocation source-backed owned group. Signal zero is
// nonmutating. ESRCH is the sole absence result. EPERM, reuse/presence and unknown
// errors remain uncertainty; these numeric IDs are NEVER teardown targets.
func protocolGroupAbsent(pgid int, bound time.Duration) error {
	if pgid <= 1 {
		return errors.New("unvalidated group")
	}
	deadline := time.Now().Add(bound)
	for {
		err := syscall.Kill(-pgid, 0)
		if protocolAbsenceResult(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("absence probe %d: %w", pgid, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("group %d present or reused", pgid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func protocolAbsenceResult(err error) bool { return errors.Is(err, syscall.ESRCH) }

type protocolIdentity struct {
	role, epoch               string
	pid, parent, group, child int
}

func (id protocolIdentity) record() string {
	return fmt.Sprintf(
		"role=%s;epoch=%s;pid=%d;parent=%d;group=%d;child=%d",
		id.role,
		id.epoch,
		id.pid,
		id.parent,
		id.group,
		id.child,
	)
}

func protocolBindOuter(root string) error {
	group, err := syscall.Getpgid(0)
	if err != nil {
		return err
	}
	anchor := strconv.Itoa(group)
	if err := protocolCheckOuterRecord(root, anchor); err != nil {
		return err
	}
	return os.Setenv("FOCUSED_PROTOCOL_ANCHOR", anchor)
}

func protocolCheckOuterRecord(root, anchor string) error {
	pid, err := strconv.Atoi(anchor)
	if err != nil || pid <= 1 || strconv.Itoa(pid) != anchor {
		return errors.New("invalid inherited outer group")
	}
	expected := protocolFrame(
		os.Getenv("FOCUSED_PROTOCOL_BOUNDARY"),
		os.Getenv("FOCUSED_PROTOCOL_EPOCH"),
		anchor,
		"started",
	)
	deadline := time.Now().Add(time.Second)
	for {
		ready, err := readProtocolRecord(filepath.Join(root, "anchor-record"), expected)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("outer anchor publication absent")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Used only by the existing test fixtures. Never enable strict mode in production
// and never promote the best-effort process-events diagnostics to authority.
func protocolFixtureIdentity(root, role string, child int) error {
	if os.Getenv("FOCUSED_PROTOCOL_STRICT") != "1" {
		return nil
	}
	if root == "" || root != os.Getenv("FOCUSED_PROTOCOL_ROOT") {
		return errors.New("invalid private identity root")
	}
	if err := protocolCheckOuterRecord(root, os.Getenv("FOCUSED_PROTOCOL_ANCHOR")); err != nil {
		return err
	}
	group, err := syscall.Getpgid(0)
	if err != nil {
		return err
	}
	id := protocolIdentity{
		role,
		os.Getenv("FOCUSED_PROTOCOL_EPOCH"),
		os.Getpid(),
		os.Getppid(),
		group,
		child,
	}
	return publishProtocolRecord(filepath.Join(root, role), id.record())
}

func readProtocolIdentity(root, role string) (protocolIdentity, error) {
	var id protocolIdentity
	data, err := readProtocolRegular(filepath.Join(root, role), 1024)
	if err != nil {
		return id, err
	}
	parts := strings.Split(string(data), ";")
	if len(parts) != 6 {
		return id, errors.New("malformed identity")
	}
	id.role, id.epoch = strings.TrimPrefix(
		parts[0],
		"role=",
	), strings.TrimPrefix(
		parts[1],
		"epoch=",
	)
	for i, out := range []*int{&id.pid, &id.parent, &id.group, &id.child} {
		key := []string{"pid=", "parent=", "group=", "child="}[i]
		n, err := strconv.Atoi(strings.TrimPrefix(parts[i+2], key))
		if err != nil {
			return id, err
		}
		*out = n
	}
	if id.role != role || id.epoch != filepath.Base(root) || id.pid <= 1 || id.parent <= 1 ||
		id.group <= 1 ||
		id.child < 0 ||
		string(data) != id.record() {
		return id, errors.New("identity is not exact/current")
	}
	return id, nil
}

type protocolGateFailure struct{ reason string }

func (e *protocolGateFailure) Error() string { return e.reason }

type protocolCleanupUncertain struct{ reason string }

func (e *protocolCleanupUncertain) Error() string { return "cleanup-uncertain: " + e.reason }

func protocolAccountInner(root string) error {
	uncertain := func(reason string) error { return &protocolCleanupUncertain{reason} }
	witness, witnessErr := readProtocolRegular(filepath.Join(root, "pre-start-refusal"), 512)
	if witnessErr == nil {
		valid := false
		for _, ack := range []string{"ACST", "A\nST"} {
			expected := fmt.Sprintf(
				"focused: wrapper readiness: invalid focused wrapper acknowledgment %q\n",
				ack,
			)
			if protocolExact(witness, expected) {
				valid = true
			}
		}
		if !valid {
			return uncertain("malformed complete pre-Start witness")
		}
		// The sole publisher reaches this immutable witness only after the
		// actual run returned 1 with its exact untruncated pre-Start diagnostic.
		// This is source-backed no-start evidence, not a missing-sentinel guess.
		// Generic return and any inner-child records are mutually exclusive.
		for _, name := range []string{"real-run-return", "child-start", "child-spawn-intent", "child-spawn", "descendant-start", "ready"} {
			path := filepath.Join(root, name)
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				return uncertain(
					fmt.Sprintf("record %q contradicts complete pre-Start witness: %v", path, err),
				)
			}
		}
		return nil
	}
	if !protocolRecordIsPending(witnessErr) {
		return uncertain(fmt.Sprintf("pre-Start witness unreadable: %v", witnessErr))
	}
	// Late child evidence is allowed only with complete source-backed run
	// return and checked spawn/descendant records. Absence probes never signal.
	data, err := readProtocolRegular(filepath.Join(root, "real-run-return"), 3)
	if err != nil {
		return uncertain("real run return missing; possible unrecorded inner topology")
	}
	code, err := strconv.Atoi(string(data))
	if err != nil || code < 0 || code > 255 || strconv.Itoa(code) != string(data) {
		return uncertain("malformed real run return")
	}
	if ready, err := readProtocolRecord(
		filepath.Join(root, "real-run-return"),
		strconv.Itoa(code),
	); err != nil ||
		!ready {
		return uncertain("real run return changed")
	}
	child, err := readProtocolIdentity(root, "child-start")
	if err != nil || child.parent != child.group || child.pid == child.group {
		return uncertain(fmt.Sprintf("unvalidated inner anchor/child: %v", err))
	}
	spawn, err := readProtocolIdentity(root, "child-spawn")
	if err != nil || spawn.pid != child.pid || spawn.group != child.group || spawn.child <= 1 {
		return uncertain(fmt.Sprintf("incomplete child spawn: %v", err))
	}
	descendant, err := readProtocolIdentity(root, "descendant-start")
	if err != nil || descendant.pid != spawn.child || descendant.parent != child.pid ||
		descendant.group != child.group {
		return uncertain(fmt.Sprintf("incomplete descendant topology: %v", err))
	}
	if err := protocolGroupAbsent(child.group, 500*time.Millisecond); err != nil {
		return uncertain(err.Error())
	}
	return nil
}

func protocolAccountOuterDescendant(root string, group int) error {
	uncertain := func(reason string) error { return &protocolCleanupUncertain{reason} }
	runner, err := readProtocolIdentity(root, "protocol-runner")
	if err != nil || runner.parent != group || runner.group != group {
		return uncertain("outer runner identity missing or ambiguous")
	}
	intent, err := readProtocolIdentity(root, "protocol-spawn-intent")
	if err != nil || intent.pid != runner.pid || intent.group != group {
		return uncertain("outer spawn intent missing or ambiguous")
	}
	spawn, err := readProtocolIdentity(root, "protocol-spawn")
	if err != nil || spawn.pid != runner.pid || spawn.group != group || spawn.child <= 1 {
		return uncertain("outer spawn record missing or ambiguous")
	}
	descendant, err := readProtocolIdentity(root, "descendant-start")
	if err != nil || descendant.pid != spawn.child || descendant.parent != runner.pid ||
		descendant.group != group {
		return uncertain("outer descendant identity missing or ambiguous")
	}
	return nil
}

func protocolSensitivity(t *testing.T, phase string, sig syscall.Signal, noPulse bool) {
	t.Helper()
	tc := protocolCase{name: phase, boundary: phase, signal: sig, forward: sig != 0}
	if sig != 0 {
		tc.want = 128 + int(sig)
	}
	runProtocolCase(t, tc, true, noPulse)
}

func protocolScript(t *testing.T, root, script string, policies ...protocolPolicy) *protocolOwner {
	t.Helper()
	// Retention is frozen before Start, independently of expected exit/t.Failed.
	// Standalone negatives default to retained; explicit positives may opt out.
	policy := protocolPolicy{noAnchor: true, retain: true, builtinOnly: true}
	if len(policies) != 0 {
		policy = policies[0]
	}
	shell := protocolShell
	if policy.queueBeforeRead {
		// Fixture-only, once before ANY FD8 consumption; no normal gate change,
		// no helper process, no producer goroutine, no per-return permission.
		seam := "  protocol_event boundary ready || protocol_park\n"
		if strings.Count(shell, seam) != 1 {
			t.Fatal("missing queue barrier seam")
		}
		shell = strings.Replace(shell, seam, seam+`  protocol_barrier=
  IFS= read -r protocol_barrier <&10
  protocol_barrier_code=$?
  exec 10<&-
  [ "$protocol_barrier_code" -eq 1 ] && [ -z "$protocol_barrier" ] || protocol_park
`, 1)
	}
	path := focusedTaskTrace(
		t,
		root,
		"#!/usr/bin/env bash\n"+shell+"\nstage=build\ncancelled=0\n"+script,
	)
	cmd := exec.Command("bash", path)
	cmd.Env = protocolPrivateEnv(
		root,
		"FOCUSED_PROTOCOL_ROOT="+root,
		"FOCUSED_PROTOCOL_BOUNDARY=reader",
		"FOCUSED_PROTOCOL_EPOCH="+filepath.Base(root),
	)
	return startProtocolOwner(t, root, cmd, policy)
}

func protocolRequireExit(t *testing.T, owner *protocolOwner, want int) {
	t.Helper()
	err, waited := owner.wait(4 * time.Second)
	if !waited {
		t.Fatal("prerequisite shell did not return")
	}
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatal(err)
		}
		code = exitErr.ExitCode()
	}
	owner.cleanup()
	if !owner.complete {
		t.Fatalf("prerequisite cleanup uncertain: %s", owner.reason)
	}
	if code != want {
		t.Fatalf("shell exit=%d, want=%d; %s", code, want, owner.outputText())
	}
}

func protocolCheckShellRecord(t *testing.T, data, expected string, valid bool) {
	t.Helper()
	root := protocolPrivateRoot(t)
	if err := os.WriteFile(filepath.Join(root, "record"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	// Expected strings in this inventory contain no single quotes.
	script := "if protocol_record \"$FOCUSED_PROTOCOL_ROOT/record\" '" + expected + "'; then exit 0; else exit 9; fi\n"
	want := 9
	if valid {
		want = 0
	}
	protocolRequireExit(t, protocolScript(t, root, script), want)
}

func protocolCheckWakeClassification(
	t *testing.T,
	code int,
	data string,
	cancelled int,
	want string,
) {
	t.Helper()
	root := protocolPrivateRoot(t)
	script := fmt.Sprintf(
		"protocol_cancel_count=1\ngate_code=%d\ngate_data='%s'\ncancelled=%d\nprotocol_classify\n[ \"$protocol_class\" = '%s' ]\n",
		code,
		data,
		cancelled,
		want,
	)
	protocolRequireExit(t, protocolScript(t, root, script), 0)
}

func protocolCheckErrorLatch(t *testing.T) {
	t.Helper()
	root := protocolPrivateRoot(t)
	if err := publishProtocolRecord(
		filepath.Join(root, "continue"),
		protocolFrame("reader", filepath.Base(root), "absent", "continue"),
	); err != nil {
		t.Fatal(err)
	}
	// Deterministic failure at the read seam, not a second pipe writer. The park
	// override is only for this no-child model; production phase gates park.
	script := `read() { gate_data=unexpected; return 0; }
protocol_park() { [ "$protocol_failed" -eq 1 ] || exit 20; exit 19; }
protocol_gate
exit 21
`
	owner := protocolScript(
		t,
		root,
		script,
		protocolPolicy{noAnchor: true, retain: true, builtinOnly: true, expectedGateFailure: true},
	)
	protocolRequireExit(t, owner, 19)
	waitProtocolExact(
		t,
		filepath.Join(root, "read-return-1"),
		protocolFrame(
			"reader",
			filepath.Base(root),
			"absent",
			"stage=build;cancelled=0;code=0;class=failure",
		),
	)
	if _, err := os.Lstat(filepath.Join(root, "closed")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("latched read failure accepted valid continuation")
	}
}

func protocolCheckPulseClosure(t *testing.T) {
	root := protocolPrivateRoot(t)
	owner := protocolScript(
		t,
		root,
		"protocol_park() { exit 19; }\nprotocol_gate\nexit 21\n",
		protocolPolicy{
			noAnchor:            true,
			retain:              true,
			noPulse:             true,
			builtinOnly:         true,
			expectedGateFailure: true,
		},
	)
	waitProtocolExact(
		t,
		filepath.Join(root, "read-enter-1"),
		protocolFrame("reader", filepath.Base(root), "absent", "stage=build;cancelled=0"),
	)
	if err := owner.pulse.closeWriter(); err != nil {
		t.Fatal(err)
	}
	protocolRequireExit(t, owner, 19)
	waitProtocolExact(
		t,
		filepath.Join(root, "read-return-1"),
		protocolFrame(
			"reader",
			filepath.Base(root),
			"absent",
			"stage=build;cancelled=0;code=1;class=failure",
		),
	)
}

func protocolCheckDescriptor(t *testing.T) {
	t.Helper()
	root := protocolPrivateRoot(t)
	if err := publishProtocolRecord(
		filepath.Join(root, "continue"),
		protocolFrame("reader", filepath.Base(root), "absent", "continue"),
	); err != nil {
		t.Fatal(err)
	}
	script := `protocol_gate
if { : <&8; } 2>/dev/null; then exit 9; fi
# A synchronous subshell checks inheritance before a Go runtime can reuse fd8.
( if { : <&8; } 2>/dev/null; then exit 9; fi; exit 0 ) || exit 9
exit 0
`
	owner := protocolScript(t, root, script)
	protocolRequireExit(t, owner, 0)
	state := protocolState{"reader", filepath.Base(root), "absent"}
	waitProtocolExact(
		t,
		filepath.Join(root, "boundary"),
		protocolFrame(state.phase, state.epoch, state.anchor, "ready"),
	)
	waitProtocolExact(
		t,
		filepath.Join(root, "closed"),
		protocolFrame(state.phase, state.epoch, state.anchor, "closed"),
	)
}

func protocolCheckSolePulseWriter(t *testing.T) {
	t.Helper()
	if strings.Contains(protocolShell, ">&8") || strings.Contains(protocolShell, "-t 1") ||
		!strings.Contains(protocolShell, "IFS= read -r gate_data <&8") {
		t.Fatal("shell is not an untimed read-only consumer")
	}
	root := protocolPrivateRoot(t)
	owner := protocolScript(t, root, "protocol_gate\nexit 0\n")
	state := protocolState{"reader", filepath.Base(root), "absent"}
	if err := waitProtocolWake(owner, state, time.Second); err != nil {
		t.Fatal(err)
	}
	if owner.cmd.ExtraFiles[5] == owner.pulse.writer ||
		owner.cmd.ExtraFiles[6] == owner.pulse.writer {
		t.Fatal("writer inherited")
	}
	if _, err := owner.cmd.ExtraFiles[5].Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("parent read copy still open: %v", err)
	}
	continueProtocol(t, owner, state)
	protocolRequireExit(t, owner, 0)
}

func protocolCheckParentAbort(t *testing.T) {
	t.Helper()
	root := protocolPrivateRoot(t)
	owner := protocolScript(t, root, "protocol_gate\nexit 9\n")
	waitProtocolExact(
		t,
		filepath.Join(root, "boundary"),
		protocolFrame("reader", filepath.Base(root), "absent", "ready"),
	)
	owner.expectedAbort = true
	owner.cleanup()
	if !owner.complete {
		t.Fatalf("parent abort incomplete: %s", owner.reason)
	}
	if owner.waitCalls != 1 {
		t.Fatalf("Wait called %d times", owner.waitCalls)
	}
	if _, err := os.Lstat(filepath.Join(root, "continue")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("parent abort published continuation")
	}
}

func protocolCheckOwnerOnce(t *testing.T) {
	t.Helper()
	for _, fatalPath := range []bool{false, true} {
		t.Run(strconv.FormatBool(fatalPath), func(t *testing.T) {
			root := protocolPrivateRoot(t)
			script := "exit 0\n"
			if fatalPath {
				script = "protocol_gate\nexit 9\n"
			}
			owner := protocolScript(t, root, script)
			if fatalPath {
				waitProtocolExact(
					t,
					filepath.Join(root, "boundary"),
					protocolFrame("reader", filepath.Base(root), "absent", "ready"),
				)
				// Invoke the identical callback registered for t.Fatal cleanup,
				// without deliberately failing the enclosing Go test.
				owner.expectedAbort = true
				owner.cleanup()
			} else {
				protocolRequireExit(t, owner, 0)
			}
			owner.cleanup()
			err1, ok1 := owner.wait(time.Second)
			err2, ok2 := owner.wait(time.Second)
			//nolint:errorlint // The cached Wait error must be the identical object, not merely equivalent.
			if !ok1 || !ok2 || err1 != err2 || owner.waitCalls != 1 || !owner.complete {
				t.Fatalf(
					"once-only Wait/result/cleanup: %v %v %v %v calls=%d complete=%v",
					err1,
					err2,
					ok1,
					ok2,
					owner.waitCalls,
					owner.complete,
				)
			}
		})
	}
}

func protocolCheckCleanupUncertainty(t *testing.T) {
	t.Helper()
	root := protocolPrivateRoot(t) // No processes: retained model evidence.
	for _, tc := range []struct {
		name   string
		err    error
		absent bool
	}{
		{"ESRCH", syscall.ESRCH, true},
		{"EPERM", syscall.EPERM, false},
		{"present or reused", nil, false},
		{"unknown", syscall.EIO, false},
	} {
		if got := protocolAbsenceResult(tc.err); got != tc.absent {
			t.Errorf("%s absence=%v", tc.name, got)
		}
	}
	for _, name := range []string{"missing run return", "return without inner identity"} {
		if name == "return without inner identity" {
			if err := publishProtocolRecord(
				filepath.Join(root, "real-run-return"),
				"1",
			); err != nil {
				t.Fatal(err)
			}
		}
		var uncertain *protocolCleanupUncertain
		if err := protocolAccountInner(root); !errors.As(err, &uncertain) {
			t.Fatalf("%s: expected typed cleanup uncertainty, got %v", name, err)
		}
		if _, err := os.Stat(root); err != nil {
			t.Fatalf("uncertain evidence removed: %v", err)
		}
	}
	t.Logf("expected cleanup-uncertain model; retained %s", root)
	t.Run("complete pre-start witness", protocolCheckPreStartWitness)
}

// The pre-start witness is itself published only after the real run returned 1
// with the exact unique readiness diagnostic. A second publication is not part
// of that proof: a signal can terminate the fixture between the two writes.
func protocolCheckPreStartWitness(t *testing.T) {
	partial := "focused: wrapper readiness: invalid focused wrapper acknowledgment \"ACST\"\n"
	short := "focused: wrapper readiness: invalid focused wrapper acknowledgment \"A\\nST\"\n"
	for _, tc := range []struct {
		name, witness, returned, conflict string
		uncertain                         bool
	}{
		{"partial ACK without second publication", partial, "", "", false},
		{"short ACK without second publication", short, "", "", false},
		{"missing witness", "", "", "", true},
		{"malformed witness", "not a pre-Start return\n", "", "", true},
		{"partial witness", strings.TrimSuffix(partial, "\n"), "", "", true},
		{"NUL witness", partial + "\x00", "", "", true},
		{"oversized witness", strings.Repeat("x", 1025), "", "", true},
		{"competing return record", partial, "1", "", true},
		{"contradictory return record", partial, "7", "", true},
		{"child start", partial, "", "child-start", true},
		{"spawn intent", partial, "", "child-spawn-intent", true},
		{"spawn completion", partial, "", "child-spawn", true},
		{"descendant start", partial, "", "descendant-start", true},
		{"descendant readiness", partial, "", "ready", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := protocolPrivateRoot(t) // No processes or group probes in these models.
			if tc.witness != "" {
				if err := publishProtocolRecord(
					filepath.Join(root, "pre-start-refusal"),
					tc.witness,
				); err != nil {
					t.Fatal(err)
				}
			}
			if tc.returned != "" {
				if err := publishProtocolRecord(
					filepath.Join(root, "real-run-return"),
					tc.returned,
				); err != nil {
					t.Fatal(err)
				}
			}
			if tc.conflict != "" {
				if err := publishProtocolRecord(
					filepath.Join(root, tc.conflict),
					"contradiction",
				); err != nil {
					t.Fatal(err)
				}
			}
			err := protocolAccountInner(root)
			var uncertain *protocolCleanupUncertain
			if tc.uncertain {
				if !errors.As(err, &uncertain) {
					t.Fatalf("missing typed uncertainty: %v", err)
				}
			} else if err != nil {
				t.Fatalf("complete pre-Start witness required a second publication: %v", err)
			}
			if _, err := os.Stat(root); err != nil {
				t.Fatalf("model evidence removed: %v", err)
			}
		})
	}
	for _, kind := range []string{"FIFO", "directory", "symlink"} {
		t.Run(kind+" witness", func(t *testing.T) {
			root := protocolPrivateRoot(t)
			path := filepath.Join(root, "pre-start-refusal")
			var err error
			switch kind {
			case "FIFO":
				err = syscall.Mkfifo(path, 0o600)
			case "directory":
				err = os.Mkdir(path, 0o700)
			case "symlink":
				if err = publishProtocolRecord(filepath.Join(root, "target"), partial); err == nil {
					err = os.Symlink(filepath.Join(root, "target"), path)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			var uncertain *protocolCleanupUncertain
			if err := protocolAccountInner(root); !errors.As(err, &uncertain) {
				t.Fatalf("nonregular witness did not refuse: %v", err)
			}
		})
	}
}

func protocolCheckEventChannel(t *testing.T) {
	t.Helper()
	for _, tc := range []struct {
		name, wire string
		invalid    bool
	}{
		{"complete", "boundary|reader|epoch|absent|ready\n", false},
		{"partial", "boundary|reader|epoch|absent|ready", true},
		{"NUL", "boundary|reader|epoch|absent|ready\x00\n", true},
		{"wrong epoch", "boundary|reader|stale|absent|ready\n", true},
		{"oversized", strings.Repeat("x", 1025) + "\n", true},
		{"overflow", protocolOverflowEvents(), true},
		{"duplicate", strings.Repeat("boundary|reader|epoch|absent|ready\n", 2), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := protocolPrivateRoot(t)
			events := &protocolEvents{
				root:  root,
				phase: "reader",
				epoch: "epoch",
				done:  make(chan struct{}),
			}
			events.drain(strings.NewReader(tc.wire))
			<-events.done
			if (events.health.failure() != nil) != tc.invalid {
				t.Fatalf("event error=%v", events.health.failure())
			}
			if tc.name == "overflow" &&
				(events.drainErr == nil || events.drainErr.Error() != "event journal overflow") {
				t.Fatalf("missing terminal overflow after discard: %v", events.drainErr)
			}
			if tc.name == "duplicate" {
				if ok, err := readProtocolRecord(
					filepath.Join(root, "boundary"),
					protocolFrame("reader", "epoch", "absent", "ready"),
				); err != nil ||
					!ok {
					t.Fatalf("first publication changed: %v", err)
				}
			}
		})
	}
	t.Run("fragmented complete event", func(t *testing.T) {
		root := protocolPrivateRoot(t)
		events := &protocolEvents{
			root:  root,
			phase: "reader",
			epoch: "epoch",
			done:  make(chan struct{}),
		}
		events.drain(
			io.MultiReader(
				strings.NewReader("boundary|reader|"),
				strings.NewReader("epoch|absent|ready\n"),
			),
		)
		if err := events.health.failure(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("read error latches", func(t *testing.T) {
		root := protocolPrivateRoot(t)
		events := &protocolEvents{
			root:  root,
			phase: "reader",
			epoch: "epoch",
			done:  make(chan struct{}),
		}
		events.drain(protocolFailedReader{})
		if !errors.Is(events.health.failure(), syscall.EIO) {
			t.Fatalf("read error lost: %v", events.health.failure())
		}
	})
	t.Run("open writer is incomplete", func(t *testing.T) {
		root := protocolPrivateRoot(t)
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer w.Close()
		events := &protocolEvents{root: root, done: make(chan struct{})}
		go events.drain(r)
		var incomplete *protocolCleanupUncertain
		if err := finishProtocolEvents(
			events,
			r,
			20*time.Millisecond,
		); !errors.As(
			err,
			&incomplete,
		) {
			t.Fatalf("open writer did not yield typed uncertainty: %v", err)
		}
		if _, err := os.Stat(root); err != nil {
			t.Fatalf("incomplete evidence removed: %v", err)
		}
		select {
		case <-events.done:
		case <-time.After(time.Second):
			t.Fatal("closed reader did not drain")
		}
		t.Logf("expected incomplete-drain model; retained %s", root)
	})

	t.Run("anchor inheritance", func(t *testing.T) {
		root := protocolPrivateRoot(t)
		script := `( if { : <&8; } 2>/dev/null; then exit 8; fi
if { : >&9; } 2>/dev/null; then exit 9; fi; exit 0 ) 8<&- 9>&- || exit 9
exit 0
`
		protocolRequireExit(t, protocolScript(t, root, script), 0)
		source, err := os.ReadFile("task.sh")
		if err != nil {
			t.Fatal(err)
		}
		instrumented := instrumentProtocolWrapper(t, string(source))
		if !strings.Contains(instrumented, ") 8<&- 9>&- &\nanchor=$!") {
			t.Fatal("production-derived anchor does not close both private FDs")
		}
	})
}

func protocolOverflowEvents() string {
	// Source event-count bounds reject first; the byte cap independently
	// bounds discard/storage even when the producer continues after failure.
	return strings.Repeat("x", protocolEventLimit+1)
}

func protocolCheckFixtureBoundary(t *testing.T) {
	root := protocolPrivateRoot(t)
	state := protocolState{"reader", filepath.Base(root), "absent"}
	if err := publishProtocolRecord(
		filepath.Join(root, "boundary"),
		protocolFrame(state.phase, state.epoch, state.anchor, "ready"),
	); err != nil {
		t.Fatal(err)
	}
	// This script does not call protocol_gate or emit a shell boundary. Its
	// blocking read represents the production read at a Go-fixture boundary.
	owner := protocolScript(t, root, "IFS= read -r unused <&8\nexit 9\n")
	err := waitProtocolWake(owner, state, 150*time.Millisecond)
	var missing *protocolMissingWake
	if !errors.As(err, &missing) {
		t.Fatalf("fixture-only boundary woke: %v", err)
	}
	owner.events.health.mu.Lock()
	sent, gated := owner.pulse.sent, owner.events.gated
	owner.events.health.mu.Unlock()
	if sent != 0 || gated {
		t.Fatalf("fixture boundary started shell pulses: sent=%d gated=%v", sent, gated)
	}
	owner.expectedAbort = true
	owner.cleanup()
	if !owner.complete {
		t.Fatalf("fixture-boundary cleanup uncertain: %s", owner.reason)
	}
}

func protocolCheckPendingContinuation(t *testing.T, name string) {
	root := protocolPrivateRoot(t)
	state := protocolState{"reader", filepath.Base(root), "absent"}
	if name == "interruption skips grant" {
		if err := publishProtocolRecord(
			filepath.Join(root, "continue"),
			protocolFrame(state.phase, state.epoch, state.anchor, "continue"),
		); err != nil {
			t.Fatal(err)
		}
		// Deterministic interrupted-return model; the next read is the actual
		// untimed builtin. No per-return grant is added to the real controller.
		script := `read() {
  if [ "${injected:-0}" -eq 0 ]; then
    injected=1
    cancelled=130
    protocol_cancel_event
    gate_data=
    return 130
  fi
  builtin read "$@"
}
protocol_gate
exit 0
`
		owner := protocolScript(t, root, script)
		protocolRequireExit(t, owner, 0)
		waitProtocolExact(
			t,
			filepath.Join(root, "read-return-1"),
			protocolFrame(
				state.phase,
				state.epoch,
				state.anchor,
				"stage=build;cancelled=130;code=130;class=interrupted",
			),
		)
		waitProtocolExact(
			t,
			filepath.Join(root, "read-return-2"),
			protocolFrame(
				state.phase,
				state.epoch,
				state.anchor,
				"stage=build;cancelled=130;code=0;class=wake",
			),
		)
		return
	}
	if name == "queued wakes hold" || name == "queued barrier abort" {
		owner := protocolScript(
			t,
			root,
			"protocol_gate\nexit 0\n",
			protocolPolicy{noAnchor: true, retain: true, builtinOnly: true, queueBeforeRead: true},
		)
		deadline := time.Now().Add(time.Second)
		var queued int
		for {
			owner.events.health.mu.Lock()
			sent, entries, returns, failure := owner.pulse.sent, owner.events.entries, owner.events.returns, owner.events.health.err
			owner.events.health.mu.Unlock()
			if failure != nil || entries != 0 || returns != 0 {
				t.Fatalf(
					"consumption before queue barrier: sent=%d entries=%d returns=%d error=%v",
					sent,
					entries,
					returns,
					failure,
				)
			}
			if sent >= 3 {
				queued = sent
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("multiple successful sends were not queued")
			}
			time.Sleep(10 * time.Millisecond)
		}
		assertProtocolHeld(t, root)
		if err := publishProtocolRecord(
			filepath.Join(root, "queued-before-consumption"),
			fmt.Sprintf("sent=%d;entries=0;returns=0", queued),
		); err != nil {
			t.Fatal(err)
		}
		if name == "queued barrier abort" {
			owner.expectedAbort = true
			owner.cleanup()
			if !owner.complete || owner.waitCalls != 1 {
				t.Fatalf("barrier abort unjoined: %s", owner.reason)
			}
			for _, endpoint := range []*os.File{owner.queueWriter, owner.cmd.ExtraFiles[7]} {
				if _, err := endpoint.Stat(); !errors.Is(err, os.ErrClosed) {
					t.Fatalf("barrier endpoint survived abort: %v", err)
				}
			}
			assertProtocolHeld(t, root)
			if _, err := os.Lstat(
				filepath.Join(root, "read-enter-1"),
			); !errors.Is(
				err,
				os.ErrNotExist,
			) {
				t.Fatalf("abort advanced past pre-consumption barrier: %v", err)
			}
			return
		}
		// FD10 EOF releases ONLY the one-time pre-consumption fixture barrier.
		// The sole LF writer already enqueued >=3 bytes; no FD8 read precedes
		// this release, so FIFO order makes the first three wakes consume those
		// bytes, not later sends. Neither barrier EOF nor LF grants continuation.
		if err := owner.releaseQueue(); err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= 3; i++ {
			waitProtocolExact(
				t,
				filepath.Join(root, fmt.Sprintf("read-enter-%d", i)),
				protocolFrame(state.phase, state.epoch, state.anchor, "stage=build;cancelled=0"),
			)
			waitProtocolExact(
				t,
				filepath.Join(root, fmt.Sprintf("read-return-%d", i)),
				protocolFrame(
					state.phase,
					state.epoch,
					state.anchor,
					"stage=build;cancelled=0;code=0;class=wake",
				),
			)
		}
		assertProtocolHeld(t, root)
		continueProtocol(t, owner, state)
		protocolRequireExit(t, owner, 0)
		waitProtocolExact(
			t,
			filepath.Join(root, "closed"),
			protocolFrame(state.phase, state.epoch, state.anchor, "closed"),
		)
		return
	}
	owner := protocolScript(
		t,
		root,
		"protocol_gate\nexit 0\n",
		protocolPolicy{
			noAnchor:    true,
			retain:      true,
			builtinOnly: true,
			pulsePeriod: 300 * time.Millisecond,
		},
	)
	if err := waitProtocolWake(owner, state, time.Second); err != nil {
		t.Fatal(err)
	}
	iteration := 2
	waitProtocolExact(
		t,
		filepath.Join(root, fmt.Sprintf("read-enter-%d", iteration)),
		protocolFrame(state.phase, state.epoch, state.anchor, "stage=build;cancelled=0"),
	)
	if _, err := os.Lstat(
		filepath.Join(root, fmt.Sprintf("read-return-%d", iteration)),
	); !errors.Is(
		err,
		os.ErrNotExist,
	) {
		t.Fatalf("later read already returned, pending window not evidenced: %v", err)
	}
	assertProtocolHeld(t, root)
	if err := owner.authorize(state, iteration); err != nil {
		t.Fatal(err)
	}
	protocolRequireExit(t, owner, 0)
	waitProtocolExact(
		t,
		filepath.Join(root, fmt.Sprintf("read-return-%d", iteration)),
		protocolFrame(
			state.phase,
			state.epoch,
			state.anchor,
			"stage=build;cancelled=0;code=0;class=wake",
		),
	)
	waitProtocolExact(
		t,
		filepath.Join(root, "closed"),
		protocolFrame(state.phase, state.epoch, state.anchor, "closed"),
	)
}

func protocolCheckPulseLifecycle(t *testing.T, name string) {
	root := protocolPrivateRoot(t) // Expected producer failures are retained.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	events := &protocolEvents{
		root:  root,
		phase: "reader",
		epoch: "epoch",
		done:  make(chan struct{}),
	}
	health := &events.health
	pulse := newProtocolPulse(w, health, false)
	events.pulse = pulse
	pulse.period, pulse.budget = time.Millisecond, 3
	t.Cleanup(func() {
		joinErr := pulse.stopAndJoin(time.Second)
		closeErr := r.Close()
		if err := errors.Join(joinErr, closeErr); err != nil {
			t.Error(err)
		}
		health.mu.Lock()
		sent := pulse.sent
		health.mu.Unlock()
		if err := publishProtocolRecord(
			filepath.Join(root, "pulse-result"),
			fmt.Sprintf("case=%s;sent=%d;health=%v;join=%v", name, sent, health.failure(), joinErr),
		); err != nil {
			t.Error(err)
		}
	})
	switch name {
	case "deadline":
		pulse.lifetime = 0
	case "backpressure", "cancel blocked write joins":
		// Sole producer still writes one LF per call. Test-only fast rate and
		// larger budget reach pipe capacity without a second filling writer.
		pulse.period, pulse.budget = time.Nanosecond, 1<<20
		pulse.lifetime, pulse.writeBound = 3*time.Second, 20*time.Millisecond
		if name == "cancel blocked write joins" {
			pulse.writeBound = time.Second
		}
	case "EPIPE before close event":
		if err := r.Close(); err != nil {
			t.Fatal(err)
		}
		// Replace cleanup's read endpoint with an already-independent /dev/null
		// handle; the original anonymous pipe has no reader and cannot be reused.
		r, err = os.Open(os.DevNull)
		if err != nil {
			t.Fatal(err)
		}
	case "validated close":
		// Use the actual validator, not an optimistic flag after failed Write.
		events.gated, events.entries, events.returns, events.lastClass = true, 1, 1, "wake"
		if err := events.publishLocked([]byte("closed|reader|epoch|absent|closed")); err != nil {
			t.Fatal(err)
		}
	}
	pulse.start()
	if name == "cancel blocked write joins" {
		deadline := time.Now().Add(2 * time.Second)
		previous := -1
		for {
			health.mu.Lock()
			sent, writing := pulse.sent, pulse.writing
			health.mu.Unlock()
			if writing && sent >= 512 && sent == previous {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("pending full-pipe Write not evidenced")
			}
			previous = sent
			time.Sleep(20 * time.Millisecond)
		}
		if err := pulse.stopAndJoin(500 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	if name == "cancel join" {
		if err := pulse.stopAndJoin(time.Second); err != nil {
			t.Fatal(err)
		}
	} else {
		select {
		case <-pulse.done:
		case <-time.After(4 * time.Second):
			t.Fatal("producer did not finish within bound")
		}
	}
	failure := health.failure()
	switch name {
	case "sole LF bytes", "queued bytes are not grants", "budget":
		if failure == nil || !strings.Contains(failure.Error(), "budget") {
			t.Fatalf("missing budget failure: %v", failure)
		}
		if err := pulse.closeWriter(); err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(io.LimitReader(r, 4))
		if err != nil || string(data) != "\n\n\n" || pulse.sent != 3 {
			t.Fatalf("sole LF producer: %q, sent=%d, %v", data, pulse.sent, err)
		}
		if name == "queued bytes are not grants" {
			// No read occurred until producer completion: all three bytes were
			// queued, including across writer Close. None carries authority.
			if ready, err := readProtocolRecord(
				filepath.Join(root, "continue"),
				"continue",
			); err != nil ||
				ready {
				t.Fatalf("queued wakes granted: %v,%v", ready, err)
			}
		}
	case "deadline":
		if !errors.Is(failure, os.ErrDeadlineExceeded) {
			t.Fatalf("deadline not latched: %v", failure)
		}
	case "backpressure":
		if !errors.Is(failure, os.ErrDeadlineExceeded) || pulse.sent < 512 {
			t.Fatalf("backpressure not evidenced: sent=%d error=%v", pulse.sent, failure)
		}
	case "EPIPE before close event":
		if !errors.Is(failure, syscall.EPIPE) {
			t.Fatalf("EPIPE not latched: %v", failure)
		}
		events.gated, events.entries, events.returns, events.lastClass = true, 1, 1, "wake"
		events.drain(strings.NewReader("closed|reader|epoch|absent|closed\n"))
		//nolint:errorlint // Late evidence must preserve the original sticky error object.
		if health.failure() != failure || pulse.closed {
			t.Fatal("late close evidence erased unknown producer loss")
		}
		if _, err := os.Lstat(filepath.Join(root, "closed")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("poisoned stream published close: %v", err)
		}
	case "validated close", "cancel join", "cancel blocked write joins":
		if failure != nil {
			t.Fatal(failure)
		}
	}
}

func protocolCheckIdentityRecord(t *testing.T, name string) {
	root := protocolPrivateRoot(t)
	id := protocolIdentity{"child-start", filepath.Base(root), 101, 100, 100, 0}
	path := filepath.Join(root, id.role)
	data := id.record()
	var err error
	switch name {
	case "FIFO":
		err = syscall.Mkfifo(path, 0o600)
	case "directory":
		err = os.Mkdir(path, 0o700)
	case "symlink":
		err = os.WriteFile(filepath.Join(root, "target"), []byte(data), 0o600)
		if err == nil {
			err = os.Symlink(filepath.Join(root, "target"), path)
		}
	default:
		switch name {
		case "oversized":
			data = strings.Repeat("x", 1025)
		case "malformed":
			data = "role=child-start;pid=1"
		case "NUL":
			data += "\x00"
		case "trailing newline":
			data += "\n"
		}
		err = os.WriteFile(path, []byte(data), 0o600)
	}
	if err != nil {
		t.Fatal(err)
	}
	if name == "replacement" {
		var replaceErr error
		_, err := readProtocolRegularChecked(path, 1024, func() {
			replaceErr = os.Rename(path, filepath.Join(root, "original"))
			if replaceErr == nil {
				replaceErr = os.WriteFile(path, []byte(data), 0o600)
			}
		})
		if replaceErr != nil {
			t.Fatal(replaceErr)
		}
		if err == nil {
			t.Fatal("replacement accepted")
		}
		return
	}
	got, err := readProtocolIdentity(root, id.role)
	if name == "exact" {
		if err != nil || got != id {
			t.Fatalf("exact identity=%v,%v", got, err)
		}
	} else if err == nil {
		t.Fatalf("accepted %s identity", name)
	}
	if name == "FIFO" {
		if err := publishProtocolRecord(filepath.Join(root, "real-run-return"), "9"); err != nil {
			t.Fatal(err)
		}
		var uncertain *protocolCleanupUncertain
		if err := protocolAccountInner(root); !errors.As(err, &uncertain) {
			t.Fatalf("FIFO cleanup did not refuse: %v", err)
		}
	}
}

func protocolCheckOuterIdentity(t *testing.T, name string) {
	root := protocolPrivateRoot(t)
	group, err := syscall.Getpgid(0)
	if err != nil {
		t.Fatal(err)
	}
	phase := "startup"
	t.Setenv("FOCUSED_PROTOCOL_ROOT", root)
	t.Setenv("FOCUSED_PROTOCOL_EPOCH", filepath.Base(root))
	t.Setenv("FOCUSED_PROTOCOL_BOUNDARY", phase)
	t.Setenv("FOCUSED_PROTOCOL_STRICT", "1")
	t.Setenv("FOCUSED_PROTOCOL_ANCHOR", "")
	publishedGroup := group
	if name == "wrong group" {
		publishedGroup++
	}
	if err := publishProtocolRecord(
		filepath.Join(root, "anchor-record"),
		protocolFrame(phase, filepath.Base(root), strconv.Itoa(publishedGroup), "started"),
	); err != nil {
		t.Fatal(err)
	}
	if name == "wrong group" {
		if err := protocolBindOuter(root); err == nil {
			t.Fatal("runner accepted a different actual group")
		}
		if os.Getenv("FOCUSED_PROTOCOL_ANCHOR") != "" {
			t.Fatal("failed binding propagated")
		}
		return
	}
	if err := protocolBindOuter(root); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("FOCUSED_PROTOCOL_ANCHOR") != strconv.Itoa(group) {
		t.Fatal("validated group not propagated")
	}
	if name == "publication failure prevents spawn" ||
		name == "spawn-intent failure prevents spawn" {
		// Existing strict child fixture must fail on its required start record,
		// before its spawn intent or descendant Start. No runner copy is used.
		blocked := "child-start"
		if name == "spawn-intent failure prevents spawn" {
			blocked = "child-spawn-intent"
		}
		if err := publishProtocolRecord(filepath.Join(root, blocked), "occupied"); err != nil {
			t.Fatal(err)
		}
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(binary, "-test.run=^TestFocusedCancellationProcessFixture$")
		cmd.Env = protocolPrivateEnv(
			root,
			"FOCUSED_PROTOCOL_ROOT="+root,
			"FOCUSED_PROTOCOL_BOUNDARY="+phase,
			"FOCUSED_PROTOCOL_EPOCH="+filepath.Base(root),
			"FOCUSED_PROTOCOL_ANCHOR="+strconv.Itoa(group),
			"FOCUSED_PROTOCOL_STRICT=1",
			"DOTTY_FOCUSED_CANCEL_FIXTURE=child",
			"DOTTY_FOCUSED_CANCEL_ROOT="+root,
			"DOTTY_FOCUSED_CANCEL_BEHAVIOR=retained pipe",
		)
		owner := startProtocolOwner(
			t,
			root,
			cmd,
			protocolPolicy{noAnchor: true, retain: true, builtinOnly: true},
		)
		protocolRequireExit(t, owner, 9)
		for _, file := range []string{"child-spawn-intent", "child-spawn", "descendant-start", "ready"} {
			if file == blocked {
				continue
			}
			if _, err := os.Lstat(filepath.Join(root, file)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("spawn followed publication failure: %s: %v", file, err)
			}
		}
		return
	}
	role := "protocol-runner"
	if name == "descendant" {
		role = "descendant-start"
	}
	if err := protocolFixtureIdentity(root, role, 0); err != nil {
		t.Fatal(err)
	}
	id, err := readProtocolIdentity(root, role)
	if err != nil || id.group != group || id.pid != os.Getpid() {
		t.Fatalf("identity binding=%v,%v", id, err)
	}
}

func protocolCheckAuthorizationHealth(t *testing.T, name string) {
	root := protocolPrivateRoot(t)
	events := &protocolEvents{
		root:  root,
		phase: "before anchor",
		epoch: filepath.Base(root),
		done:  make(chan struct{}),
	}
	pulse := &protocolPulse{health: &events.health, sent: 1}
	owner := &protocolOwner{root: root, events: events, pulse: pulse}
	state := protocolState{events.phase, events.epoch, "absent"}
	frame := func(kind, anchor, action string) string {
		return strings.Join([]string{kind, events.phase, events.epoch, anchor, action}, "|") + "\n"
	}
	wire := frame(
		"boundary",
		"absent",
		"ready",
	) + frame(
		"read-enter-1",
		"absent",
		"stage=build;cancelled=0",
	) + frame(
		"read-return-1",
		"absent",
		"stage=build;cancelled=0;code=0;class=wake",
	) + frame(
		"cancel-1",
		"absent",
		"cancelled=130",
	)
	switch name {
	case "malformed after cancellation":
		wire += "malformed\n"
	case "overflow after cancellation":
		wire += protocolOverflowEvents()
	case "lone return":
		wire += frame("read-return-2", "absent", "stage=build;cancelled=130;code=0;class=wake")
	case "wrong anchor":
		wire += frame("read-enter-2", "123", "stage=build;cancelled=130")
	case "wrong sequence":
		wire += frame("read-enter-3", "absent", "stage=build;cancelled=130")
	case "wrong action":
		wire += frame("read-enter-2", "absent", "garbage")
	case "EOF with outstanding entry":
		wire += frame("read-enter-2", "absent", "stage=build;cancelled=130")
	case "empty channel loss":
		wire = ""
	}
	events.drain(strings.NewReader(wire))
	if name == "pulse failure" {
		events.health.mu.Lock()
		events.health.failLocked(syscall.EPIPE)
		events.health.mu.Unlock()
	}
	terminalOnly := strings.HasPrefix(name, "EOF ") || name == "empty channel loss"
	sticky := events.health.failure()
	if terminalOnly {
		if sticky != nil {
			t.Fatalf("clean EOF conflated with invalid framing: %v", sticky)
		}
	} else if sticky == nil {
		t.Fatal("invalid stream did not latch")
	}
	if name == "overflow after cancellation" &&
		(events.drainErr == nil || events.drainErr.Error() != "event journal overflow") {
		t.Fatalf("missing distinct total overflow: %v", events.drainErr)
	}
	if name != "empty channel loss" {
		waitProtocolExact(
			t,
			filepath.Join(root, "cancel-1"),
			protocolFrame(state.phase, state.epoch, state.anchor, "cancelled=130"),
		)
	}
	//nolint:errorlint // Identity proves the sticky guard won instead of a different EOF error.
	if err := owner.authorize(state); err == nil {
		t.Fatal("known failure/ended observations permitted continuation")
	} else if terminalOnly && err.Error() != "event observations ended" {
		t.Fatalf("terminal state did not independently block authorization: %v", err)
	} else if !terminalOnly && err != sticky {
		t.Fatalf("EOF masked sticky authorization failure: got %v, want identical %v", err, sticky)
	}
	if _, err := os.Lstat(filepath.Join(root, "continue")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("continuation exists: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "read-return-2")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid evidence published: %v", err)
	}
}

func protocolCheckNegativeRetention(t *testing.T) {
	for _, name := range []string{"record rejection", "read failure", "EOF", "abort", "positive removal"} {
		t.Run(name, func(t *testing.T) {
			var root string
			if !t.Run("registered cleanup", func(t *testing.T) {
				root = protocolPrivateRoot(t)
				switch name {
				case "read failure":
					owner := protocolScript(
						t,
						root,
						"read() { gate_data=bad; return 0; }\nprotocol_park() { exit 19; }\nprotocol_gate\n",
						protocolPolicy{
							noAnchor:            true,
							retain:              true,
							builtinOnly:         true,
							expectedGateFailure: true,
						},
					)
					protocolRequireExit(t, owner, 19)
				case "EOF":
					owner := protocolScript(
						t,
						root,
						"protocol_park() { exit 19; }\nprotocol_gate\n",
						protocolPolicy{
							noAnchor:            true,
							retain:              true,
							noPulse:             true,
							builtinOnly:         true,
							expectedGateFailure: true,
						},
					)
					waitProtocolExact(
						t,
						filepath.Join(root, "boundary"),
						protocolFrame("reader", filepath.Base(root), "absent", "ready"),
					)
					if err := owner.pulse.closeWriter(); err != nil {
						t.Fatal(err)
					}
					protocolRequireExit(t, owner, 19)
				case "abort":
					owner := protocolScript(t, root, "protocol_gate\n")
					waitProtocolExact(
						t,
						filepath.Join(root, "boundary"),
						protocolFrame("reader", filepath.Base(root), "absent", "ready"),
					)
					owner.expectedAbort = true
				case "record rejection":
					if err := os.WriteFile(
						filepath.Join(root, "record"),
						[]byte("bad"),
						0o600,
					); err != nil {
						t.Fatal(err)
					}
					protocolRequireExit(
						t,
						protocolScript(
							t,
							root,
							"protocol_record \"$FOCUSED_PROTOCOL_ROOT/record\" exact && exit 0\nexit 9\n",
						),
						9,
					)
				case "positive removal":
					protocolRequireExit(
						t,
						protocolScript(
							t,
							root,
							"exit 0\n",
							protocolPolicy{noAnchor: true, builtinOnly: true},
						),
						0,
					)
				}
			}) {
				return
			}
			_, err := os.Stat(root)
			if name == "positive removal" {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("verified positive root not removed: %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("negative root removed by registered cleanup: %v", err)
				}
				waitProtocolExact(t, filepath.Join(root, "cleanup-complete"), "quiescent")
			}
		})
	}
}

func protocolCheckFatalOwner(t *testing.T) {
	root := protocolPrivateRoot(t)
	childRoot := protocolPrivateRoot(t)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestFocusedProtocolFatalOwnerFixture$")
	cmd.Env = protocolPrivateEnv(root, "FOCUSED_PROTOCOL_FATAL_ROOT="+childRoot)
	owner := startProtocolOwner(
		t,
		root,
		cmd,
		protocolPolicy{noAnchor: true, retain: true, nestedRoot: childRoot},
	)
	protocolRequireExit(t, owner, 1)
	data, truncated := owner.capture.snapshot()
	if truncated || !strings.Contains(string(data), "intentional private fatal owner unwind") {
		t.Fatalf("subprocess failed before the intended fatal boundary: %s", data)
	}
	waitProtocolExact(
		t,
		filepath.Join(childRoot, "read-enter-1"),
		protocolFrame("reader", filepath.Base(childRoot), "absent", "stage=build;cancelled=0"),
	)
	waitProtocolExact(
		t,
		filepath.Join(childRoot, "read-return-1"),
		protocolFrame(
			"reader",
			filepath.Base(childRoot),
			"absent",
			"stage=build;cancelled=0;code=0;class=wake",
		),
	)
	waitProtocolExact(
		t,
		filepath.Join(childRoot, "fatal-unwind"),
		"wait-calls=1;complete=true;retained=true",
	)
	if _, err := os.Stat(childRoot); err != nil {
		t.Fatalf("fatal root removed: %v", err)
	}
	if _, err := readProtocolRegular(
		filepath.Join(childRoot, "captured-output"),
		protocolCaptureLimit+512,
	); err != nil {
		t.Fatal(err)
	}
}

func protocolCheckStartFailure(t *testing.T) {
	root := protocolPrivateRoot(t)
	cmd := exec.Command(filepath.Join(root, "absent-executable"))
	cmd.Env = protocolPrivateEnv(root)
	owner, err := tryStartProtocolOwner(
		t,
		root,
		cmd,
		protocolPolicy{noAnchor: true, retain: true, queueBeforeRead: true, captureDuplicate: true},
	)
	if !errors.Is(err, os.ErrNotExist) || cmd.Process != nil {
		t.Fatalf("actual Start failure=%v process=%v", err, cmd.Process)
	}
	if owner.pulse == nil || owner.events == nil {
		t.Fatal("actual setup path was not exercised")
	}
	for _, file := range append(owner.cmd.ExtraFiles, owner.pulse.writer, owner.eventReader, owner.capture.reader, owner.capture.writer, owner.queueWriter, owner.captureDuplicate) {
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("Start failure leaked endpoint: %v", err)
		}
	}
	select {
	case <-owner.pulse.done:
	default:
		t.Fatal("Start failure pulse not joined")
	}
	select {
	case <-owner.events.done:
	default:
		t.Fatal("Start failure drainer not joined")
	}
	select {
	case <-owner.capture.done:
	default:
		t.Fatal("Start failure capture not joined")
	}
	if err := owner.capture.finish(); err != nil {
		t.Fatalf("Start failure capture incomplete: %v", err)
	}
	if owner.waitCalls != 0 {
		t.Fatal("Wait after failed Start")
	}
	data, err := readProtocolRegular(filepath.Join(root, "start-failure"), 2048)
	if err != nil || !strings.HasSuffix(string(data), ";endpoints-and-joins=<nil>;no-child") {
		t.Fatalf("Start failure ownership incomplete: %s,%v", data, err)
	}
}

func protocolCheckEventTermination(t *testing.T) {
	// State-machine reconciliation model; actual startup trap exits remain
	// covered by the unchanged startup/transition cancellation matrix.
	for _, name := range []string{"closed gate EOF before Wait", "nongated completion", "startup trap without return", "open gate without completion"} {
		t.Run(name, func(t *testing.T) {
			root := protocolPrivateRoot(t)
			events := &protocolEvents{
				root:  root,
				phase: "reader",
				epoch: "epoch",
				done:  make(chan struct{}),
			}
			wire := ""
			if name != "nongated completion" {
				wire = "boundary|reader|epoch|absent|ready\nread-enter-1|reader|epoch|absent|stage=build;cancelled=0\n"
			}
			if name == "startup trap without return" {
				wire += "cancel-1|reader|epoch|absent|cancelled=130\n"
			}
			if name == "closed gate EOF before Wait" {
				wire += "read-return-1|reader|epoch|absent|stage=build;cancelled=0;code=0;class=wake\nclosed|reader|epoch|absent|closed\n"
			}
			events.drain(strings.NewReader(wire))
			if err := events.health.failure(); err != nil {
				t.Fatal(err)
			}
			events.health.mu.Lock()
			ended := events.health.eventEnded
			events.health.mu.Unlock()
			if !ended {
				t.Fatal("EOF not recorded under health mutex")
			}
			completed := name == "nongated completion" || name == "startup trap without return"
			err := events.reconcileEnd(completed)
			if name == "open gate without completion" {
				if err == nil {
					t.Fatal("open gate EOF proved cleanup without completion")
				}
			} else if err != nil {
				t.Fatalf("valid terminal reconciliation rejected: %v", err)
			}
		})
	}
}

func protocolCheckCaptureEOF(t *testing.T) {
	root := protocolPrivateRoot(t)
	var owner *protocolOwner
	if !t.Run("registered cleanup retains incomplete capture", func(t *testing.T) {
		binary, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		exitRead, exitWrite, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = exitRead.Close(); _ = exitWrite.Close() })
		cmd := exec.Command(binary, "-test.run=^TestFocusedCancellationProcessFixture$")
		cmd.Stdin = exitRead
		cmd.Env = protocolPrivateEnv(
			root,
			"FOCUSED_PROTOCOL_ROOT="+root,
			"DOTTY_FOCUSED_CANCEL_FIXTURE=capture exit",
		)
		owner = startProtocolOwner(
			t,
			root,
			cmd,
			protocolPolicy{
				noAnchor:                  true,
				retain:                    true,
				builtinOnly:               true,
				captureDuplicate:          true,
				expectedCaptureIncomplete: true,
			},
		)
		if err := exitRead.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.capture.writer.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("parent capture writer not closed after Start: %v", err)
		}
		waitProtocolExact(
			t,
			filepath.Join(root, "capture-exit-ready"),
			fmt.Sprintf("pid=%d;group=%d", cmd.Process.Pid, cmd.Process.Pid),
		)
		writerInfo, err := owner.captureDuplicate.Stat()
		if err != nil {
			t.Fatal(err)
		}
		writerStat, ok := writerInfo.Sys().(*syscall.Stat_t)
		if !ok || writerStat.Ino == 0 {
			t.Fatal("capture writer native identity unavailable")
		}
		peerRead, peerWrite, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		peer := exec.Command(binary, "-test.run=^TestFocusedCancellationProcessFixture$")
		peer.Env = protocolPrivateEnv(
			root,
			"FOCUSED_PROTOCOL_ROOT="+root,
			"DOTTY_FOCUSED_CANCEL_FIXTURE=capture writer",
		)
		peer.Stdin = peerRead
		peer.Stdout, peer.Stderr = owner.captureDuplicate, owner.captureDuplicate
		peer.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: cmd.Process.Pid}
		peerDone := make(chan struct{})
		var peerErr error
		peerWaitCalls := 0
		started, peerJoined := false, false
		// Registered before peer Start and every publication/check. A failure
		// targets only this owned peer handle, never a group from a record.
		t.Cleanup(func() {
			_ = peerRead.Close()
			_ = peerWrite.Close()
			if started && !peerJoined {
				select {
				case <-peerDone:
					peerJoined = true
				default:
					_ = peer.Process.Kill()
				}
				if !peerJoined {
					select {
					case <-peerDone:
						peerJoined = true
					case <-time.After(time.Second):
						t.Error("capture peer Wait unjoined")
					}
				}
			}
			if started && peerJoined && peerWaitCalls != 1 {
				t.Errorf("capture peer Wait calls=%d", peerWaitCalls)
			}
		})
		if err := peer.Start(); err != nil {
			t.Fatal(err)
		}
		started = true
		go func() { peerWaitCalls++; peerErr = peer.Wait(); close(peerDone) }()
		if err := errors.Join(peerRead.Close(), owner.closeCaptureDuplicate()); err != nil {
			t.Fatal(err)
		}
		group, err := syscall.Getpgid(peer.Process.Pid)
		if err != nil || group != cmd.Process.Pid {
			t.Fatalf("peer did not join current owned group: %d,%v", group, err)
		}
		if _, waited := owner.wait(0); waited {
			t.Fatal("wrapper exited before peer binding")
		}
		waitProtocolExact(
			t,
			filepath.Join(root, "capture-writer-ready"),
			fmt.Sprintf(
				"pid=%d;group=%d;writer-dev=%d;writer-ino=%d",
				peer.Process.Pid,
				group,
				writerStat.Dev,
				writerStat.Ino,
			),
		)
		if err := exitWrite.Close(); err != nil {
			t.Fatal(err)
		}
		waitErr, waited := owner.wait(time.Second)
		var exitErr *exec.ExitError
		if !waited || !errors.As(waitErr, &exitErr) || exitErr.ExitCode() != 7 {
			t.Fatalf("primary exit7 lost: %v, waited=%v", waitErr, waited)
		}
		// Peer acknowledged the same native pipe identity and cannot close it
		// until our still-open control writer closes. Wait's ExitError cannot
		// prove EOF; this explicit drain must time out, force-close and join.
		captureErr := owner.capture.finish()
		var incomplete *protocolCleanupUncertain
		if !errors.As(captureErr, &incomplete) ||
			!strings.Contains(captureErr.Error(), "capture reader forced closed") {
			t.Fatalf("missing independent capture-incomplete reason: %v", captureErr)
		}
		owner.capture.mu.Lock()
		exactLoss := owner.capture.forced && owner.capture.joined && !owner.capture.naturalEOF &&
			!owner.capture.truncated &&
			owner.capture.closeErr == nil &&
			errors.Is(owner.capture.drainErr, os.ErrClosed)
		owner.capture.mu.Unlock()
		if !exactLoss {
			t.Fatal("capture loss included unexpected EOF/error/truncation/join state")
		}
		select {
		case <-peerDone:
			t.Fatal("writer disappeared before forced capture proof")
		default:
		}
		if err := peerWrite.Close(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-peerDone:
			peerJoined = true
		case <-time.After(time.Second):
			t.Fatal("capture writer did not join")
		}
		if peerErr != nil || peerWaitCalls != 1 {
			t.Fatalf("peer Wait=%v calls=%d", peerErr, peerWaitCalls)
		}
		waitProtocolExact(t, filepath.Join(root, "capture-writer-closed"), "closed")
		if err := protocolGroupAbsent(cmd.Process.Pid, time.Second); err != nil {
			t.Fatal(err)
		}
		if err := publishProtocolRecord(
			filepath.Join(root, "capture-peer-reaped"),
			fmt.Sprintf("pid=%d;wait-calls=1;group-absent", peer.Process.Pid),
		); err != nil {
			t.Fatal(err)
		}
		owner.cleanup()
		//nolint:errorlint // finish must return the identical cached incomplete-capture error.
		if owner.complete || !owner.processesQuiescent || owner.reason != captureErr.Error() ||
			owner.capture.finish() != captureErr {
			t.Fatalf(
				"late absence repaired/obscured capture loss: complete=%v processes=%v reason=%s",
				owner.complete,
				owner.processesQuiescent,
				owner.reason,
			)
		}
		//nolint:errorlint // Capture handling must retain the identical primary Wait result.
		if err, waited := owner.wait(0); !waited || err != waitErr || owner.waitCalls != 1 {
			t.Fatal("capture handling changed primary Wait result")
		}
	}) {
		return
	}
	if owner == nil || owner.complete || !owner.retain {
		t.Fatal("incomplete capture marked complete or not retained")
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("registered cleanup deleted incomplete capture: %v", err)
	}
	if _, err := os.Lstat(
		filepath.Join(root, "cleanup-complete"),
	); !errors.Is(
		err,
		os.ErrNotExist,
	) {
		t.Fatalf("false cleanup-complete record: %v", err)
	}
	data, err := readProtocolRegular(
		filepath.Join(root, "captured-output"),
		protocolCaptureLimit+1024,
	)
	if err != nil ||
		!strings.Contains(
			string(data),
			"capture-complete=false;natural-EOF=false;forced-close=true;joined=true",
		) {
		t.Fatalf("capture proof not preserved: %s,%v", data, err)
	}
}

// Private modes of the existing test binary; no helper executable. Control EOF
// is fixture lifetime only, unrelated to the shell gate's continuation protocol.
func protocolCaptureFixture(mode string) {
	root := os.Getenv("FOCUSED_PROTOCOL_ROOT")
	if root == "" {
		os.Exit(9)
	}
	time.AfterFunc(8*time.Second, func() { os.Exit(9) })
	group, err := syscall.Getpgid(0)
	if err != nil {
		os.Exit(9)
	}
	name, record := "capture-exit-ready", fmt.Sprintf("pid=%d;group=%d", os.Getpid(), group)
	if mode == "capture writer" {
		info, err := os.Stdout.Stat()
		if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
			os.Exit(9)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			os.Exit(9)
		}
		name = "capture-writer-ready"
		record += fmt.Sprintf(";writer-dev=%d;writer-ino=%d", stat.Dev, stat.Ino)
	}
	if err := publishProtocolRecord(filepath.Join(root, name), record); err != nil {
		os.Exit(9)
	}
	var b [1]byte
	if n, err := os.Stdin.Read(b[:]); n != 0 || err != io.EOF {
		os.Exit(9)
	}
	if mode == "capture exit" {
		os.Exit(7)
	}
	if err := errors.Join(os.Stdout.Close(), os.Stderr.Close()); err != nil {
		os.Exit(9)
	}
	if err := publishProtocolRecord(
		filepath.Join(root, "capture-writer-closed"),
		"closed",
	); err != nil {
		os.Exit(9)
	}
	os.Exit(0)
}

type protocolFailedReader struct{}

func (protocolFailedReader) Read(_ []byte) (int, error) { return 0, syscall.EIO }

func finishProtocolEvents(events *protocolEvents, reader *os.File, bound time.Duration) error {
	select {
	case <-events.done:
		if err := errors.Join(reader.Close(), events.drainErr); err != nil {
			return errors.Join(events.health.failure(), err)
		}
		return events.health.failure()
	case <-time.After(bound):
		// Closing our reader bounds the goroutine; it does NOT turn an open
		// writer into EOF evidence. Preserve uncertainty even if it now exits.
		closeErr := reader.Close()
		select {
		case <-events.done:
			return errors.Join(
				closeErr,
				&protocolCleanupUncertain{
					"event writer remained open; closed reader joined, output incomplete",
				},
			)
		case <-time.After(bound):
			return errors.Join(
				closeErr,
				&protocolCleanupUncertain{
					"event writer remains open and drainer unjoined; output incomplete",
				},
			)
		}
	}
}
