package dotty

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

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
			if err := copyPath(
				filepath.Join(src, entry.Name()),
				filepath.Join(dst, entry.Name()),
			); err != nil {
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

func validateCopyablePath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}

	mode := info.Mode()
	if mode&os.ModeSymlink != 0 {
		if _, err := os.Readlink(path); err != nil {
			return fmt.Errorf("read symlink %s: %w", path, err)
		}
		return nil
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return fmt.Errorf("read directory %s: %w", path, err)
		}
		for _, entry := range entries {
			if err := validateCopyablePath(filepath.Join(path, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if !mode.IsRegular() {
		return fmt.Errorf("unsupported file type at %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
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
