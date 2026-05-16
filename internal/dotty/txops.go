package dotty

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

var (
	renamePath    = os.Rename
	removePath    = os.Remove
	removeAllPath = os.RemoveAll
	symlinkPath   = os.Symlink
	copyPathOp    = copyPath
)

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
				if err := removePath(path); err != nil && !os.IsNotExist(err) {
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
	previousPerm := perm
	existed := false
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf(
				"refuse to replace symlink %s (replace it with a regular file or edit its target directly)",
				path,
			)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("existing file %s is not a regular file", path)
		}
		previousPerm = info.Mode().Perm()
		current, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read existing file %s: %w", path, err)
		}
		existed = true
		previous = current
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect existing file %s: %w", path, err)
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
			return restoreRegularFileNoFollow(path, previous, previousPerm)
		}
		return removePath(path)
	})
	return nil
}

func restoreRegularFileNoFollow(path string, data []byte, perm os.FileMode) error {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if err := removePath(path); err != nil {
				return err
			}
		} else if !info.Mode().IsRegular() {
			return fmt.Errorf("refuse to restore %s over non-regular path", path)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect rollback path %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(dirOf(path), ".dotty-rollback-*")
	if err != nil {
		return fmt.Errorf("create rollback temporary file for %s: %w", path, err)
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
		return fmt.Errorf("write rollback temporary file for %s: %w", path, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		return fmt.Errorf("chmod rollback temporary file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close rollback temporary file for %s: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("restore %s: %w", path, err)
	}
	cleanupTemp = false
	return nil
}

func MovePathTx(tx *Tx, src, dst string) error {
	if err := EnsureDirTx(tx, dirOf(dst), 0o755); err != nil {
		return err
	}
	if err := movePathWithFallback(src, dst); err != nil {
		return fmt.Errorf("move %s to %s: %w", src, dst, err)
	}
	tx.AddRollback(func() error {
		return restoreMovedPathWithFallback(dst, src)
	})
	return nil
}

func CopyPathAndMoveAsideSourceTx(tx *Tx, src, dst string) error {
	if err := CopyPathTx(tx, src, dst); err != nil {
		return err
	}
	if err := MoveAsideTx(tx, src); err != nil {
		return err
	}
	return nil
}

func CopyPathTx(tx *Tx, src, dst string) error {
	if err := EnsureDirTx(tx, dirOf(dst), 0o755); err != nil {
		return err
	}
	dstExisted, err := pathExists(dst)
	if err != nil {
		return err
	}
	if err := copyPathOp(src, dst); err != nil {
		if !dstExisted {
			_ = os.RemoveAll(dst)
		}
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
	if err := removePath(path); err != nil {
		return err
	}
	tx.AddRollback(func() error {
		return symlinkPath(target, path)
	})
	return nil
}

func CreateSymlinkTx(tx *Tx, sourceAbs, targetAbs string) error {
	if err := EnsureDirTx(tx, dirOf(targetAbs), 0o755); err != nil {
		return err
	}
	if err := symlinkPath(sourceAbs, targetAbs); err != nil {
		return fmt.Errorf("create symlink %s -> %s: %w", targetAbs, sourceAbs, err)
	}
	tx.AddRollback(func() error {
		return removePath(targetAbs)
	})
	return nil
}

func MoveAsideTx(tx *Tx, path string) error {
	parent := dirOf(path)
	backup, err := os.MkdirTemp(parent, ".dotty-backup-")
	if err != nil {
		return fmt.Errorf("create backup path for %s: %w", path, err)
	}
	if err := removePath(backup); err != nil {
		return fmt.Errorf("prepare backup path for %s: %w", path, err)
	}
	if err := movePathWithFallback(path, backup); err != nil {
		return fmt.Errorf("move %s aside: %w", path, err)
	}
	tx.AddRollback(func() error {
		return restoreMovedPathWithFallback(backup, path)
	})
	tx.AddCleanup(func() error {
		return removeAllPath(backup)
	})
	return nil
}

func movePathWithFallback(src, dst string) error {
	return renameOrCopyRemove(src, dst)
}

func restoreMovedPathWithFallback(src, dst string) error {
	return renameOrCopyRemove(src, dst)
}

func renameOrCopyRemove(src, dst string) error {
	if err := renamePath(src, dst); err != nil {
		if !errors.Is(err, syscall.EXDEV) {
			return err
		}
	} else {
		return nil
	}

	dstExisted, err := pathExists(dst)
	if err != nil {
		return err
	}
	if err := copyPathOp(src, dst); err != nil {
		if !dstExisted {
			_ = os.RemoveAll(dst)
		}
		return err
	}
	if err := os.RemoveAll(src); err != nil {
		return fmt.Errorf("remove source %s after cross-device move: %w", src, err)
	}
	return nil
}
