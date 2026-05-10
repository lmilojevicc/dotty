package dotty

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCopyPathCopiesDirectoryFileAndSymlink(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	writeTextFile(t, filepath.Join(src, "nested", "config"), "enabled = true\n")
	requireNoError(t, os.Symlink("nested/config", filepath.Join(src, "config.link")))

	requireNoError(t, copyPath(src, dst))
	requireFileContent(t, filepath.Join(dst, "nested", "config"), "enabled = true\n")
	assertSymlink(t, filepath.Join(dst, "config.link"), "nested/config")
}

func TestCopyPathRejectsExistingDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	writeTextFile(t, src, "source\n")
	writeTextFile(t, dst, "destination\n")

	requireErrorContains(t, copyPath(src, dst), "destination")
	requireFileContent(t, dst, "destination\n")
}

func TestCopyPathTxRejectsUnsupportedFileTypeAndRemovesPartialDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	writeTextFile(t, filepath.Join(src, "regular"), "content\n")
	requireNoError(t, syscall.Mkfifo(filepath.Join(src, "fifo"), 0o600))

	err := RunAtomic(func(tx *Tx) error {
		return CopyPathTx(tx, src, dst)
	})
	requireErrorContains(t, err, "unsupported file type")
	requireNoPath(t, dst)
}

func TestCopyPathTxCommitsCopiedPath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	writeTextFile(t, filepath.Join(src, "config"), "content\n")

	requireNoError(t, RunAtomic(func(tx *Tx) error {
		return CopyPathTx(tx, src, dst)
	}))

	requireFileContent(t, filepath.Join(dst, "config"), "content\n")
}

func TestCopyPathTxRejectsUnreadableSourceAndRemovesPartialDestination(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read files regardless of permission bits")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	writeTextFile(t, filepath.Join(src, "regular"), "content\n")
	secret := filepath.Join(src, "secret")
	writeTextFile(t, secret, "secret\n")
	requireNoError(t, os.Chmod(secret, 0))
	t.Cleanup(func() {
		_ = os.Chmod(secret, 0o600)
	})

	err := RunAtomic(func(tx *Tx) error {
		return CopyPathTx(tx, src, dst)
	})
	requireErrorContains(t, err, "open")
	requireNoPath(t, dst)
}

func TestValidateCopyablePathAcceptsRegularDirectoryAndSymlink(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	writeTextFile(t, filepath.Join(source, "nested", "config"), "enabled = true\n")
	requireNoError(t, os.Symlink("nested/config", filepath.Join(source, "config.link")))

	requireNoError(t, validateCopyablePath(source))
}

func TestValidateCopyablePathRejectsUnsupportedFileType(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	requireNoError(t, syscall.Mkfifo(fifo, 0o600))

	requireErrorContains(t, validateCopyablePath(fifo), "unsupported file type")
}

func TestMissingDirsReturnsChildFirstMissingParentsAndRejectsFileParent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "one", "two", "three")

	missing, err := missingDirs(path)
	requireNoError(t, err)
	requireEqualStrings(
		t,
		missing,
		[]string{path, filepath.Dir(path), filepath.Dir(filepath.Dir(path))},
	)

	fileParent := filepath.Join(dir, "file")
	writeTextFile(t, fileParent, "not a directory\n")
	_, err = missingDirs(filepath.Join(fileParent, "child"))
	requireErrorContains(t, err, "not a directory")
}
