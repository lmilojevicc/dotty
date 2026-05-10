package dotty

import (
	"path/filepath"
	"testing"
)

func TestEnvFromOSDefaultsXDGConfigHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("DOTTY_REPO", "")

	env, err := EnvFromOS()
	requireNoError(t, err)
	if env.Home != home {
		t.Fatalf("home mismatch: want %s, got %s", home, env.Home)
	}
	if want := filepath.Join(home, ".config"); env.XDGConfigHome != want {
		t.Fatalf("xdg config home mismatch: want %s, got %s", want, env.XDGConfigHome)
	}
	if env.DottyRepo != "" {
		t.Fatalf("dotty repo should be empty, got %s", env.DottyRepo)
	}
}

func TestEnvFromOSUsesXDGConfigHomeAndDottyRepo(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	xdg := filepath.Join(t.TempDir(), "xdg")
	repo := filepath.Join(t.TempDir(), "dotfiles")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("DOTTY_REPO", repo)

	env, err := EnvFromOS()
	requireNoError(t, err)
	if env.Home != home || env.XDGConfigHome != xdg || env.DottyRepo != repo {
		t.Fatalf("unexpected env: %#v", env)
	}
}

func TestEnvFromOSErrorsWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")

	_, err := EnvFromOS()
	requireErrorContains(t, err, "determine home directory")
}
