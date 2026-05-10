package dotty

import (
	"os"
	"path/filepath"
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
