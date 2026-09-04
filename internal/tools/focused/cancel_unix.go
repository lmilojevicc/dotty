//go:build darwin || linux

package main

import (
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func prepareChild(cmd *exec.Cmd) error {
	if cmd.Err != nil {
		return cmd.Err
	}
	// Validate the already resolved executable, relative to Dir when needed;
	// do not search PATH again for the original command. Keep ordinary missing
	// and non-executable paths as startup failures rather than shell exit 127/126.
	path := cmd.Path
	if !filepath.IsAbs(path) {
		var err error
		path, err = filepath.Abs(filepath.Join(cmd.Dir, path))
		if err != nil {
			return err
		}
	}
	if _, err := exec.LookPath(path); err != nil {
		return err
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		return err
	}
	// A non-interactive Bash anchor synchronously waits with stdout open even
	// if Go closes its own stdout early. The no-op traps and trailing status
	// handling prevent exec optimization; job control stays disabled, so Go
	// and its descendants share the anchor's group. This is for trusted Go
	// execution, not arbitrary-command validation: a missing interpreter or a
	// later executable race can still become shell exit 126/127.
	const anchor = "trap ':' INT TERM\n\"$@\"\ncode=$?\nexit \"$code\""
	args := []string{bash, "-c", anchor, "focused-child", path}
	if len(cmd.Args) > 1 {
		args = append(args, cmd.Args[1:]...)
	}
	cmd.Path, cmd.Args = bash, args
	// Only this newly created group is signalled. Never use the caller's
	// terminal group: it may contain mise, editors, or unrelated work.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	return nil
}

func cancellationSignals() (<-chan os.Signal, func()) {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	return signals, func() { signal.Stop(signals) }
}

func watchChild(cmd *exec.Cmd, signals <-chan os.Signal) func() int {
	finished := make(chan struct{})
	result := make(chan int, 1)
	go func() {
		var sig os.Signal
		select {
		case sig = <-signals:
		case <-finished:
			// Prefer a queued cancellation over ordinary completion.
			select {
			case sig = <-signals:
			default:
				result <- 0
				return
			}
		}
		unixSignal := sig.(syscall.Signal)
		_ = syscall.Kill(-cmd.Process.Pid, unixSignal)
		// Even if the driver exits promptly, descendants may ignore the
		// forwarded signal and retain pipes. The direct-child Bash anchor is
		// not reaped until ALL group signalling finishes, reserving its PID
		// (and thus the process-group ID) through forwarding and escalation.
		// No bound is promised for blocked output sinks, uninterruptible tasks,
		// or descendants that escape this group.
		time.Sleep(250 * time.Millisecond)
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		result <- 128 + int(unixSignal)
	}()
	return func() int {
		close(finished)
		return <-result
	}
}

func childSignalExitCode(err *exec.ExitError) int {
	if status, ok := err.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return 1
}
