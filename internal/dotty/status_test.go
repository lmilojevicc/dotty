package dotty

import (
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
)

func TestStatusStateFilter(t *testing.T) {
	t.Run("supported values and parsing", func(t *testing.T) {
		wantValues := []string{
			"linked",
			"unlinked",
			"partial",
			"conflict",
			"blocked",
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
			"blocked":        StateBlocked,
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
			`unsupported status state "invalid" (supported values: linked, unlinked, partial, conflict, blocked, missing-source, empty, untracked)`,
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

func TestStatusReportsBlockedTargetWithOwner(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, `version = 1

[packages.tmux-macos]
links = [
  { source = ".", target = "~/.config/tmux" },
]

[packages.tmux-linux]
links = [
  { source = ".", target = "~/.config/tmux" },
]
`)
	macosSource := filepath.Join(repo, "tmux-macos")
	linuxSource := filepath.Join(repo, "tmux-linux")
	requireNoError(t, os.MkdirAll(macosSource, 0o755))
	requireNoError(t, os.MkdirAll(linuxSource, 0o755))
	target := filepath.Join(home, ".config", "tmux")
	requireNoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	requireNoError(t, os.Symlink(linuxSource, target))

	report, err := NewService(repo, env).Status([]string{"tmux-macos", "tmux-linux"})
	requireNoError(t, err)
	if report.Packages[0].State != StateBlocked || len(report.Packages[0].Entries) != 1 ||
		report.Packages[0].Entries[0].BlockedBy != "tmux-linux" {
		t.Fatalf("expected tmux-macos blocked by tmux-linux, got %#v", report.Packages[0])
	}
	if report.Packages[1].State != StateLinked {
		t.Fatalf("expected tmux-linux linked, got %#v", report.Packages[1])
	}
}

func TestStatusReportsCompetingAbsentTargetsAsUnlinked(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, `version = 1

[packages.tmux-macos]
links = [
  { source = ".", target = "~/.config/tmux" },
]

[packages.tmux-linux]
links = [
  { source = ".", target = "~/.config/tmux" },
]
`)
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "tmux-macos"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "tmux-linux"), 0o755))

	report, err := NewService(repo, env).Status([]string{"tmux-macos", "tmux-linux"})
	requireNoError(t, err)
	if report.Packages[0].State != StateUnlinked || report.Packages[1].State != StateUnlinked {
		t.Fatalf("expected both competing absent targets to be UNLINKED, got %#v", report.Packages)
	}
}

func TestStatusSupportsPackageSourceSelectors(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
  { source = "sesh-fzf", target = "~/.local/bin/sesh-fzf" },
]
`)
	writeTextFile(t, filepath.Join(repo, "scripts", "docx2pdf"), "docx2pdf\n")
	writeTextFile(t, filepath.Join(repo, "scripts", "sesh-fzf"), "sesh\n")

	report, err := NewService(repo, env).Status([]string{"scripts/docx2pdf"})
	requireNoError(t, err)
	if len(report.Packages) != 1 || report.Packages[0].Name != "scripts/docx2pdf" {
		t.Fatalf("unexpected package source status: %#v", report.Packages)
	}
	if len(report.Packages[0].Entries) != 1 || report.Packages[0].Entries[0].Source != "docx2pdf" {
		t.Fatalf("expected only docx2pdf entry, got %#v", report.Packages[0].Entries)
	}
}

func TestStatusPackageSourceSelectorIncludesUntrackedContentUnderDirectory(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "office/docx2pdf", target = "~/.local/bin/docx2pdf" },
]
`)
	writeTextFile(t, filepath.Join(repo, "scripts", "office", "docx2pdf"), "docx2pdf\n")
	writeTextFile(t, filepath.Join(repo, "scripts", "office", "unused"), "unused\n")
	writeTextFile(t, filepath.Join(repo, "scripts", "other"), "other\n")

	report, err := NewService(repo, env).Status([]string{"scripts/office"})
	requireNoError(t, err)
	if len(report.Packages) != 1 || report.Packages[0].Name != "scripts/office" {
		t.Fatalf("unexpected office status: %#v", report.Packages)
	}
	if len(report.Packages[0].Entries) != 1 ||
		report.Packages[0].Entries[0].Source != "office/docx2pdf" {
		t.Fatalf("expected office/docx2pdf entry, got %#v", report.Packages[0].Entries)
	}
	if len(report.Untracked) != 1 || report.Untracked[0].Path != "scripts/office/unused" {
		t.Fatalf("expected untracked content under scripts/office, got %#v", report.Untracked)
	}
}

func TestStatusSupportsMixedPackageAndPackageSourceSelectors(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]
`)
	writeTextFile(t, filepath.Join(repo, "scripts", "docx2pdf"), "docx2pdf\n")
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "tmux"), 0o755))

	report, err := NewService(repo, env).Status([]string{"scripts/docx2pdf", "tmux"})
	requireNoError(t, err)
	if len(report.Packages) != 2 {
		t.Fatalf("expected two statuses, got %#v", report.Packages)
	}
	if report.Packages[0].Name != "scripts/docx2pdf" || report.Packages[1].Name != "tmux" {
		t.Fatalf("unexpected mixed selector order: %#v", report.Packages)
	}
}

func TestStatusReportsEscapingPackageSourceSymlinkAsConflict(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, manifestWithSingleZshrcLink)
	external := filepath.Join(home, "external", ".zshrc")
	writeTextFile(t, external, "external config\n")
	source := filepath.Join(repo, "zsh", ".zshrc")
	requireNoError(t, os.MkdirAll(filepath.Dir(source), 0o755))
	requireNoError(t, os.Symlink(external, source))

	report, err := NewService(repo, env).Status([]string{"zsh"})
	requireNoError(t, err)
	if len(report.Packages) != 1 {
		t.Fatalf("expected one package status, got %d", len(report.Packages))
	}
	if got := report.Packages[0].State; got != StateConflict {
		t.Fatalf("escaping Package Source symlink should report CONFLICT, got %s", got)
	}
	if len(report.Packages[0].Entries) != 1 ||
		report.Packages[0].Entries[0].State != StateConflict {
		t.Fatalf(
			"escaping Package Source symlink entry should report CONFLICT, got %#v",
			report.Packages[0].Entries,
		)
	}
}

func TestStatusReportsDanglingPackageSourceSymlinkAsMissingSource(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, `version = 1

[packages.ghost]
links = [
  { source = "config", target = "~/.config/ghost" },
]
`)
	packageRoot := filepath.Join(repo, "ghost")
	requireNoError(t, os.MkdirAll(packageRoot, 0o755))
	requireNoError(t, os.Symlink("missing-config", filepath.Join(packageRoot, "config")))

	report, err := NewService(repo, env).Status([]string{"ghost"})
	requireNoError(t, err)
	if len(report.Packages) != 1 {
		t.Fatalf("expected one package status, got %d", len(report.Packages))
	}
	if got := report.Packages[0].State; got != StateMissingSource {
		t.Fatalf("dangling Package Source symlink should report MISSING SOURCE, got %s", got)
	}
	if len(report.Packages[0].Entries) != 1 ||
		report.Packages[0].Entries[0].State != StateMissingSource {
		t.Fatalf(
			"dangling Package Source symlink entry should report MISSING SOURCE, got %#v",
			report.Packages[0].Entries,
		)
	}
}

func TestStatusReportsPackageSourceSymlinkLoopAsConflict(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, `version = 1

[packages.ghost]
links = [
  { source = "config", target = "~/.config/ghost" },
]
`)
	packageRoot := filepath.Join(repo, "ghost")
	requireNoError(t, os.MkdirAll(packageRoot, 0o755))
	requireNoError(t, os.Symlink("config-other", filepath.Join(packageRoot, "config")))
	requireNoError(t, os.Symlink("config", filepath.Join(packageRoot, "config-other")))

	report, err := NewService(repo, env).Status([]string{"ghost"})
	requireNoError(t, err)
	if len(report.Packages) != 1 {
		t.Fatalf("expected one package status, got %d", len(report.Packages))
	}
	if got := report.Packages[0].State; got != StateConflict {
		t.Fatalf("Package Source symlink loop should report CONFLICT, got %s", got)
	}
	if len(report.Packages[0].Entries) != 1 ||
		report.Packages[0].Entries[0].State != StateConflict {
		t.Fatalf(
			"Package Source symlink loop entry should report CONFLICT, got %#v",
			report.Packages[0].Entries,
		)
	}
}

func TestStatusReportsUnsupportedSpecialFilePackageSourceAsConflict(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, `version = 1

[packages.pipes]
links = [
  { source = "app.pipe", target = "~/.config/app.pipe" },
]
`)
	source := filepath.Join(repo, "pipes", "app.pipe")
	requireNoError(t, os.MkdirAll(filepath.Dir(source), 0o755))
	requireNoError(t, syscall.Mkfifo(source, 0o600))
	target := filepath.Join(home, ".config", "app.pipe")
	requireNoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	requireNoError(t, os.Symlink(source, target))

	report, err := NewService(repo, env).Status([]string{"pipes"})
	requireNoError(t, err)
	if len(report.Packages) != 1 {
		t.Fatalf("expected one package status, got %d", len(report.Packages))
	}
	if got := report.Packages[0].State; got != StateConflict {
		t.Fatalf("unsupported Package Source should report CONFLICT, got %s", got)
	}
	if len(report.Packages[0].Entries) != 1 ||
		report.Packages[0].Entries[0].State != StateConflict {
		t.Fatalf(
			"unsupported Package Source entry should report CONFLICT, got %#v",
			report.Packages[0].Entries,
		)
	}
}

func TestStatusReportsExternalHardlinkPackageSourceAsConflict(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, manifestWithSingleZshrcLink)
	protected := filepath.Join(home, "protected-zshrc")
	writeTextFile(t, protected, "export TOKEN=keep\n")
	source := filepath.Join(repo, "zsh", ".zshrc")
	requireNoError(t, os.MkdirAll(filepath.Dir(source), 0o755))
	if err := os.Link(protected, source); err != nil {
		t.Skipf("hardlinks are not supported in test filesystem: %v", err)
	}
	target := filepath.Join(home, ".zshrc")
	requireNoError(t, os.Symlink(source, target))

	report, err := NewService(repo, env).Status([]string{"zsh"})
	requireNoError(t, err)
	if len(report.Packages) != 1 {
		t.Fatalf("expected one package status, got %d", len(report.Packages))
	}
	if got := report.Packages[0].State; got != StateConflict {
		t.Fatalf("external-hardlink Package Source should report CONFLICT, got %s", got)
	}
	if len(report.Packages[0].Entries) != 1 ||
		report.Packages[0].Entries[0].State != StateConflict {
		t.Fatalf(
			"external-hardlink entry should report CONFLICT, got %#v",
			report.Packages[0].Entries,
		)
	}
}

func TestStatusReportsAbsentTargetUnderSymlinkedParentAsConflict(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, `version = 1

[packages.app]
links = [
  { source = "config", target = "~/.config/config" },
]
`)
	source := filepath.Join(repo, "app", "config")
	writeTextFile(t, source, "managed config\n")
	externalParent := filepath.Join(filepath.Dir(home), "external-config")
	requireNoError(t, os.MkdirAll(externalParent, 0o755))
	targetParent := filepath.Join(home, ".config")
	requireNoError(t, os.RemoveAll(targetParent))
	requireNoError(t, os.Symlink(externalParent, targetParent))

	report, err := NewService(repo, env).Status([]string{"app"})
	requireNoError(t, err)
	if len(report.Packages) != 1 {
		t.Fatalf("expected one package status, got %d", len(report.Packages))
	}
	if got := report.Packages[0].State; got != StateConflict {
		t.Fatalf("absent Target Path under symlinked parent should report CONFLICT, got %s", got)
	}
	if len(report.Packages[0].Entries) != 1 ||
		report.Packages[0].Entries[0].State != StateConflict {
		t.Fatalf(
			"absent Target Path under symlinked parent entry should report CONFLICT, got %#v",
			report.Packages[0].Entries,
		)
	}
	assertSymlink(t, targetParent, externalParent)
	requireNoPath(t, filepath.Join(externalParent, "config"))
	requireFileContent(t, source, "managed config\n")
}

func TestStatusReportsSymlinkedTargetParentAsConflictWithoutMutatingReferent(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, `version = 1

[packages.app]
links = [
  { source = "config", target = "~/.config/config" },
]
`)
	source := filepath.Join(repo, "app", "config")
	writeTextFile(t, source, "managed config\n")
	externalParent := filepath.Join(filepath.Dir(home), "external-config")
	requireNoError(t, os.MkdirAll(externalParent, 0o755))
	targetParent := filepath.Join(home, ".config")
	requireNoError(t, os.RemoveAll(targetParent))
	requireNoError(t, os.Symlink(externalParent, targetParent))
	referentLink := filepath.Join(externalParent, "config")
	requireNoError(t, os.Symlink(source, referentLink))

	report, err := NewService(repo, env).Status([]string{"app"})
	requireNoError(t, err)
	if len(report.Packages) != 1 {
		t.Fatalf("expected one package status, got %d", len(report.Packages))
	}
	if got := report.Packages[0].State; got != StateConflict {
		t.Fatalf("symlinked Target Path parent should report CONFLICT, got %s", got)
	}
	if len(report.Packages[0].Entries) != 1 ||
		report.Packages[0].Entries[0].State != StateConflict {
		t.Fatalf(
			"symlinked Target Path parent entry should report CONFLICT, got %#v",
			report.Packages[0].Entries,
		)
	}
	assertSymlink(t, targetParent, externalParent)
	assertSymlink(t, referentLink, source)
	requireFileContent(t, source, "managed config\n")
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

func TestStatusSummarizesMixedStatesByPriority(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	requireNoError(t, os.MkdirAll(repo, 0o755))
	writeDottyManifest(t, repo, `version = 1

[packages.conflict_priority]
links = [
  { source = "linked", target = "~/.config/conflict-priority-linked" },
  { source = "conflict", target = "~/.config/conflict-priority-conflict" },
  { source = "unlinked", target = "~/.config/conflict-priority-unlinked" },
]

[packages.missing_priority]
links = [
  { source = "missing", target = "~/.config/missing-priority-missing" },
  { source = "conflict", target = "~/.config/missing-priority-conflict" },
  { source = "linked", target = "~/.config/missing-priority-linked" },
]
`)
	writeTextFile(t, filepath.Join(repo, "conflict_priority", "linked"), "linked\n")
	writeTextFile(t, filepath.Join(repo, "conflict_priority", "conflict"), "conflict\n")
	writeTextFile(t, filepath.Join(repo, "conflict_priority", "unlinked"), "unlinked\n")
	writeTextFile(t, filepath.Join(repo, "missing_priority", "conflict"), "conflict\n")
	writeTextFile(t, filepath.Join(repo, "missing_priority", "linked"), "linked\n")
	requireNoError(t, os.MkdirAll(filepath.Join(home, ".config"), 0o755))
	requireNoError(
		t,
		os.Symlink(
			filepath.Join(repo, "conflict_priority", "linked"),
			filepath.Join(home, ".config", "conflict-priority-linked"),
		),
	)
	writeTextFile(
		t,
		filepath.Join(home, ".config", "conflict-priority-conflict"),
		"target copy\n",
	)
	writeTextFile(
		t,
		filepath.Join(home, ".config", "missing-priority-conflict"),
		"target copy\n",
	)
	requireNoError(
		t,
		os.Symlink(
			filepath.Join(repo, "missing_priority", "linked"),
			filepath.Join(home, ".config", "missing-priority-linked"),
		),
	)

	report, err := NewService(repo, env).Status(nil)
	requireNoError(t, err)

	gotStates := map[string]State{}
	for _, pkg := range report.Packages {
		gotStates[pkg.Name] = pkg.State
	}
	if gotStates["conflict_priority"] != StateConflict {
		t.Fatalf(
			"conflict should outrank linked/unlinked mixed states, got %s",
			gotStates["conflict_priority"],
		)
	}
	if gotStates["missing_priority"] != StateMissingSource {
		t.Fatalf(
			"missing source should outrank conflict and linked states, got %s",
			gotStates["missing_priority"],
		)
	}
}

func TestStatusPackageFilterReportsOnlySelectedPackageUntrackedContent(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	requireNoError(t, os.MkdirAll(repo, 0o755))
	writeDottyManifest(t, repo, `version = 1

[packages.nvim]
links = [
  { source = "init.lua", target = "~/.config/nvim/init.lua" },
]

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)
	writeTextFile(t, filepath.Join(repo, "nvim", "init.lua"), "vim.opt.nu = true\n")
	writeTextFile(t, filepath.Join(repo, "nvim", "after", "plugin.lua"), "unselected\n")
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "tmux"), 0o755))
	writeTextFile(t, filepath.Join(repo, "zsh", ".zshrc"), "source ~/.zprofile\n")
	writeTextFile(t, filepath.Join(repo, "zsh", ".zprofile"), "export PATH\n")
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "ghostty"), 0o755))

	report, err := NewService(repo, env).Status([]string{"zsh"})
	requireNoError(t, err)

	gotPackages := make([]string, 0, len(report.Packages))
	for _, pkg := range report.Packages {
		gotPackages = append(gotPackages, pkg.Name)
	}
	requireEqualStrings(t, gotPackages, []string{"zsh"})

	gotUntracked := make([]string, 0, len(report.Untracked))
	for _, item := range report.Untracked {
		gotUntracked = append(gotUntracked, item.Path)
	}
	requireEqualStrings(t, gotUntracked, []string{"zsh/.zprofile"})
	requireUntrackedItemLocation(t, report.Untracked[0], "zsh", ".zprofile")
}

func TestStatusPackageFilterRepeatsWithoutDuplicatingPackageUntrackedContent(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	requireNoError(t, os.MkdirAll(repo, 0o755))
	writeDottyManifest(t, repo, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)
	writeTextFile(t, filepath.Join(repo, "zsh", ".zshrc"), "source ~/.zprofile\n")
	writeTextFile(t, filepath.Join(repo, "zsh", ".zprofile"), "export PATH\n")

	report, err := NewService(repo, env).Status([]string{"zsh", "zsh"})
	requireNoError(t, err)

	gotPackages := make([]string, 0, len(report.Packages))
	for _, pkg := range report.Packages {
		gotPackages = append(gotPackages, pkg.Name)
	}
	requireEqualStrings(t, gotPackages, []string{"zsh", "zsh"})

	gotUntracked := make([]string, 0, len(report.Untracked))
	for _, item := range report.Untracked {
		gotUntracked = append(gotUntracked, item.Path)
	}
	requireEqualStrings(t, gotUntracked, []string{"zsh/.zprofile"})
}

func TestStatusPackageFilterSourceDotSuppressesPackageUntrackedContent(t *testing.T) {
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
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "ghostty"), 0o755))

	report, err := NewService(repo, env).Status([]string{"tmux"})
	requireNoError(t, err)

	if len(report.Untracked) != 0 {
		t.Fatalf(
			"source = . package should have no package-local untracked content, got %#v",
			report.Untracked,
		)
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
	byPath := map[string]UntrackedItem{}
	for _, item := range report.Untracked {
		if item.State != StateUntracked {
			t.Fatalf("untracked item %s has state %s", item.Path, item.State)
		}
		got = append(got, item.Path)
		byPath[item.Path] = item
	}
	requireEqualStrings(t, got, []string{"ghostty", "zsh/.zprofile", "zsh/secrets"})
	requireUntrackedItemLocation(t, byPath["ghostty"], "", "")
	requireUntrackedItemLocation(t, byPath["zsh/.zprofile"], "zsh", ".zprofile")
	requireUntrackedItemLocation(t, byPath["zsh/secrets"], "zsh", "secrets")
}

func TestStatusReportsSourceAwareUntrackedRepositoryContent(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	requireNoError(t, os.MkdirAll(repo, 0o755))
	writeDottyManifest(t, repo, `version = 1

[packages.nvim]
links = [
  { source = "config/nvim", target = "~/.config/nvim" },
]

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]
`)
	writeTextFile(
		t,
		filepath.Join(repo, "nvim", "config", "nvim", "init.lua"),
		"vim.opt.nu = true\n",
	)
	writeTextFile(
		t,
		filepath.Join(repo, "nvim", "config", "nvim", "after", "plugin.lua"),
		"tracked\n",
	)
	writeTextFile(t, filepath.Join(repo, "nvim", "config", "git", "config"), "[user]\n")
	writeTextFile(t, filepath.Join(repo, "nvim", ".secret"), "hidden package content\n")
	writeTextFile(t, filepath.Join(repo, "tmux", "tmux.conf"), "set -g mouse on\n")
	requireNoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(repo, ".config"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(home, "external"), 0o755))
	requireNoError(
		t,
		os.Symlink(filepath.Join(home, "external"), filepath.Join(repo, "nvim", "cache-link")),
	)
	requireNoError(
		t,
		os.Symlink(filepath.Join(home, "external"), filepath.Join(repo, "wezterm")),
	)

	report, err := NewService(repo, env).Status(nil)
	requireNoError(t, err)

	got := make([]string, 0, len(report.Untracked))
	for _, item := range report.Untracked {
		got = append(got, item.Path)
	}
	requireEqualStrings(
		t,
		got,
		[]string{
			".config",
			"nvim/.secret",
			"nvim/cache-link",
			"nvim/config/git",
			"wezterm",
		},
	)
}

func requireUntrackedItemLocation(
	t *testing.T,
	item UntrackedItem,
	wantPackage, wantSource string,
) {
	t.Helper()
	if item.Package != wantPackage || item.Source != wantSource {
		t.Fatalf(
			"untracked location mismatch for %s: want package=%q source=%q, got package=%q source=%q",
			item.Path,
			wantPackage,
			wantSource,
			item.Package,
			item.Source,
		)
	}
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
