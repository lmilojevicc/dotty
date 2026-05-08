package dotty

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Tx struct {
	rollbacks []func() error
	cleanups  []func() error
}

func RunAtomic(fn func(*Tx) error) error {
	tx := &Tx{}
	if err := fn(tx); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("%w (rollback failed: %v)", err, rollbackErr)
		}
		return err
	}
	return tx.Commit()
}

func (tx *Tx) AddRollback(fn func() error) {
	tx.rollbacks = append(tx.rollbacks, fn)
}

func (tx *Tx) AddCleanup(fn func() error) {
	tx.cleanups = append(tx.cleanups, fn)
}

func (tx *Tx) Rollback() error {
	var errs []error
	for i := len(tx.rollbacks) - 1; i >= 0; i-- {
		if err := tx.rollbacks[i](); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	tx.rollbacks = nil
	for _, cleanup := range tx.cleanups {
		if err := cleanup(); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	tx.cleanups = nil
	return errors.Join(errs...)
}

func (tx *Tx) Commit() error {
	var errs []error
	for _, cleanup := range tx.cleanups {
		if err := cleanup(); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	tx.rollbacks = nil
	tx.cleanups = nil
	return errors.Join(errs...)
}

func EnsureDirTx(tx *Tx, dir string, perm os.FileMode) error {
	dir = filepath.Clean(dir)
	missing, err := missingDirs(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, perm); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	if len(missing) > 0 {
		tx.AddRollback(func() error {
			var errs []error
			for _, path := range missing {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					errs = append(errs, err)
				}
			}
			return errors.Join(errs...)
		})
	}
	return nil
}

func WriteFileTx(tx *Tx, path string, data []byte, perm os.FileMode) error {
	if err := EnsureDirTx(tx, dirOf(path), 0o755); err != nil {
		return err
	}
	var previous []byte
	existed := false
	if current, err := os.ReadFile(path); err == nil {
		existed = true
		previous = current
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read existing file %s: %w", path, err)
	}

	tmp, err := os.CreateTemp(dirOf(path), ".dotty-write-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	cleanupTemp := true
	defer func() {
		_ = tmp.Close()
		if cleanupTemp {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write temporary file for %s: %w", path, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		return fmt.Errorf("chmod temporary file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file for %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	cleanupTemp = false

	tx.AddRollback(func() error {
		if existed {
			return os.WriteFile(path, previous, perm)
		}
		return os.Remove(path)
	})
	return nil
}

func MovePathTx(tx *Tx, src, dst string) error {
	if err := EnsureDirTx(tx, dirOf(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("move %s to %s: %w", src, dst, err)
	}
	tx.AddRollback(func() error {
		return os.Rename(dst, src)
	})
	return nil
}

func CopyPathTx(tx *Tx, src, dst string) error {
	if err := EnsureDirTx(tx, dirOf(dst), 0o755); err != nil {
		return err
	}
	if err := copyPath(src, dst); err != nil {
		_ = os.RemoveAll(dst)
		return err
	}
	tx.AddRollback(func() error {
		return os.RemoveAll(dst)
	})
	return nil
}

func RemoveSymlinkTx(tx *Tx, path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s is not a symlink", path)
	}
	target, err := os.Readlink(path)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	tx.AddRollback(func() error {
		return os.Symlink(target, path)
	})
	return nil
}

func CreateSymlinkTx(tx *Tx, sourceAbs, targetAbs string) error {
	if err := EnsureDirTx(tx, dirOf(targetAbs), 0o755); err != nil {
		return err
	}
	if err := os.Symlink(sourceAbs, targetAbs); err != nil {
		return fmt.Errorf("create symlink %s -> %s: %w", targetAbs, sourceAbs, err)
	}
	tx.AddRollback(func() error {
		return os.Remove(targetAbs)
	})
	return nil
}

func MoveAsideTx(tx *Tx, path string) error {
	parent := dirOf(path)
	backup, err := os.MkdirTemp(parent, ".dotty-backup-")
	if err != nil {
		return fmt.Errorf("create backup path for %s: %w", path, err)
	}
	if err := os.Remove(backup); err != nil {
		return fmt.Errorf("prepare backup path for %s: %w", path, err)
	}
	if err := os.Rename(path, backup); err != nil {
		return fmt.Errorf("move %s aside: %w", path, err)
	}
	tx.AddRollback(func() error {
		return os.Rename(backup, path)
	})
	tx.AddCleanup(func() error {
		return os.RemoveAll(backup)
	})
	return nil
}

func copyPath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", src, err)
	}
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("destination %s already exists", dst)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect destination %s: %w", dst, err)
	}

	mode := info.Mode()
	if mode&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return fmt.Errorf("read symlink %s: %w", src, err)
		}
		return os.Symlink(target, dst)
	}
	if info.IsDir() {
		if err := os.Mkdir(dst, mode.Perm()); err != nil {
			return fmt.Errorf("create directory %s: %w", dst, err)
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return fmt.Errorf("read directory %s: %w", src, err)
		}
		for _, entry := range entries {
			if err := copyPath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
				return err
			}
		}
		return os.Chmod(dst, mode.Perm())
	}
	if !mode.IsRegular() {
		return fmt.Errorf("unsupported file type at %s", src)
	}
	return copyFile(src, dst, mode.Perm())
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy %s to %s: %w", src, dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dst, err)
	}
	return nil
}

func missingDirs(dir string) ([]string, error) {
	var parents []string
	for current := dir; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil {
			if !info.IsDir() {
				return nil, fmt.Errorf("%s exists and is not a directory", current)
			}
			break
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
		parents = append(parents, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	// Rollback needs child-first order; collection already walked child to parent.
	return parents, nil
}

func dirOf(path string) string {
	return filepath.Dir(path)
}
