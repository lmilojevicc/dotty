package dotty

import (
	"path/filepath"
	"testing"
)

func TestPathExpansionAndStorageUseHomeRelativePaths(t *testing.T) {
	home, env := setupHome(t)

	expanded, err := ExpandPath("~/.config/../.zshrc", env)
	requireNoError(t, err)
	if want := filepath.Join(home, ".zshrc"); expanded != want {
		t.Fatalf("expanded path mismatch: want %s, got %s", want, expanded)
	}

	stored, err := StoreTargetPath(filepath.Join(home, ".config", "tmux"), env)
	requireNoError(t, err)
	if stored != "~/.config/tmux" {
		t.Fatalf("stored target mismatch: want ~/.config/tmux, got %s", stored)
	}

	outside := filepath.Join(t.TempDir(), "outside")
	if got := HomeRelative(outside, env); got != outside {
		t.Fatalf("outside home path should stay absolute: want %s, got %s", outside, got)
	}
}

func TestPackageSourcePathStaysWithinPackageRoot(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "dotfiles")

	root, err := PackageSourcePath(repo, "tmux", ".")
	requireNoError(t, err)
	if want := filepath.Join(repo, "tmux"); root != want {
		t.Fatalf("package root mismatch: want %s, got %s", want, root)
	}

	source, err := PackageSourcePath(repo, "tmux", "config/../tmux.conf")
	requireNoError(t, err)
	if want := filepath.Join(repo, "tmux", "tmux.conf"); source != want {
		t.Fatalf("package source mismatch: want %s, got %s", want, source)
	}

	for _, invalid := range []string{"../secret", filepath.Join(repo, "other"), "~/.zshrc"} {
		t.Run(invalid, func(t *testing.T) {
			if _, err := PackageSourcePath(repo, "tmux", invalid); err == nil {
				t.Fatalf("expected source %q to be rejected", invalid)
			}
		})
	}
}

func TestResolveRepoPrecedence(t *testing.T) {
	home, env := setupHome(t)
	explicit := filepath.Join(home, "explicit")
	envRepo := filepath.Join(home, "env")
	configured := filepath.Join(home, "configured")

	envWithRepo := env
	envWithRepo.DottyRepo = envRepo
	got, err := ResolveRepo(explicit, envWithRepo)
	requireNoError(t, err)
	if got != explicit {
		t.Fatalf("explicit repo should win: want %s, got %s", explicit, got)
	}

	got, err = ResolveRepo("", envWithRepo)
	requireNoError(t, err)
	if got != envRepo {
		t.Fatalf("DOTTY_REPO should win over config: want %s, got %s", envRepo, got)
	}

	requireNoError(t, RunAtomic(func(tx *Tx) error {
		return SaveConfig(tx, env, &Config{Repo: "~/configured"})
	}))
	got, err = ResolveRepo("", env)
	requireNoError(t, err)
	if got != configured {
		t.Fatalf("config repo mismatch: want %s, got %s", configured, got)
	}
}

func TestResolveRepoErrorsWhenNoRepositoryIsConfigured(t *testing.T) {
	_, env := setupHome(t)

	_, err := ResolveRepo("", env)
	requireErrorContains(t, err, "repository is not configured")
}
