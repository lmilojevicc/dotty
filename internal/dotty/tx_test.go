package dotty

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
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

	err := RunAtomic(func(tx *Tx) error {
		requireNoError(t, WriteFileTx(tx, existing, []byte("new\n"), 0o644))
		requireNoError(t, WriteFileTx(tx, created, []byte("created\n"), 0o644))
		return errors.New("stop")
	})
	requireErrorContains(t, err, "stop")

	requireFileContent(t, existing, "old\n")
	requireNoPath(t, created)
	requireNoPath(t, filepath.Dir(created))
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
