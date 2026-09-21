//go:build (!linux && !darwin) || (darwin && !cgo)

package nativefs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateUnavailable(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("DOTTY_REPO", filepath.Join(root, "repo"))
	var d Dir
	name, err := ParseComponent("never-created")
	if err != nil {
		t.Fatal(err)
	}
	check := func(err error) {
		t.Helper()
		var e *AuthorityError
		if !errors.As(err, &e) || e.Reason != ReasonUnsupported {
			t.Fatalf("unavailable backend did not fail closed: %v", err)
		}
	}
	child, err := d.OpenPrivateDir(name)
	check(err)
	if child != nil {
		t.Fatal("unsupported directory returned")
	}
	child, record, err := d.CreatePrivateDir(name)
	check(err)
	if child != nil || record.Created() {
		t.Fatal("unsupported directory creation")
	}
	f, err := d.OpenLockFile(name)
	check(err)
	if f != nil {
		t.Fatal("unsupported file returned")
	}
	f, record, err = d.CreateLockFile(name)
	check(err)
	if f != nil || record.Created() {
		t.Fatal("unsupported file creation")
	}
	if err := (&File{}).Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unsupported operation wrote: %v / %v", entries, err)
	}
}
