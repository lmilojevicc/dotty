package dotty

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

type hardlinkKey struct {
	dev uint64
	ino uint64
}

func fileHardlinkKey(info os.FileInfo) (hardlinkKey, uint64, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink <= 1 {
		return hardlinkKey{}, 0, false
	}
	return hardlinkKey{dev: statDev(stat), ino: stat.Ino}, statNlink(stat), true
}

func hasExternalHardlinks(path string) (bool, error) {
	return hasHardlinksOutsideRoot(path, path)
}

func hasHardlinksOutsideRoot(path, root string) (bool, error) {
	inspectPath, err := hardlinkInspectionPath(path)
	if err != nil {
		return false, err
	}
	inspectRoot := root
	if filepath.Clean(path) == filepath.Clean(root) {
		inspectRoot = inspectPath
	}

	links := map[hardlinkKey]uint64{}
	if err := collectHardlinkKeys(inspectPath, links); err != nil {
		return false, err
	}
	if len(links) == 0 {
		return false, nil
	}

	inspectRoot, err = hardlinkInspectionPath(inspectRoot)
	if err != nil {
		return false, err
	}
	counts := map[hardlinkKey]uint64{}
	if err := filepath.WalkDir(
		inspectRoot,
		func(current string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			key, _, ok := fileHardlinkKey(info)
			if !ok {
				return nil
			}
			if _, tracked := links[key]; tracked {
				counts[key]++
			}
			return nil
		},
	); err != nil {
		return false, err
	}
	for key, nlink := range links {
		if counts[key] < nlink {
			return true, nil
		}
	}
	return false, nil
}

func hasPreservedSymlinkReferentHardlinksOutsideRoot(
	path string,
	copyRoot string,
	hardlinkRoot string,
) (bool, error) {
	path = filepath.Clean(path)
	info, err := os.Lstat(path)
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", path, err)
	}
	mode := info.Mode()
	if mode&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return false, fmt.Errorf("read symlink %s: %w", path, err)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return false, fmt.Errorf("resolve symlink %s: %w", path, err)
		}
		resolved = filepath.Clean(resolved)
		referentInsideCopyRoot := isWithin(filepath.Clean(copyRoot), resolved) ||
			isWithin(resolveExistingPath(copyRoot), resolved)
		if referentInsideCopyRoot && !filepath.IsAbs(target) {
			return false, nil
		}
		return hasHardlinksOutsideRoot(resolved, hardlinkRoot)
	}
	if !info.IsDir() {
		return false, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, fmt.Errorf("read directory %s: %w", path, err)
	}
	for _, entry := range entries {
		externalHardlinks, err := hasPreservedSymlinkReferentHardlinksOutsideRoot(
			filepath.Join(path, entry.Name()),
			copyRoot,
			hardlinkRoot,
		)
		if err != nil || externalHardlinks {
			return externalHardlinks, err
		}
	}
	return false, nil
}

func hardlinkInspectionPath(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve symlink %s: %w", path, err)
	}
	return filepath.Clean(resolved), nil
}

func collectHardlinkKeys(path string, links map[hardlinkKey]uint64) error {
	return collectHardlinkKeysRecursive(path, links, map[string]struct{}{})
}

func collectHardlinkKeysRecursive(
	path string,
	links map[hardlinkKey]uint64,
	seen map[string]struct{},
) error {
	path = filepath.Clean(path)
	if _, ok := seen[path]; ok {
		return nil
	}
	seen[path] = struct{}{}

	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	mode := info.Mode()
	if mode&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("resolve symlink %s: %w", path, err)
		}
		return collectHardlinkKeysRecursive(resolved, links, seen)
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := collectHardlinkKeysRecursive(
				filepath.Join(path, entry.Name()),
				links,
				seen,
			); err != nil {
				return err
			}
		}
		return nil
	}
	if !mode.IsRegular() {
		return nil
	}
	key, nlink, ok := fileHardlinkKey(info)
	if ok {
		links[key] = nlink
	}
	return nil
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		info, err = os.Stat(path)
		if err != nil {
			return nil, err
		}
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return os.ReadFile(path)
}

func copyPath(src, dst string) error {
	return copyPathWithHardlinks(src, dst, map[hardlinkKey]string{})
}

func copyPathWithHardlinks(src, dst string, copied map[hardlinkKey]string) error {
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
			if err := copyPathWithHardlinks(
				filepath.Join(src, entry.Name()),
				filepath.Join(dst, entry.Name()),
				copied,
			); err != nil {
				return err
			}
		}
		return os.Chmod(dst, mode.Perm())
	}
	if !mode.IsRegular() {
		return fmt.Errorf("unsupported file type at %s", src)
	}
	if key, _, ok := fileHardlinkKey(info); ok {
		if previousDst, seen := copied[key]; seen {
			return os.Link(previousDst, dst)
		}
		copied[key] = dst
	}
	return copyFile(src, dst, mode.Perm())
}

func validateSupportedSourcePath(path string) error {
	return validateSupportedSourcePathRecursive(path, map[string]struct{}{})
}

func validateSupportedSourcePathRecursive(path string, seen map[string]struct{}) error {
	path = filepath.Clean(path)
	if _, ok := seen[path]; ok {
		return nil
	}
	seen[path] = struct{}{}

	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}

	mode := info.Mode()
	if mode&os.ModeSymlink != 0 {
		if _, err := os.Readlink(path); err != nil {
			return fmt.Errorf("read symlink %s: %w", path, err)
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("resolve symlink %s: %w", path, err)
		}
		return validateSupportedSourcePathRecursive(resolved, seen)
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return fmt.Errorf("read directory %s: %w", path, err)
		}
		for _, entry := range entries {
			if err := validateSupportedSourcePathRecursive(
				filepath.Join(path, entry.Name()),
				seen,
			); err != nil {
				return err
			}
		}
		return nil
	}
	if !mode.IsRegular() {
		return fmt.Errorf("unsupported file type at %s", path)
	}
	return nil
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
