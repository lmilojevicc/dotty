package dotty

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestInitCreatesRepositoryManifestAndDefaultRepositoryConfig(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")

	svc, err := InitRepo(repo, env)
	requireNoError(t, err)
	if svc.Repo != repo {
		t.Fatalf("repo path mismatch: want %s, got %s", repo, svc.Repo)
	}
	if _, err := os.Stat(ManifestPath(repo)); err != nil {
		t.Fatalf("manifest was not created: %v", err)
	}
	cfg, err := LoadConfig(env)
	requireNoError(t, err)
	if cfg.Repo != "~/dotfiles" {
		t.Fatalf("default repository mismatch: want ~/dotfiles, got %s", cfg.Repo)
	}
}

func TestInitValidatesExistingManifestWithoutOverwritingIt(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	requireNoError(t, os.MkdirAll(repo, 0o755))
	manifest := "version = 1\n\n[packages.zsh]\nlinks = []\n"
	writeDottyManifest(t, repo, manifest)

	_, err := InitRepo(repo, env)
	requireNoError(t, err)
	requireFileContent(t, ManifestPath(repo), manifest)
}

func TestInitRejectsInvalidExistingManifest(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	requireNoError(t, os.MkdirAll(repo, 0o755))
	writeDottyManifest(t, repo, "version = 2\n")

	_, err := InitRepo(repo, env)
	requireErrorContains(t, err, "unsupported manifest version")

	configPath := env.ConfigFilePath()
	requireNoPath(t, configPath)
}

func TestInitRollsBackCreatedRepositoryWhenConfigSaveFails(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeTextFile(t, filepath.Join(home, ".config", "dotty"), "not a directory\n")

	_, err := InitRepo(repo, env)
	requireErrorContains(t, err, "not a directory")
	requireNoPath(t, repo)
}

func TestInitRefusesSymlinkedConfigWithoutReplacingIt(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	externalConfig := filepath.Join(home, "external-config.toml")
	writeTextFile(t, externalConfig, "repo = \"~/old-dotfiles\"\n")
	configPath := env.ConfigFilePath()
	requireNoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	requireNoError(t, os.Symlink(externalConfig, configPath))

	_, err := InitRepo(repo, env)
	requireErrorContains(t, err, "refuse to replace symlink")

	assertSymlink(t, configPath, externalConfig)
	requireFileContent(t, externalConfig, "repo = \"~/old-dotfiles\"\n")
	requireNoPath(t, repo)
}

func TestLoadConfigRejectsFIFOWithoutBlocking(t *testing.T) {
	_, env := setupHome(t)
	configPath := env.ConfigFilePath()
	requireNoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	requireNoError(t, syscall.Mkfifo(configPath, 0o600))

	done := make(chan error, 1)
	go func() {
		_, err := LoadConfig(env)
		done <- err
	}()

	select {
	case err := <-done:
		requireErrorContains(t, err, "not a regular file")
	case <-time.After(250 * time.Millisecond):
		t.Fatal("LoadConfig blocked on FIFO instead of rejecting it")
	}

	info, err := os.Lstat(configPath)
	requireNoError(t, err)
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("expected config FIFO to remain unchanged, mode=%v", info.Mode())
	}
}

func TestLoadConfigReturnsEmptyConfigWhenMissing(t *testing.T) {
	_, env := setupHome(t)

	cfg, err := LoadConfig(env)
	requireNoError(t, err)
	if cfg.Repo != "" {
		t.Fatalf("missing config should return empty repo, got %q", cfg.Repo)
	}
}

func TestListReportsSortedInventoryWithoutInspectingFilesystemState(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	requireNoError(t, os.MkdirAll(repo, 0o755))
	writeDottyManifest(t, repo, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
  { source = ".zprofile", target = "~/.zprofile" },
]

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]

[collections.terminal]
packages = ["tmux", "zsh"]

[collections.shell]
packages = ["zsh"]
`)

	inv, err := NewService(repo, env).List()
	requireNoError(t, err)
	if len(inv.Packages) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(inv.Packages))
	}
	if inv.Packages[0].Name != "tmux" || inv.Packages[0].LinkCount != 1 {
		t.Fatalf("unexpected first package: %#v", inv.Packages[0])
	}
	if inv.Packages[1].Name != "zsh" || inv.Packages[1].LinkCount != 2 {
		t.Fatalf("unexpected second package: %#v", inv.Packages[1])
	}
	if len(inv.Collections) != 2 {
		t.Fatalf("expected 2 collections, got %d", len(inv.Collections))
	}
	if inv.Collections[0].Name != "shell" {
		t.Fatalf("collections should be sorted by name: %#v", inv.Collections)
	}
	requireEqualStrings(t, inv.Collections[1].Packages, []string{"tmux", "zsh"})
}

func TestListReportsEmptyManifestInventory(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	requireNoError(t, os.MkdirAll(repo, 0o755))
	writeDottyManifest(t, repo, "version = 1\n")

	inv, err := NewService(repo, env).List()
	requireNoError(t, err)
	if len(inv.Packages) != 0 {
		t.Fatalf("expected no packages, got %#v", inv.Packages)
	}
	if len(inv.Collections) != 0 {
		t.Fatalf("expected no collections, got %#v", inv.Collections)
	}
}
