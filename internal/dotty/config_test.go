package dotty

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveConfigWritesRepoTOMLAndCreatesDirectory(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")

	requireNoError(t, RunAtomic(func(tx *Tx) error {
		return SaveConfig(tx, env, &Config{Repo: "~/dotfiles"})
	}))

	requireFileContent(t, env.ConfigFilePath(), "repo = \"~/dotfiles\"\n")
	info, err := os.Stat(env.ConfigFilePath())
	requireNoError(t, err)
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("config mode mismatch: want 0644, got %o", got)
	}
	if _, err := os.Stat(filepath.Dir(env.ConfigFilePath())); err != nil {
		t.Fatalf("config directory was not created: %v", err)
	}
	if _, err := ExpandPath(repo, env); err != nil {
		t.Fatalf("repo path should remain expandable: %v", err)
	}
}
