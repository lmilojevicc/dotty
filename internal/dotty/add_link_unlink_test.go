package dotty

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestAddFileToNewAndExistingPackageUsesBasenameSource(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	svc, err := InitRepo(repo, env)
	requireNoError(t, err)

	zshrc := filepath.Join(home, ".zshrc")
	writeTextFile(t, zshrc, "export EDITOR=vim\n")
	result, err := svc.Add(zshrc, "zsh")
	requireNoError(t, err)
	if result.Source != ".zshrc" || result.Target != "~/.zshrc" {
		t.Fatalf("unexpected add result: %#v", result)
	}
	assertSymlink(t, zshrc, filepath.Join(repo, "zsh", ".zshrc"))
	requireFileContent(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=vim\n")

	zprofile := filepath.Join(home, ".zprofile")
	writeTextFile(t, zprofile, "export PATH=$HOME/bin:$PATH\n")
	result, err = svc.Add(zprofile, "zsh")
	requireNoError(t, err)
	if result.Source != ".zprofile" || result.Target != "~/.zprofile" {
		t.Fatalf("unexpected add result: %#v", result)
	}
	assertSymlink(t, zprofile, filepath.Join(repo, "zsh", ".zprofile"))

	manifest, err := LoadManifest(repo, env)
	requireNoError(t, err)
	if got := len(manifest.Packages["zsh"].Links); got != 2 {
		t.Fatalf("expected 2 zsh link mappings, got %d", got)
	}
}

func TestAddRejectsMissingTargetWithoutChangingManifestOrRepository(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	_, err := InitRepo(repo, env)
	requireNoError(t, err)
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)

	_, err = NewService(repo, env).Add(filepath.Join(home, ".missing"), "zsh")
	requireErrorContains(t, err, "does not exist")
	requireErrorContains(t, err, "choose an existing Target Path")

	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
	requireNoPath(t, filepath.Join(repo, "zsh"))
}

func TestAddDryRunValidatesAndDoesNotMoveLinkOrWriteManifest(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	_, err := InitRepo(repo, env)
	requireNoError(t, err)
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)
	target := filepath.Join(home, ".config", "tmux")
	writeTextFile(t, filepath.Join(target, "tmux.conf"), "set -g mouse on\n")

	result, err := NewService(
		repo, env,
	).AddWithOptions(AddOptions{Target: target, Package: "tmux", DryRun: true})
	requireNoError(t, err)
	if !result.DryRun || result.Source != "." || result.Target != "~/.config/tmux" ||
		result.SourcePath != "~/dotfiles/tmux" {
		t.Fatalf("unexpected dry-run add result: %#v", result)
	}
	requireFileContent(t, filepath.Join(target, "tmux.conf"), "set -g mouse on\n")
	requireNoPath(t, filepath.Join(repo, "tmux"))
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
}

func TestAddDryRunRejectsExternalSymlinkSourceThatCannotBeCopied(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	_, err := InitRepo(repo, env)
	requireNoError(t, err)
	oldSource := filepath.Join(home, "old-stow", "tmux")
	requireNoError(t, os.MkdirAll(oldSource, 0o755))
	requireNoError(t, syscall.Mkfifo(filepath.Join(oldSource, "fifo"), 0o600))
	target := filepath.Join(home, ".config", "tmux")
	requireNoError(t, os.Symlink(oldSource, target))

	_, err = NewService(
		repo, env,
	).AddWithOptions(AddOptions{Target: target, Package: "tmux", DryRun: true})
	requireErrorContains(t, err, "unsupported file type")
	assertSymlink(t, target, oldSource)
	requireNoPath(t, filepath.Join(repo, "tmux"))
}

func TestAddDryRunRejectsExternalSymlinkSourceThatCannotBeRead(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	_, err := InitRepo(repo, env)
	requireNoError(t, err)
	oldSource := filepath.Join(home, "old-stow", "tmux")
	secret := filepath.Join(oldSource, "secret.conf")
	writeTextFile(t, secret, "token=secret\n")
	requireNoError(t, os.Chmod(secret, 0))
	t.Cleanup(func() {
		_ = os.Chmod(secret, 0o600)
	})
	target := filepath.Join(home, ".config", "tmux")
	requireNoError(t, os.Symlink(oldSource, target))

	_, err = NewService(
		repo, env,
	).AddWithOptions(AddOptions{Target: target, Package: "tmux", DryRun: true})
	requireErrorContains(t, err, "open")
	assertSymlink(t, target, oldSource)
	requireNoPath(t, filepath.Join(repo, "tmux"))
}

func TestAddRefusesSymlinkPointingInsideRepositoryButNotIntendedSource(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	_, err := InitRepo(repo, env)
	requireNoError(t, err)
	actualSource := filepath.Join(repo, "stow-tmux")
	requireNoError(t, os.MkdirAll(actualSource, 0o755))
	writeTextFile(t, filepath.Join(actualSource, "tmux.conf"), "set -g status on\n")
	target := filepath.Join(home, ".config", "tmux")
	requireNoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	requireNoError(t, os.Symlink(actualSource, target))

	_, err = NewService(repo, env).Add(target, "tmux")
	requireErrorContains(t, err, "inside the dotfiles repository")
	assertSymlink(t, target, actualSource)
	requireNoPath(t, filepath.Join(repo, "tmux"))
}

func TestAddRefusesSymlinkPointingInsideSymlinkedRepository(t *testing.T) {
	home, env := setupHome(t)
	realRepo := filepath.Join(home, "real-dotfiles")
	repoLink := filepath.Join(home, "dotfiles-link")
	_, err := InitRepo(realRepo, env)
	requireNoError(t, err)
	requireNoError(t, os.Symlink(realRepo, repoLink))

	actualSource := filepath.Join(realRepo, "stow-tmux")
	requireNoError(t, os.MkdirAll(actualSource, 0o755))
	writeTextFile(t, filepath.Join(actualSource, "tmux.conf"), "set -g status on\n")
	target := filepath.Join(home, ".config", "tmux")
	requireNoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	requireNoError(t, os.Symlink(actualSource, target))

	_, err = NewService(repoLink, env).Add(target, "tmux")
	requireErrorContains(t, err, "inside the dotfiles repository")
	assertSymlink(t, target, actualSource)
	requireNoPath(t, filepath.Join(realRepo, "tmux"))
}

func TestLinkRefusesTargetConflictUnlessForced(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)
	writeTextFile(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=vim\n")
	target := filepath.Join(home, ".zshrc")
	writeTextFile(t, target, "local copy\n")
	svc := NewService(repo, env)

	_, err := svc.Link(LinkOptions{Packages: []string{"zsh"}})
	requireErrorContains(t, err, "already exists")
	requireFileContent(t, target, "local copy\n")

	_, err = svc.Link(LinkOptions{Packages: []string{"zsh"}, Force: true})
	requireNoError(t, err)
	assertSymlink(t, target, filepath.Join(repo, "zsh", ".zshrc"))
}

func TestLinkDryRunValidatesForceWithoutReplacingConflict(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)
	writeTextFile(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=vim\n")
	target := filepath.Join(home, ".zshrc")
	writeTextFile(t, target, "local copy\n")

	results, err := NewService(
		repo, env,
	).Link(LinkOptions{Packages: []string{"zsh"}, Force: true, DryRun: true})
	requireNoError(t, err)
	if len(results) != 1 || !results[0].DryRun || results[0].Target != "~/.zshrc" {
		t.Fatalf("unexpected link dry-run results: %#v", results)
	}
	requireFileContent(t, target, "local copy\n")
}

func TestLinkDryRunRejectsTargetParentConflict(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.config/zsh/.zshrc" },
]
`)
	writeTextFile(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=vim\n")
	writeTextFile(t, filepath.Join(home, ".config", "zsh"), "not a directory\n")

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"zsh"}, DryRun: true})
	requireErrorContains(t, err, "not a directory")
	requireFileContent(t, filepath.Join(home, ".config", "zsh"), "not a directory\n")
}

func TestLinkRefusesWrongSymlinkUnlessForced(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)
	writeTextFile(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=vim\n")
	target := filepath.Join(home, ".zshrc")
	wrongSource := filepath.Join(home, "other-zshrc")
	writeTextFile(t, wrongSource, "wrong\n")
	requireNoError(t, os.Symlink(wrongSource, target))
	svc := NewService(repo, env)

	_, err := svc.Link(LinkOptions{Packages: []string{"zsh"}})
	requireErrorContains(t, err, "symlink to another source")
	assertSymlink(t, target, wrongSource)

	_, err = svc.Link(LinkOptions{Packages: []string{"zsh"}, Force: true})
	requireNoError(t, err)
	assertSymlink(t, target, filepath.Join(repo, "zsh", ".zshrc"))
}

func TestLinkNormalizesExpectedRelativeSymlinkToAbsoluteLink(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]
`)
	source := filepath.Join(repo, "tmux")
	requireNoError(t, os.MkdirAll(source, 0o755))
	target := filepath.Join(home, ".config", "tmux")
	requireNoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	rel, err := filepath.Rel(filepath.Dir(target), source)
	requireNoError(t, err)
	requireNoError(t, os.Symlink(rel, target))

	_, err = NewService(repo, env).Link(LinkOptions{Packages: []string{"tmux"}})
	requireNoError(t, err)
	assertSymlink(t, target, source)

	_, err = NewService(repo, env).Link(LinkOptions{Packages: []string{"tmux"}})
	requireNoError(t, err)
	assertSymlink(t, target, source)
}

func TestLinkRollsBackEarlierLinksWhenLaterMappingFails(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
  { source = ".missing", target = "~/.missing" },
]
`)
	writeTextFile(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=vim\n")

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"zsh"}})
	requireErrorContains(t, err, "source \".missing\" is missing")
	requireNoPath(t, filepath.Join(home, ".zshrc"))
}

func TestLinkForcePrevalidationLeavesConflictUnchangedWhenLaterSourceMissing(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
  { source = ".missing", target = "~/.missing" },
]
`)
	writeTextFile(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=vim\n")
	target := filepath.Join(home, ".zshrc")
	writeTextFile(t, target, "local copy\n")

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"zsh"}, Force: true})
	requireErrorContains(t, err, "source \".missing\" is missing")
	requireFileContent(t, target, "local copy\n")
	requireNoDottyBackups(t, home)
}

func TestLinkAllLinksEveryPackageAndBareLinkRequiresSelection(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]
`)
	writeTextFile(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=vim\n")
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "tmux"), 0o755))
	svc := NewService(repo, env)

	_, err := svc.Link(LinkOptions{})
	requireErrorContains(t, err, "select at least one package or collection")

	results, err := svc.Link(LinkOptions{All: true})
	requireNoError(t, err)
	requireResultPackages(t, results, []string{"tmux", "zsh"})
	assertSymlink(t, filepath.Join(home, ".config", "tmux"), filepath.Join(repo, "tmux"))
	assertSymlink(t, filepath.Join(home, ".zshrc"), filepath.Join(repo, "zsh", ".zshrc"))
}

func TestUnlinkHandlesAbsentTargetsAndHardUnlinkWithoutSource(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)
	svc := NewService(repo, env)

	results, err := svc.Unlink(UnlinkOptions{Packages: []string{"zsh"}})
	requireNoError(t, err)
	if len(results) != 1 || results[0].Target != "~/.zshrc" {
		t.Fatalf("unexpected unlink results: %#v", results)
	}

	target := filepath.Join(home, ".zshrc")
	expectedSource := filepath.Join(repo, "zsh", ".zshrc")
	requireNoError(t, os.Symlink(expectedSource, target))
	_, err = svc.Unlink(UnlinkOptions{Packages: []string{"zsh"}, Hard: true})
	requireNoError(t, err)
	requireNoPath(t, target)
}

func TestUnlinkDryRunLeavesSoftAndHardTargetsUnchanged(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)
	source := filepath.Join(repo, "zsh", ".zshrc")
	writeTextFile(t, source, "export EDITOR=vim\n")
	target := filepath.Join(home, ".zshrc")
	requireNoError(t, os.Symlink(source, target))
	svc := NewService(repo, env)

	results, err := svc.Unlink(UnlinkOptions{Packages: []string{"zsh"}, DryRun: true})
	requireNoError(t, err)
	if len(results) != 1 || !results[0].DryRun || results[0].Hard {
		t.Fatalf("unexpected soft unlink dry-run results: %#v", results)
	}
	assertSymlink(t, target, source)

	results, err = svc.Unlink(UnlinkOptions{Packages: []string{"zsh"}, Hard: true, DryRun: true})
	requireNoError(t, err)
	if len(results) != 1 || !results[0].DryRun || !results[0].Hard {
		t.Fatalf("unexpected hard unlink dry-run results: %#v", results)
	}
	assertSymlink(t, target, source)
}

func TestUnlinkDryRunRejectsSourceThatCannotBeCopied(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = ".", target = "~/.config/zsh" },
]
`)
	source := filepath.Join(repo, "zsh")
	requireNoError(t, os.MkdirAll(source, 0o755))
	requireNoError(t, syscall.Mkfifo(filepath.Join(source, "fifo"), 0o600))
	target := filepath.Join(home, ".config", "zsh")
	requireNoError(t, os.Symlink(source, target))

	_, err := NewService(repo, env).Unlink(UnlinkOptions{Packages: []string{"zsh"}, DryRun: true})
	requireErrorContains(t, err, "unsupported file type")
	assertSymlink(t, target, source)
}

func TestUnlinkAllUnlinksEveryPackage(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]
`)
	writeTextFile(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=vim\n")
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "tmux"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(home, ".config"), 0o755))
	requireNoError(
		t,
		os.Symlink(filepath.Join(repo, "tmux"), filepath.Join(home, ".config", "tmux")),
	)
	requireNoError(
		t,
		os.Symlink(filepath.Join(repo, "zsh", ".zshrc"), filepath.Join(home, ".zshrc")),
	)

	results, err := NewService(repo, env).Unlink(UnlinkOptions{All: true, Hard: true})
	requireNoError(t, err)
	requireUnlinkResultPackages(t, results, []string{"tmux", "zsh"})
	requireNoPath(t, filepath.Join(home, ".config", "tmux"))
	requireNoPath(t, filepath.Join(home, ".zshrc"))
}

func TestUnlinkRefusesConflictsAndWrongSymlinks(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)
	writeTextFile(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=vim\n")
	svc := NewService(repo, env)
	target := filepath.Join(home, ".zshrc")

	writeTextFile(t, target, "local copy\n")
	_, err := svc.Unlink(UnlinkOptions{Packages: []string{"zsh"}})
	requireErrorContains(t, err, "not an expected dotty link")
	requireFileContent(t, target, "local copy\n")

	requireNoError(t, os.Remove(target))
	wrongSource := filepath.Join(home, "wrong")
	writeTextFile(t, wrongSource, "wrong\n")
	requireNoError(t, os.Symlink(wrongSource, target))
	_, err = svc.Unlink(UnlinkOptions{Packages: []string{"zsh"}})
	requireErrorContains(t, err, "symlink to another source")
	assertSymlink(t, target, wrongSource)
}

func TestSoftUnlinkCopiesSourceAndFailsWhenSourceIsMissing(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)
	source := filepath.Join(repo, "zsh", ".zshrc")
	writeTextFile(t, source, "export EDITOR=vim\n")
	target := filepath.Join(home, ".zshrc")
	requireNoError(t, os.Symlink(source, target))
	svc := NewService(repo, env)

	_, err := svc.Unlink(UnlinkOptions{Packages: []string{"zsh"}})
	requireNoError(t, err)
	requireFileContent(t, target, "export EDITOR=vim\n")

	requireNoError(t, os.Remove(target))
	requireNoError(t, os.Remove(source))
	requireNoError(t, os.Symlink(source, target))
	_, err = svc.Unlink(UnlinkOptions{Packages: []string{"zsh"}})
	requireErrorContains(t, err, "source \".zshrc\" is missing")
	assertSymlink(t, target, source)
}

func setupLinkedPackageTest(t *testing.T, manifest string) (home string, repo string, env Env) {
	t.Helper()
	home, env = setupHome(t)
	repo = filepath.Join(home, "dotfiles")
	requireNoError(t, os.MkdirAll(repo, 0o755))
	writeDottyManifest(t, repo, manifest)
	return home, repo, env
}

func requireResultPackages(t *testing.T, results []LinkResult, want []string) {
	t.Helper()
	got := make([]string, 0, len(results))
	for _, result := range results {
		got = append(got, result.Package)
	}
	requireEqualStrings(t, got, want)
}

func requireUnlinkResultPackages(t *testing.T, results []UnlinkResult, want []string) {
	t.Helper()
	got := make([]string, 0, len(results))
	for _, result := range results {
		got = append(got, result.Package)
	}
	requireEqualStrings(t, got, want)
}
