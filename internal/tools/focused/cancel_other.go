//go:build !darwin && !linux

package main

import (
	"errors"
	"os"
	"os/exec"
)

// Refuse before starting Go rather than claim descendant cancellation on a
// platform without the focused runner's process-group implementation.
func prepareChild(_ *exec.Cmd) error {
	return errors.New(
		"focused execution requires Linux or macOS for owned process-group cancellation",
	)
}

func cancellationSignals() (<-chan os.Signal, func()) {
	return nil, func() {}
}

func watchChild(_ *exec.Cmd, _ <-chan os.Signal) func() int {
	return func() int { return 0 }
}

func childSignalExitCode(_ *exec.ExitError) int {
	return 1
}
