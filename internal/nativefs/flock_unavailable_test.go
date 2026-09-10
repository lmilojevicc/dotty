//go:build (!linux && !darwin) || (darwin && !cgo)

package nativefs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFlockUnavailable(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("DOTTY_REPO", filepath.Join(root, "repo"))
	var d Dir
	name, err := ParseComponent("never-opened")
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range []bool{false, true} {
		var lease *Lease
		if directory {
			lease, err = d.LockDirectory(context.Background())
		} else {
			lease, err = d.LockFile(context.Background(), name)
		}
		if lease != nil {
			_ = lease.Release()
			t.Fatal("unsupported lock returned")
		}
		var authority *AuthorityError
		if !errors.As(err, &authority) || authority.Reason != ReasonUnsupported {
			t.Fatalf("unavailable flock: %v", err)
		}
	}
	var empty *Lease
	if err := empty.Release(); err != nil {
		t.Fatal(err)
	}
	if err := (&Lease{}).Release(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unavailable flock wrote: %v / %v", entries, err)
	}
}
