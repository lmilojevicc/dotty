package dotty

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ExpandPath(path string, env Env) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}

	expanded, err := expandHome(path, env)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(expanded) {
		expanded, err = filepath.Abs(expanded)
		if err != nil {
			return "", err
		}
	}
	return filepath.Clean(expanded), nil
}

func ExpandTargetPath(path string, env Env) (string, error) {
	return ExpandPath(path, env)
}

func HomeRelative(abs string, env Env) string {
	abs = filepath.Clean(abs)
	home := filepath.Clean(env.Home)
	if home == "" {
		return abs
	}
	if abs == home {
		return "~"
	}
	rel, err := filepath.Rel(home, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) ||
		rel == ".." {
		return abs
	}
	return filepath.ToSlash(filepath.Join("~", rel))
}

func ManifestPath(repo string) string {
	return filepath.Join(repo, ManifestFileName)
}

func PackageRoot(repo, packageName string) string {
	return filepath.Join(repo, packageName)
}

func PackageSourcePath(repo, packageName, source string) (string, error) {
	joined, err := packageSourcePathLexical(repo, packageName, source)
	if err != nil {
		return "", err
	}
	root := PackageRoot(repo, packageName)
	if !resolvedSourceStaysWithinPackage(repo, root, joined) {
		return "", fmt.Errorf("source %q escapes package %q", source, packageName)
	}
	return joined, nil
}

func packageSourcePathLexical(repo, packageName, source string) (string, error) {
	if err := validateSourcePath(source); err != nil {
		return "", err
	}
	root := PackageRoot(repo, packageName)
	joined := root
	if source != "." {
		joined = filepath.Clean(filepath.Join(root, filepath.FromSlash(source)))
	}
	if !isWithin(root, joined) {
		return "", fmt.Errorf("source %q escapes package %q", source, packageName)
	}
	return joined, nil
}

func StoreTargetPath(input string, env Env) (string, error) {
	abs, err := ExpandTargetPath(input, env)
	if err != nil {
		return "", err
	}
	return HomeRelative(abs, env), nil
}

func ResolveRepo(override string, env Env) (string, error) {
	if override != "" {
		return ExpandPath(override, env)
	}
	if env.DottyRepo != "" {
		return ExpandPath(env.DottyRepo, env)
	}
	cfg, err := LoadConfig(env)
	if err != nil {
		return "", err
	}
	if cfg.Repo == "" {
		return "", fmt.Errorf(
			"dotty repository is not configured; run `dotty init <path>` or pass --repo",
		)
	}
	return ExpandPath(cfg.Repo, env)
}

func expandHome(path string, env Env) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if env.Home == "" {
			return "", fmt.Errorf("determine home directory: home is empty")
		}
		if path == "~" {
			return env.Home, nil
		}
		return filepath.Join(env.Home, path[2:]), nil
	}
	return path, nil
}

func isWithin(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func resolvedSourceStaysWithinPackage(repo, root, source string) bool {
	if _, err := os.Lstat(root); err != nil {
		return os.IsNotExist(err)
	}
	repoResolved, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return os.IsNotExist(err)
	}
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return os.IsNotExist(err)
	}
	if !isWithin(repoResolved, rootResolved) {
		return false
	}
	existingSource, ok := nearestExistingPath(source)
	if !ok {
		return false
	}
	sourceResolved, err := filepath.EvalSymlinks(existingSource)
	if err != nil {
		return os.IsNotExist(err)
	}
	return isWithin(rootResolved, sourceResolved)
}

func nearestExistingPath(path string) (string, bool) {
	current := filepath.Clean(path)
	for {
		if _, err := os.Lstat(current); err == nil {
			return current, true
		} else if !os.IsNotExist(err) {
			return "", false
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}
