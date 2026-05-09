package dotty

import (
	"fmt"
	"os"
	"path/filepath"
)

// Env captures the runtime environment that Dotty depends on.
// Production code builds this from the OS; tests construct it directly.
type Env struct {
	Home          string // absolute path to user home directory
	XDGConfigHome string // absolute path to XDG config home (defaults to Home/.config)
	DottyRepo     string // DOTTY_REPO env override (may be empty)
}

// EnvFromOS builds an Env from the current operating system environment.
func EnvFromOS() (Env, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return Env{}, fmt.Errorf("determine home directory: %w", err)
	}
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(home, ".config")
	}
	return Env{
		Home:          home,
		XDGConfigHome: xdg,
		DottyRepo:     os.Getenv("DOTTY_REPO"),
	}, nil
}

// ConfigFilePath returns the path to dotty's user configuration file.
func (e Env) ConfigFilePath() string {
	return filepath.Join(e.XDGConfigHome, "dotty", "config.toml")
}
