package dotty

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathExistsReportsExistingMissingAndBrokenSymlink(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "file")
	missing := filepath.Join(dir, "missing")
	broken := filepath.Join(dir, "broken")
	writeTextFile(t, existing, "content\n")
	requireNoError(t, os.Symlink(filepath.Join(dir, "nope"), broken))

	exists, err := pathExists(existing)
	requireNoError(t, err)
	if !exists {
		t.Fatal("existing file should exist")
	}
	exists, err = pathExists(missing)
	requireNoError(t, err)
	if exists {
		t.Fatal("missing file should not exist")
	}
	exists, err = pathExists(broken)
	requireNoError(t, err)
	if !exists {
		t.Fatal("broken symlink should exist according to lstat")
	}
}

func TestSymlinkPointsToMatchesAbsoluteAndRelativeTargets(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	absLink := filepath.Join(dir, "absolute")
	relLink := filepath.Join(dir, "relative")
	writeTextFile(t, target, "content\n")
	requireNoError(t, os.Symlink(target, absLink))
	requireNoError(t, os.Symlink("target", relLink))

	if !symlinkPointsTo(absLink, target) {
		t.Fatal("absolute symlink should point to target")
	}
	if !symlinkPointsTo(relLink, target) {
		t.Fatal("relative symlink should point to target")
	}
	if symlinkPointsTo(relLink, filepath.Join(dir, "other")) {
		t.Fatal("symlink should not match a different target")
	}
	if symlinkPointsTo(target, target) {
		t.Fatal("regular file should not be treated as a matching symlink")
	}
}

func TestSameExistingPathHandlesIdenticalPathsSymlinkAliasesAndBrokenSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	alias := filepath.Join(dir, "alias")
	brokenA := filepath.Join(dir, "broken-a")
	brokenB := filepath.Join(dir, "broken-b")
	writeTextFile(t, target, "content\n")
	requireNoError(t, os.Symlink(target, alias))
	requireNoError(t, os.Symlink(filepath.Join(dir, "missing"), brokenA))
	requireNoError(t, os.Symlink(filepath.Join(dir, "missing"), brokenB))

	if !sameExistingPath(target, target) {
		t.Fatal("identical paths should match")
	}
	if !sameExistingPath(target, alias) {
		t.Fatal("symlink alias should match its resolved target")
	}
	if sameExistingPath(brokenA, brokenB) {
		t.Fatal("broken symlinks are currently treated as different paths")
	}
}
