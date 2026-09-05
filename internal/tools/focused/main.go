// Command focused is the implementation of mise run test:focused, not a Dotty command.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// notifyWrapperReady sends readiness only after signal subscription, then waits
// for the wrapper's ACK before any separately grouped child can start. Without
// that barrier a damaged readiness read could strand work during wrapper abort.
// Close both private descriptors and remove the marker before starting children.
func notifyWrapperReady() error {
	if os.Getenv("DOTTY_FOCUSED_WRAPPER") != "1" {
		return nil
	}
	if err := os.Unsetenv("DOTTY_FOCUSED_WRAPPER"); err != nil {
		return err
	}
	ready := os.NewFile(3, "focused-wrapper-status")
	if ready == nil {
		return errors.New("invalid focused wrapper status descriptor")
	}
	_, err := ready.WriteString("RUNNER_READY\n")
	closeErr := ready.Close()
	if err := errors.Join(err, closeErr); err != nil {
		return err
	}
	return awaitWrapperAck()
}

func awaitWrapperAck() error {
	control := os.NewFile(4, "focused-wrapper-control")
	if control == nil {
		return errors.New("invalid focused wrapper control descriptor")
	}
	// Read exactly one frame, without buffering RELEASE for the anchor. The
	// anchor does not read control again until this runner has exited.
	var ack [4]byte
	_, err := io.ReadFull(control, ack[:])
	closeErr := control.Close()
	if err := errors.Join(err, closeErr); err != nil {
		return err
	}
	if string(ack[:]) != "ACK\n" {
		return fmt.Errorf("invalid focused wrapper acknowledgment %q", ack[:])
	}
	return nil
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 || args[0] == "" || args[1] == "" {
		fmt.Fprintln(
			stderr,
			"usage: test:focused PACKAGE PATTERN (one Go package argument and one -run pattern)",
		)
		return 2
	}
	// Explicit flags override GOFLAGS. Disable caching: cached JSON is not
	// evidence that tests executed during this invocation.
	cmd := exec.Command("go", "test", args[0], "-json", "-count=1", "-run", args[1])
	cmd.Stdin = os.Stdin
	return execute(cmd, args[1], stdout, stderr)
}

type testEvent struct {
	Action  string
	Package string
	Test    string
	Output  string
}

type testID struct {
	pkg  string
	name string
}

func execute(cmd *exec.Cmd, pattern string, stdout, stderr io.Writer) int {
	selection, err := compileSelection(pattern)
	if err != nil {
		fmt.Fprintln(stderr, "focused:", err)
		return 1
	}
	if err := prepareChild(cmd); err != nil {
		fmt.Fprintln(stderr, "focused:", err)
		return 1
	}
	signals, stopSignals := cancellationSignals()
	defer stopSignals()
	if err := notifyWrapperReady(); err != nil {
		fmt.Fprintln(stderr, "focused: wrapper readiness:", err)
		return 1
	}
	// A cancellation forwarded between READY and ACK is already subscribed.
	// Do not launch child work for a cancellation queued during that handshake.
	select {
	case sig := <-signals:
		fmt.Fprintln(stderr, "focused: cancelled before child startup")
		if sig == os.Interrupt {
			return 130
		}
		return 143 // cancellationSignals subscribes only INT and TERM.
	default:
	}
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintln(stderr, "focused:", err)
		return 1
	}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		_ = pipe.Close()
		fmt.Fprintln(stderr, "focused:", err)
		return 1
	}

	finishChild := watchChild(cmd, signals)
	active := make(map[testID]bool)
	executed := make(map[testID]bool)
	// A false entry is a started package still awaiting its terminal event.
	// Track every package, including those that never fully match selection.
	packages := make(map[string]bool)
	failed := false
	var streamErr error
	decoder := json.NewDecoder(pipe)
	for {
		var event testEvent
		err := decoder.Decode(&event)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			streamErr = fmt.Errorf("read go test JSON: %w", err)
			// Drain before Wait, even on malformed output, so a child cannot
			// block on its stdout pipe and its original exit status survives.
			_, _ = io.Copy(io.Discard, pipe)
			break
		}
		if event.Output != "" {
			if _, err := io.WriteString(stdout, event.Output); err != nil && streamErr == nil {
				streamErr = fmt.Errorf("write test output: %w", err)
			}
		}
		if event.Action == "fail" || event.Action == "build-fail" {
			failed = true
		}
		protocolError := func() {
			if streamErr == nil {
				streamErr = fmt.Errorf(
					"invalid go test JSON: %s for package %q test %q",
					event.Action,
					event.Package,
					event.Test,
				)
			}
		}
		switch event.Action {
		case "output", "build-start", "build-output", "build-fail":
			continue
		case "start", "run", "pass", "fail", "skip", "pause", "cont", "bench":
		default:
			protocolError()
			continue
		}
		complete, started := packages[event.Package]
		if event.Package == "" {
			protocolError()
		}
		if event.Test == "" {
			switch event.Action {
			case "start":
				if started {
					protocolError()
				}
				packages[event.Package] = false
			case "pass", "fail", "skip":
				if !started || complete {
					protocolError()
				}
				for id := range active {
					if id.pkg == event.Package {
						protocolError()
					}
				}
				packages[event.Package] = true
			default:
				protocolError()
			}
			continue
		}
		if !started || complete {
			protocolError()
		}
		id := testID{event.Package, event.Test}
		switch event.Action {
		case "run":
			if active[id] {
				protocolError()
			}
			active[id] = true
		case "pass", "fail", "skip":
			if !active[id] {
				protocolError()
			}
			if active[id] && event.Action != "skip" && selection.fullMatch(event.Test) {
				executed[id] = true
			}
			delete(active, id)
		case "pause", "cont":
			if !active[id] {
				protocolError()
			}
		case "bench":
		default:
			protocolError()
		}
	}
	// On Unix, the direct-child Bash anchor holds stdout open while waiting
	// for Go, so early driver EOF cannot stop the watcher while Go is alive.
	// Preserve drain -> finishChild -> Wait: the unreaped anchor reserves its
	// PID through ALL group signalling, even if the driver already exited.
	// A concurrent Wait would release that reservation before escalation.
	cancelCode := finishChild()
	commandErr := cmd.Wait()
	if cancelCode != 0 {
		fmt.Fprintln(stderr, "focused: cancelled; child process group stopped")
		return cancelCode
	}
	for pkg, complete := range packages {
		if !complete && streamErr == nil {
			streamErr = fmt.Errorf("incomplete go test JSON for package %q", pkg)
		}
	}

	var names []string
	for id := range executed {
		if id.pkg == "" || !packages[id.pkg] {
			if streamErr == nil {
				streamErr = fmt.Errorf("incomplete go test JSON for package %q", id.pkg)
			}
			continue
		}
		names = append(names, id.pkg+" "+id.name)
	}
	if len(active) != 0 && streamErr == nil {
		streamErr = errors.New("incomplete go test JSON: tests started without terminal events")
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := fmt.Fprintln(stdout, "executed:", name); err != nil && streamErr == nil {
			streamErr = fmt.Errorf("write execution report: %w", err)
		}
	}
	if streamErr != nil {
		fmt.Fprintln(stderr, "focused:", streamErr)
	}
	if commandErr != nil {
		fmt.Fprintln(stderr, "focused: go test:", commandErr)
		var exitErr *exec.ExitError
		if errors.As(commandErr, &exitErr) {
			if code := exitErr.ExitCode(); code >= 0 {
				return code
			}
			return childSignalExitCode(exitErr)
		}
		return 1
	}
	if len(names) == 0 {
		fmt.Fprintf(
			stderr,
			"focused: no non-skipped executed tests matched the full -run selection %q\n",
			pattern,
		)
		return 1
	}
	if streamErr != nil || failed {
		return 1
	}
	return 0
}
