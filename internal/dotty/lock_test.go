package dotty

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestWithRepositoryLockSerializesCallbacks(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "dotfiles")
	requireNoError(t, os.MkdirAll(repo, 0o755))

	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- withRepositoryLock(repo, func() error {
			close(firstEntered)
			<-releaseFirst
			return nil
		})
	}()

	select {
	case <-firstEntered:
	case <-time.After(time.Second):
		t.Fatal("first callback did not acquire repository lock")
	}

	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- withRepositoryLock(repo, func() error {
			close(secondEntered)
			return nil
		})
	}()

	select {
	case <-secondEntered:
		t.Fatal("second callback entered while first callback held repository lock")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseFirst)
	requireNoError(t, <-firstDone)

	select {
	case <-secondEntered:
	case <-time.After(time.Second):
		t.Fatal("second callback did not acquire repository lock after first released it")
	}
	requireNoError(t, <-secondDone)
}

func TestWithRepositoryLockSerializesSymlinkAliasRepositories(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "dotfiles")
	repoAlias := filepath.Join(root, "dotfiles-alias")
	requireNoError(t, os.MkdirAll(repo, 0o755))
	requireNoError(t, os.Symlink(repo, repoAlias))

	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- withRepositoryLock(repo, func() error {
			close(firstEntered)
			<-releaseFirst
			return nil
		})
	}()

	select {
	case <-firstEntered:
	case <-time.After(time.Second):
		t.Fatal("first callback did not acquire repository lock")
	}

	secondEntered := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- withRepositoryLock(repoAlias, func() error {
			close(secondEntered)
			return nil
		})
	}()

	select {
	case <-secondEntered:
		t.Fatal("alias callback entered while real repository path held lock")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseFirst)
	requireNoError(t, <-firstDone)

	select {
	case <-secondEntered:
	case <-time.After(time.Second):
		t.Fatal("alias callback did not acquire repository lock after real path released it")
	}
	requireNoError(t, <-secondDone)
}

func TestWithRepositoryLockUsesFlockAcrossProcesses(t *testing.T) {
	if os.Getenv("DOTTY_LOCK_TEST_CHILD") == "1" {
		runRepositoryLockChild(t)
		return
	}

	root := t.TempDir()
	repo := filepath.Join(root, "dotfiles")
	requireNoError(t, os.MkdirAll(repo, 0o755))
	attemptingPath := filepath.Join(root, "child-attempting")
	enteredPath := filepath.Join(root, "child-entered")

	executable, err := os.Executable()
	requireNoError(t, err)
	var childOutput bytes.Buffer
	cmd := exec.Command(
		executable,
		"-test.run=^TestWithRepositoryLockUsesFlockAcrossProcesses$",
	)
	cmd.Env = append(
		os.Environ(),
		"DOTTY_LOCK_TEST_CHILD=1",
		"DOTTY_LOCK_TEST_REPO="+repo,
		"DOTTY_LOCK_TEST_ATTEMPTING="+attemptingPath,
		"DOTTY_LOCK_TEST_ENTERED="+enteredPath,
	)
	cmd.Stdout = &childOutput
	cmd.Stderr = &childOutput
	t.Cleanup(func() {
		if cmd.Process != nil && cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	err = withRepositoryLock(repo, func() error {
		if err := cmd.Start(); err != nil {
			return err
		}
		if err := waitForPath(attemptingPath, time.Second); err != nil {
			return err
		}
		if err := waitForPath(enteredPath, 150*time.Millisecond); err == nil {
			return fmt.Errorf("child process entered repository lock while parent held flock")
		}
		return nil
	})
	requireNoError(t, err)

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("child lock process failed: %v\n%s", err, childOutput.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatalf(
			"child lock process did not finish after parent released flock\n%s",
			childOutput.String(),
		)
	}
	requireFileContent(t, enteredPath, "entered\n")
}

func runRepositoryLockChild(t *testing.T) {
	t.Helper()
	repo := os.Getenv("DOTTY_LOCK_TEST_REPO")
	attemptingPath := os.Getenv("DOTTY_LOCK_TEST_ATTEMPTING")
	enteredPath := os.Getenv("DOTTY_LOCK_TEST_ENTERED")
	if repo == "" || attemptingPath == "" || enteredPath == "" {
		t.Fatal("missing child repository lock test environment")
	}

	originalFlock := flock
	signaledAttempt := false
	flock = func(file *os.File, how int) error {
		if how == syscall.LOCK_EX && !signaledAttempt {
			signaledAttempt = true
			writeTextFile(t, attemptingPath, "attempting\n")
		}
		return originalFlock(file, how)
	}
	requireNoError(t, withRepositoryLock(repo, func() error {
		return os.WriteFile(enteredPath, []byte("entered\n"), 0o644)
	}))
}

func waitForPath(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Lstat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
