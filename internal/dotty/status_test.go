package dotty

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatusReportsPackageStates(t *testing.T) {
	home := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	requireNoError(t, os.MkdirAll(repo, 0o755))
	writeDottyManifest(t, repo, `version = 1

[packages.empty]
links = []

[packages.linked]
links = [
  { source = ".", target = "~/.config/linked" },
]

[packages.unlinked]
links = [
  { source = ".", target = "~/.config/unlinked" },
]

[packages.conflict_file]
links = [
  { source = ".", target = "~/.config/conflict-file" },
]

[packages.wrong_link]
links = [
  { source = ".", target = "~/.config/wrong-link" },
]

[packages.missing]
links = [
  { source = ".", target = "~/.config/missing" },
]

[packages.partial]
links = [
  { source = "linked", target = "~/.config/partial-linked" },
  { source = "unlinked", target = "~/.config/partial-unlinked" },
]
`)
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "linked"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "unlinked"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "conflict_file"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "wrong_link"), 0o755))
	writeTextFile(t, filepath.Join(repo, "partial", "linked"), "linked")
	writeTextFile(t, filepath.Join(repo, "partial", "unlinked"), "unlinked")

	requireNoError(t, os.MkdirAll(filepath.Join(home, ".config"), 0o755))
	requireNoError(t, os.Symlink(filepath.Join(repo, "linked"), filepath.Join(home, ".config", "linked")))
	writeTextFile(t, filepath.Join(home, ".config", "conflict-file"), "target copy")
	requireNoError(t, os.Symlink(filepath.Join(home, "elsewhere"), filepath.Join(home, ".config", "wrong-link")))
	requireNoError(t, os.Symlink(filepath.Join(repo, "partial", "linked"), filepath.Join(home, ".config", "partial-linked")))

	report, err := NewService(repo).Status(nil)
	requireNoError(t, err)

	wantStates := map[string]State{
		"conflict_file": StateConflict,
		"empty":         StateEmpty,
		"linked":        StateLinked,
		"missing":       StateMissingSource,
		"partial":       StatePartial,
		"unlinked":      StateUnlinked,
		"wrong_link":    StateConflict,
	}
	if len(report.Packages) != len(wantStates) {
		t.Fatalf("package count mismatch: want %d, got %d", len(wantStates), len(report.Packages))
	}
	for _, pkg := range report.Packages {
		want, ok := wantStates[pkg.Name]
		if !ok {
			t.Fatalf("unexpected package %q", pkg.Name)
		}
		if pkg.State != want {
			t.Fatalf("state mismatch for %s: want %s, got %s", pkg.Name, want, pkg.State)
		}
	}
}

func TestStatusReportsUntrackedRepositoryContent(t *testing.T) {
	home := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	requireNoError(t, os.MkdirAll(repo, 0o755))
	writeDottyManifest(t, repo, `version = 1

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)
	writeTextFile(t, filepath.Join(repo, "tmux", "tmux.conf"), "set -g mouse on\n")
	writeTextFile(t, filepath.Join(repo, "zsh", ".zshrc"), "source ~/.zprofile\n")
	writeTextFile(t, filepath.Join(repo, "zsh", ".zprofile"), "export PATH\n")
	writeTextFile(t, filepath.Join(repo, "zsh", "secrets", "token"), "secret\n")
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "ghostty"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))

	report, err := NewService(repo).Status(nil)
	requireNoError(t, err)

	got := make([]string, 0, len(report.Untracked))
	for _, item := range report.Untracked {
		if item.State != StateUntracked {
			t.Fatalf("untracked item %s has state %s", item.Path, item.State)
		}
		got = append(got, item.Path)
	}
	requireEqualStrings(t, got, []string{"ghostty", "zsh/.zprofile", "zsh/secrets"})
}

func TestStatusFilterRejectsUnknownPackage(t *testing.T) {
	home := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	requireNoError(t, os.MkdirAll(repo, 0o755))
	writeDottyManifest(t, repo, `version = 1

[packages.zsh]
links = []
`)

	_, err := NewService(repo).Status([]string{"tmux"})
	requireErrorContains(t, err, "unknown package")
}
