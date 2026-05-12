package dotty

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestStatusStateFilter(t *testing.T) {
	t.Run("supported values and parsing", func(t *testing.T) {
		wantValues := []string{
			"linked",
			"unlinked",
			"partial",
			"conflict",
			"missing-source",
			"empty",
			"untracked",
		}
		gotValues := SupportedStatusFilterValues()
		if !reflect.DeepEqual(gotValues, wantValues) {
			t.Fatalf("supported values mismatch: want %v, got %v", wantValues, gotValues)
		}

		cases := map[string]State{
			"linked":         StateLinked,
			"unlinked":       StateUnlinked,
			"partial":        StatePartial,
			"conflict":       StateConflict,
			"missing-source": StateMissingSource,
			"empty":          StateEmpty,
			"untracked":      StateUntracked,
		}
		for input, want := range cases {
			got, err := ParseStatusFilterValue(input)
			requireNoError(t, err)
			if got != want {
				t.Fatalf("ParseStatusFilterValue(%q) = %s, want %s", input, got, want)
			}
		}

		_, err := ParseStatusFilterValue("invalid")
		requireErrorContains(
			t,
			err,
			`unsupported status state "invalid" (supported values: linked, unlinked, partial, conflict, missing-source, empty, untracked)`,
		)
	})

	t.Run("filters by aggregate package state and untracked selection", func(t *testing.T) {
		original := &StatusReport{
			RepoPath: "repo",
			Packages: []PackageStatus{
				{Name: "linked", State: StateLinked, Entries: []EntryStatus{{State: StateLinked}}},
				{
					Name:    "partial",
					State:   StatePartial,
					Entries: []EntryStatus{{State: StateLinked}, {State: StateConflict}},
				},
				{Name: "empty", State: StateEmpty, Entries: nil},
			},
			Untracked: []UntrackedItem{{Path: "ghostty", State: StateUntracked}},
		}

		filtered := FilterStatusReport(original, []State{StateLinked, StateUntracked})
		if filtered == original {
			t.Fatal("FilterStatusReport returned the original report pointer")
		}
		if !reflect.DeepEqual(filtered.RepoPath, original.RepoPath) {
			t.Fatalf("repo path changed: want %q, got %q", original.RepoPath, filtered.RepoPath)
		}
		if len(filtered.Packages) != 1 || filtered.Packages[0].Name != "linked" ||
			filtered.Packages[0].State != StateLinked {
			t.Fatalf("unexpected filtered packages: %#v", filtered.Packages)
		}
		if len(filtered.Untracked) != 1 || filtered.Untracked[0].Path != "ghostty" {
			t.Fatalf("unexpected filtered untracked: %#v", filtered.Untracked)
		}
		if len(original.Packages) != 3 || original.Packages[1].Name != "partial" ||
			original.Untracked[0].Path != "ghostty" {
			t.Fatalf("original report was mutated: %#v", original)
		}
	})

	t.Run("empty selection returns copy without mutation", func(t *testing.T) {
		original := &StatusReport{
			RepoPath: "repo",
			Packages: []PackageStatus{
				{
					Name:    "partial",
					State:   StatePartial,
					Entries: []EntryStatus{{State: StateLinked}},
				},
			},
			Untracked: []UntrackedItem{{Path: "ghostty", State: StateUntracked}},
		}

		filtered := FilterStatusReport(original, nil)
		if filtered == original {
			t.Fatal("FilterStatusReport returned the original report pointer")
		}
		filtered.Packages[0].Name = "changed"
		filtered.Packages[0].Entries[0].State = StateConflict
		filtered.Untracked[0].Path = "changed"

		if original.Packages[0].Name != "partial" {
			t.Fatalf("package name mutated in original report: %#v", original.Packages[0])
		}
		if original.Packages[0].Entries[0].State != StateLinked {
			t.Fatalf(
				"package entries mutated in original report: %#v",
				original.Packages[0].Entries,
			)
		}
		if original.Untracked[0].Path != "ghostty" {
			t.Fatalf("untracked items mutated in original report: %#v", original.Untracked)
		}
	})
}

func TestStatusReportsPackageStates(t *testing.T) {
	home, env := setupHome(t)
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
	requireNoError(
		t,
		os.Symlink(filepath.Join(repo, "linked"), filepath.Join(home, ".config", "linked")),
	)
	writeTextFile(t, filepath.Join(home, ".config", "conflict-file"), "target copy")
	requireNoError(
		t,
		os.Symlink(filepath.Join(home, "elsewhere"), filepath.Join(home, ".config", "wrong-link")),
	)
	requireNoError(
		t,
		os.Symlink(
			filepath.Join(repo, "partial", "linked"),
			filepath.Join(home, ".config", "partial-linked"),
		),
	)

	report, err := NewService(repo, env).Status(nil)
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
	home, env := setupHome(t)
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

	report, err := NewService(repo, env).Status(nil)
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
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	requireNoError(t, os.MkdirAll(repo, 0o755))
	writeDottyManifest(t, repo, `version = 1

[packages.zsh]
links = []
`)

	_, err := NewService(repo, env).Status([]string{"tmux"})
	requireErrorContains(t, err, "unknown package")
	requireErrorContains(t, err, "dotty list")
}
