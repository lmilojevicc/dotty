package dotty

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

var repositoryLocks = struct {
	sync.Mutex
	locks map[string]*sync.Mutex
}{locks: map[string]*sync.Mutex{}}

func withRepositoryLock(repo string, fn func() error) (err error) {
	return withRepositoryLockTarget(repo, repo, fn)
}

func withRepositoryInitLock(repo string, fn func() error) (err error) {
	lockTarget, err := nearestExistingDir(repo)
	if err != nil {
		return err
	}
	return withRepositoryLockTarget(repo, lockTarget, fn)
}

func withRepositoryLockTarget(repo, target string, fn func() error) (err error) {
	mu := repositoryMutex(repo)
	mu.Lock()
	defer mu.Unlock()

	lockTarget, openErr := os.Open(target)
	if openErr != nil {
		return fmt.Errorf("open repository lock %s: %w", target, openErr)
	}
	defer func() {
		if closeErr := lockTarget.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close repository lock %s: %w", target, closeErr))
		}
	}()

	if lockErr := flock(lockTarget, syscall.LOCK_EX); lockErr != nil {
		return fmt.Errorf("lock repository %s: %w", repo, lockErr)
	}
	defer func() {
		if unlockErr := flock(lockTarget, syscall.LOCK_UN); unlockErr != nil {
			err = errors.Join(err, fmt.Errorf("unlock repository %s: %w", repo, unlockErr))
		}
	}()

	return fn()
}

func nearestExistingDir(path string) (string, error) {
	current := filepath.Clean(path)
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if info.IsDir() {
				return current, nil
			}
			current = filepath.Dir(current)
			continue
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect repository lock path %s: %w", current, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("find repository lock directory for %s: %w", path, err)
		}
		current = parent
	}
}

func repositoryMutex(repo string) *sync.Mutex {
	key := repositoryLockKey(repo)
	repositoryLocks.Lock()
	defer repositoryLocks.Unlock()
	mu := repositoryLocks.locks[key]
	if mu == nil {
		mu = &sync.Mutex{}
		repositoryLocks.locks[key] = mu
	}
	return mu
}

func repositoryLockKey(repo string) string {
	resolved, err := filepath.EvalSymlinks(repo)
	if err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(repo)
}

func flock(file *os.File, how int) error {
	for {
		err := syscall.Flock(int(file.Fd()), how)
		if err != syscall.EINTR {
			return err
		}
	}
}
