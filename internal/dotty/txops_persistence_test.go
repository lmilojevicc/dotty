package dotty

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestWriteFileTxPersistence(t *testing.T) {
	t.Run("modes and rollback under umask", func(t *testing.T) {
		oldMask := syscall.Umask(0o077)
		defer syscall.Umask(oldMask)
		for _, mode := range []os.FileMode{0o600, 0o640, 0o750, 0o750 | os.ModeSetuid, 0o750 | os.ModeSetgid, 0o750 | os.ModeSticky} {
			t.Run(fmt.Sprintf("mode-%o", mode), func(t *testing.T) {
				root := t.TempDir()
				// Darwin inherits the parent group, which can be a nonmember group
				// under /tmp. Use our effective group so the fixture and replacement
				// files can retain setgid without privileges.
				requireNoError(t, os.Chown(root, -1, os.Getegid()))
				existing := filepath.Join(root, "existing")
				created := filepath.Join(root, "new-parent", "new")
				writeTextFile(t, existing, "original\n")
				requireNoError(t, os.Chmod(existing, mode))
				requirePersistenceMode(t, existing, mode)
				stop := errors.New("stop after replacement")
				err := RunAtomic(func(tx *Tx) error {
					requireNoError(t, WriteFileTx(tx, existing, []byte("updated\n"), 0o644))
					requireNoError(t, WriteFileTx(tx, created, []byte("created\n"), mode))
					requirePersistenceMode(t, existing, mode)
					requirePersistenceMode(t, created, mode)
					requireFileContent(t, existing, "updated\n")
					return stop
				})
				if !errors.Is(err, stop) {
					t.Fatalf("lost failure cause: %v", err)
				}
				requireFileContent(t, existing, "original\n")
				requirePersistenceMode(t, existing, mode)
				requireNoPath(t, filepath.Dir(created))
				requireNoError(t, RunAtomic(func(tx *Tx) error {
					if err := WriteFileTx(tx, existing, []byte("committed\n"), 0o644); err != nil {
						return err
					}
					return WriteFileTx(tx, created, []byte("committed new\n"), mode)
				}))
				requireFileContent(t, existing, "committed\n")
				requirePersistenceMode(t, existing, mode)
				requirePersistenceMode(t, created, mode)
				requireNoPersistenceTemps(t, root)
			})
		}
	})
	t.Run("type conflicts", func(t *testing.T) {
		for _, kind := range []string{"directory", "symlink", "fifo"} {
			t.Run(kind, func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, "conflict")
				referent := filepath.Join(root, "referent")
				writeTextFile(t, referent, "untouched\n")
				switch kind {
				case "directory":
					requireNoError(t, os.Mkdir(path, 0o750))
				case "symlink":
					requireNoError(t, os.Symlink(referent, path))
				case "fifo":
					requireNoError(t, syscall.Mkfifo(path, 0o600))
				}
				before, err := os.Lstat(path)
				requireNoError(t, err)
				err = RunAtomic(
					func(tx *Tx) error { return WriteFileTx(tx, path, []byte("replacement"), 0o644) },
				)
				if err == nil {
					t.Fatal("type conflict was accepted")
				}
				requireErrorContains(t, err, path)
				after, err := os.Lstat(path)
				requireNoError(t, err)
				if !os.SameFile(before, after) || before.Mode() != after.Mode() {
					t.Fatal("conflict path changed")
				}
				requireFileContent(t, referent, "untouched\n")
				requireNoPersistenceTemps(t, root)
			})
		}
	})
	t.Run("injected failures", func(t *testing.T) {
		for _, stage := range []string{"write", "short write", "rename"} {
			for _, existed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s-existing-%t", stage, existed), func(t *testing.T) {
					root := t.TempDir()
					path := filepath.Join(root, "parent", "file")
					if existed {
						writeTextFile(t, path, "original\n")
						requireNoError(t, os.Chmod(path, 0o640))
					}
					injected := errors.New("injected persistence " + stage)
					oldWrite, oldRename := persistenceWrite, persistenceRename
					t.Cleanup(func() { persistenceWrite, persistenceRename = oldWrite, oldRename })
					if stage == "rename" {
						persistenceRename = func(_, _ string) error { return injected }
					} else {
						persistenceWrite = func(file *os.File, data []byte) (int, error) {
							n, err := file.Write(data[:1])
							if err != nil {
								return n, err
							}
							if stage == "short write" {
								return n, nil
							}
							return n, injected
						}
					}
					err := RunAtomic(
						func(tx *Tx) error { return WriteFileTx(tx, path, []byte("replacement"), 0o644) },
					)
					if stage == "short write" {
						requireErrorContains(t, err, "short write")
					} else if !errors.Is(err, injected) {
						t.Fatalf("lost failure cause: %v", err)
					}
					if existed {
						requireFileContent(t, path, "original\n")
						requirePersistenceMode(t, path, 0o640)
					} else {
						requireNoPath(t, filepath.Dir(path))
					}
					requireNoPersistenceTemps(t, root)
				})
			}
		}
	})
	t.Run("rollback failure cause and temp cleanup", func(t *testing.T) {
		for _, stage := range []string{"write", "rename"} {
			t.Run(stage, func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, "file")
				writeTextFile(t, path, "original\n")
				requireNoError(t, os.Chmod(path, 0o640))
				oldWrite, oldRename := persistenceWrite, persistenceRename
				t.Cleanup(func() { persistenceWrite, persistenceRename = oldWrite, oldRename })
				injected := errors.New("injected rollback " + stage)
				stop := errors.New("stop transaction")
				err := RunAtomic(func(tx *Tx) error {
					if err := WriteFileTx(tx, path, []byte("updated\n"), 0o644); err != nil {
						return err
					}
					if stage == "write" {
						persistenceWrite = func(_ *os.File, _ []byte) (int, error) { return 0, injected }
					} else {
						persistenceRename = func(_, _ string) error { return injected }
					}
					return stop
				})
				if !errors.Is(err, stop) || !errors.Is(err, injected) {
					t.Fatalf("lost failure causes: %v", err)
				}
				requireErrorContains(t, err, "rollback failed")
				requireErrorContains(t, err, path)
				requireFileContent(t, path, "updated\n")
				requirePersistenceMode(t, path, 0o640)
				requireNoPersistenceTemps(t, root)
			})
		}
	})
}

func requireNoPersistenceTemps(t *testing.T, root string) {
	t.Helper()
	requireNoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(entry.Name(), ".dotty-write-") ||
			strings.HasPrefix(entry.Name(), ".dotty-rollback-") {
			t.Errorf("temporary artifact retained: %s", path)
		}
		return nil
	}))
}
