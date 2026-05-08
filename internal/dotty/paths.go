package dotty

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ExpandPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}

	expanded, err := expandHome(path)
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

func ExpandTargetPath(path string) (string, error) {
	abs, err := ExpandPath(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(abs) {
		return "", fmt.Errorf("target path %q must be absolute or home-relative", path)
	}
	return abs, nil
}

func HomeRelative(abs string) string {
	abs = filepath.Clean(abs)
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return abs
	}
	home = filepath.Clean(home)
	if abs == home {
		return "~"
	}
	rel, err := filepath.Rel(home, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return abs
	}
	return filepath.ToSlash(filepath.Join("~", rel))
}

func ConfigFilePath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", fmt.Errorf("determine home directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "dotty", "config.toml"), nil
}

func ManifestPath(repo string) string {
	return filepath.Join(repo, ManifestFileName)
}

func PackageRoot(repo, packageName string) string {
	return filepath.Join(repo, packageName)
}

func PackageSourcePath(repo, packageName, source string) (string, error) {
	if err := validateSourcePath(source); err != nil {
		return "", err
	}
	root := PackageRoot(repo, packageName)
	if source == "." {
		return root, nil
	}
	joined := filepath.Clean(filepath.Join(root, filepath.FromSlash(source)))
	if !isWithin(root, joined) {
		return "", fmt.Errorf("source %q escapes package %q", source, packageName)
	}
	return joined, nil
}

func StoreTargetPath(input string) (string, error) {
	abs, err := ExpandTargetPath(input)
	if err != nil {
		return "", err
	}
	return HomeRelative(abs), nil
}

func ResolveRepo(override string) (string, error) {
	if override != "" {
		return ExpandPath(override)
	}
	if env := os.Getenv("DOTTY_REPO"); env != "" {
		return ExpandPath(env)
	}
	cfg, err := LoadConfig()
	if err != nil {
		return "", err
	}
	if cfg.Repo == "" {
		return "", fmt.Errorf("dotty repository is not configured; run `dotty init PATH` or pass --repo")
	}
	return ExpandPath(cfg.Repo)
}

func expandHome(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", fmt.Errorf("determine home directory: %w", err)
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, path[2:]), nil
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
