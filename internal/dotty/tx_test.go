package dotty

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunAtomicRollsBackInReverseOrderAndReportsRollbackFailure(t *testing.T) {
	var calls []string

	err := RunAtomic(func(tx *Tx) error {
		tx.AddRollback(func() error {
			calls = append(calls, "first")
			return nil
		})
		tx.AddRollback(func() error {
			calls = append(calls, "second")
			return errors.New("rollback failed")
		})
		return errors.New("operation failed")
	})

	requireErrorContains(t, err, "operation failed")
	requireErrorContains(t, err, "rollback failed")
	requireEqualStrings(t, calls, []string{"second", "first"})
}

func TestWriteFileTxRestoresPreviousFilesAndRemovesNewFilesOnRollback(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.txt")
	created := filepath.Join(dir, "nested", "created.txt")
	writeTextFile(t, existing, "old\n")
	requireNoError(t, os.Chmod(existing, 0o600))

	err := RunAtomic(func(tx *Tx) error {
		requireNoError(t, WriteFileTx(tx, existing, []byte("new\n"), 0o644))
		requireNoError(t, WriteFileTx(tx, created, []byte("created\n"), 0o644))
		return errors.New("stop")
	})
	requireErrorContains(t, err, "stop")

	requireFileContent(t, existing, "old\n")
	info, err := os.Stat(existing)
	requireNoError(t, err)
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("rollback should restore previous mode 0600, got %04o", got)
	}
	requireNoPath(t, created)
	requireNoPath(t, filepath.Dir(created))
}

func TestWriteFileTxRejectsNonRegularExistingDestinationWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "dotty.toml")
	requireNoError(t, syscall.Mkfifo(fifo, 0o600))

	done := make(chan error, 1)
	go func() {
		done <- RunAtomic(func(tx *Tx) error {
			return WriteFileTx(tx, fifo, []byte("version = 1\n"), 0o644)
		})
	}()

	select {
	case err := <-done:
		requireErrorContains(t, err, "not a regular file")
	case <-time.After(250 * time.Millisecond):
		t.Fatal("WriteFileTx blocked on existing FIFO instead of rejecting it")
	}

	info, err := os.Lstat(fifo)
	requireNoError(t, err)
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("expected FIFO to remain unchanged, mode=%v", info.Mode())
	}
}

func TestWriteFileTxRollbackDoesNotFollowSymlinkSwappedAtPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.toml")
	protected := filepath.Join(dir, "protected.txt")
	writeTextFile(t, path, "old state\n")
	writeTextFile(t, protected, "protected content\n")

	err := RunAtomic(func(tx *Tx) error {
		requireNoError(t, WriteFileTx(tx, path, []byte("new state\n"), 0o644))
		requireNoError(t, os.Remove(path))
		requireNoError(t, os.Symlink(protected, path))
		return errors.New("stop")
	})
	requireErrorContains(t, err, "stop")

	requireFileContent(t, protected, "protected content\n")
	info, err := os.Lstat(path)
	requireNoError(t, err)
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("rollback should restore a regular file without following swapped symlink")
	}
	requireFileContent(t, path, "old state\n")
}

func TestMoveAsideTxRestoresOnRollbackAndCleansBackupOnCommit(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	writeTextFile(t, target, "conflict\n")

	err := RunAtomic(func(tx *Tx) error {
		requireNoError(t, MoveAsideTx(tx, target))
		requireNoPath(t, target)
		return errors.New("stop")
	})
	requireErrorContains(t, err, "stop")
	requireFileContent(t, target, "conflict\n")

	requireNoError(t, RunAtomic(func(tx *Tx) error {
		return MoveAsideTx(tx, target)
	}))
	requireNoPath(t, target)
	requireNoDottyBackups(t, dir)
}

func TestMovePathTxFallsBackOnCrossDeviceRenameAndCommits(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	writeTextFile(t, filepath.Join(src, "nested", "config"), "enabled = true\n")
	requireNoError(t, os.Symlink("nested/config", filepath.Join(src, "config.link")))
	forceRenameError(t, syscall.EXDEV)

	requireNoError(t, RunAtomic(func(tx *Tx) error {
		return MovePathTx(tx, src, dst)
	}))

	requireNoPath(t, src)
	requireFileContent(t, filepath.Join(dst, "nested", "config"), "enabled = true\n")
	assertSymlink(t, filepath.Join(dst, "config.link"), "nested/config")
}

func TestMovePathTxFallsBackOnCrossDeviceRenameAndRollsBack(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	writeTextFile(t, filepath.Join(src, "nested", "config"), "enabled = true\n")
	requireNoError(t, os.Symlink("nested/config", filepath.Join(src, "config.link")))
	forceRenameError(t, syscall.EXDEV)

	err := RunAtomic(func(tx *Tx) error {
		requireNoError(t, MovePathTx(tx, src, dst))
		return errors.New("stop")
	})
	requireErrorContains(t, err, "stop")

	requireFileContent(t, filepath.Join(src, "nested", "config"), "enabled = true\n")
	assertSymlink(t, filepath.Join(src, "config.link"), "nested/config")
	requireNoPath(t, dst)
}

func TestMovePathTxCrossDeviceFallbackKeepsCopiedDestinationWhenRemoveFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can remove paths regardless of permission bits")
	}
	dir := t.TempDir()
	parent := filepath.Join(dir, "parent")
	src := filepath.Join(parent, "src")
	dst := filepath.Join(dir, "dst")
	writeTextFile(t, filepath.Join(src, "config"), "enabled = true\n")
	requireNoError(t, os.Chmod(parent, 0o555))
	t.Cleanup(func() {
		_ = os.Chmod(parent, 0o755)
	})
	forceRenameError(t, syscall.EXDEV)

	err := RunAtomic(func(tx *Tx) error {
		return MovePathTx(tx, src, dst)
	})
	requireErrorContains(t, err, "remove source")
	requireFileContent(t, filepath.Join(dst, "config"), "enabled = true\n")
}

func TestMovePathTxDoesNotFallbackOnOtherRenameErrors(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	writeTextFile(t, filepath.Join(src, "config"), "enabled = true\n")
	forceRenameError(t, syscall.EPERM)

	err := RunAtomic(func(tx *Tx) error {
		return MovePathTx(tx, src, dst)
	})
	requireErrorContains(t, err, "operation not permitted")
	requireFileContent(t, filepath.Join(src, "config"), "enabled = true\n")
	requireNoPath(t, dst)
}

func TestMoveAsideTxFallsBackOnCrossDeviceRenameAndRollsBack(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	writeTextFile(t, target, "conflict\n")
	forceRenameError(t, syscall.EXDEV)

	err := RunAtomic(func(tx *Tx) error {
		requireNoError(t, MoveAsideTx(tx, target))
		requireNoPath(t, target)
		return errors.New("stop")
	})
	requireErrorContains(t, err, "stop")
	requireFileContent(t, target, "conflict\n")
	requireNoDottyBackups(t, dir)
}

func TestMoveAsideTxKeepsBackupWhenRollbackRestoreFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	writeTextFile(t, target, "conflict\n")
	originalRename := renamePath

	err := RunAtomic(func(tx *Tx) error {
		requireNoError(t, MoveAsideTx(tx, target))
		renamePath = func(oldpath, newpath string) error {
			return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EPERM}
		}
		return errors.New("stop")
	})
	renamePath = originalRename

	requireErrorContains(t, err, "rollback failed")
	requireNoPath(t, target)
	requireDottyBackup(t, dir)
}

func TestCopyPathTxCopiesDirectorySymlinksAndRollsBack(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	writeTextFile(t, filepath.Join(src, "config", "app.conf"), "enabled = true\n")
	requireNoError(t, os.Symlink("config/app.conf", filepath.Join(src, "app.conf.link")))

	err := RunAtomic(func(tx *Tx) error {
		requireNoError(t, CopyPathTx(tx, src, dst))
		requireFileContent(t, filepath.Join(dst, "config", "app.conf"), "enabled = true\n")
		linkTarget, err := os.Readlink(filepath.Join(dst, "app.conf.link"))
		requireNoError(t, err)
		if linkTarget != "config/app.conf" {
			t.Fatalf("copied symlink target mismatch: want config/app.conf, got %s", linkTarget)
		}
		return errors.New("stop")
	})
	requireErrorContains(t, err, "stop")
	requireNoPath(t, dst)
}

func TestCreateSymlinkTxRollbackRemovesCreatedParents(t *testing.T) {
	dir := t.TempDir()
	keptParent := filepath.Join(dir, "home")
	source := filepath.Join(dir, "source")
	target := filepath.Join(keptParent, ".config", "zsh", ".zshrc")
	writeTextFile(t, source, "export EDITOR=vim\n")
	requireNoError(t, os.MkdirAll(keptParent, 0o755))

	err := RunAtomic(func(tx *Tx) error {
		requireNoError(t, CreateSymlinkTx(tx, source, target))
		assertSymlink(t, target, source)
		return errors.New("stop")
	})
	requireErrorContains(t, err, "stop")

	requireNoPath(t, target)
	requireNoPath(t, filepath.Join(keptParent, ".config"))
	if _, err := os.Lstat(keptParent); err != nil {
		t.Fatalf("pre-existing parent should remain: %v", err)
	}
}

func TestCopyPathTxRejectsExistingDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	writeTextFile(t, src, "source\n")
	writeTextFile(t, dst, "destination\n")

	err := RunAtomic(func(tx *Tx) error {
		return CopyPathTx(tx, src, dst)
	})
	requireErrorContains(t, err, "destination")
	requireFileContent(t, dst, "destination\n")
}

func requireNoDottyBackups(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	requireNoError(t, err)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".dotty-backup-") {
			t.Fatalf("backup %s was not cleaned up", entry.Name())
		}
	}
}

func requireDottyBackup(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	requireNoError(t, err)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".dotty-backup-") {
			return
		}
	}
	t.Fatalf("expected a .dotty-backup-* path in %s", dir)
}

func requireDottyBackupContent(t *testing.T, dir, want string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	requireNoError(t, err)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".dotty-backup-") {
			requireFileContent(t, filepath.Join(dir, entry.Name()), want)
			return
		}
	}
	t.Fatalf("expected a .dotty-backup-* path in %s", dir)
}

func forceRenameError(t *testing.T, errno syscall.Errno) {
	t.Helper()
	original := renamePath
	renamePath = func(oldpath, newpath string) error {
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: errno}
	}
	t.Cleanup(func() {
		renamePath = original
	})
}

func forceCopyPathErrorAfter(t *testing.T, successfulCopies int, err error) {
	t.Helper()
	original := copyPathOp
	calls := 0
	copyPathOp = func(src, dst string) error {
		if calls >= successfulCopies {
			return err
		}
		calls++
		return original(src, dst)
	}
	t.Cleanup(func() {
		copyPathOp = original
	})
}

func forceRemovePathErrorAfter(t *testing.T, successfulRemoves int, err error) {
	t.Helper()
	original := removePath
	calls := 0
	removePath = func(path string) error {
		if calls >= successfulRemoves {
			return err
		}
		calls++
		return original(path)
	}
	t.Cleanup(func() {
		removePath = original
	})
}

func forceRemoveAllPathError(t *testing.T, err error) {
	t.Helper()
	original := removeAllPath
	removeAllPath = func(path string) error {
		return err
	}
	t.Cleanup(func() {
		removeAllPath = original
	})
}

func forceSymlinkPathError(t *testing.T, err error) {
	t.Helper()
	original := symlinkPath
	symlinkPath = func(oldname, newname string) error {
		return err
	}
	t.Cleanup(func() {
		symlinkPath = original
	})
}
