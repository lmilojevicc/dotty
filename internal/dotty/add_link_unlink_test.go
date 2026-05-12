package dotty

import (
	"errors"
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
	requireFileContent(t, filepath.Join(repo, "zsh", ".zprofile"), "export PATH=$HOME/bin:$PATH\n")
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
  { source = ".zprofile", target = "~/.zprofile" },
]
`)

	manifest, err := LoadManifest(repo, env)
	requireNoError(t, err)
	if got := len(manifest.Packages["zsh"].Links); got != 2 {
		t.Fatalf("expected 2 zsh link mappings, got %d", got)
	}
}

func TestAddDirectoryAsNewPackageRecordsRootSourceAndManifest(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	svc, err := InitRepo(repo, env)
	requireNoError(t, err)

	target := filepath.Join(home, ".config", "tmux")
	writeTextFile(t, filepath.Join(target, "tmux.conf"), "set -g mouse on\n")

	result, err := svc.Add(target, "tmux")
	requireNoError(t, err)
	if result.Source != "." || result.Target != "~/.config/tmux" ||
		result.SourcePath != "~/dotfiles/tmux" {
		t.Fatalf("unexpected add result: %#v", result)
	}
	assertSymlink(t, target, filepath.Join(repo, "tmux"))
	requireFileContent(t, filepath.Join(repo, "tmux", "tmux.conf"), "set -g mouse on\n")
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]
`)
}

func TestAddDirectoryToExistingPackageUsesBasenameSource(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	svc, err := InitRepo(repo, env)
	requireNoError(t, err)

	zshrc := filepath.Join(home, ".zshrc")
	writeTextFile(t, zshrc, "export EDITOR=vim\n")
	_, err = svc.Add(zshrc, "config")
	requireNoError(t, err)

	nvimTarget := filepath.Join(home, ".config", "nvim")
	writeTextFile(t, filepath.Join(nvimTarget, "init.vim"), "set number\n")
	result, err := svc.Add(nvimTarget, "config")
	requireNoError(t, err)
	if result.Source != "nvim" || result.Target != "~/.config/nvim" {
		t.Fatalf("unexpected add result: %#v", result)
	}
	assertSymlink(t, nvimTarget, filepath.Join(repo, "config", "nvim"))
	requireFileContent(t, filepath.Join(repo, "config", "nvim", "init.vim"), "set number\n")
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.config]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
  { source = "nvim", target = "~/.config/nvim" },
]
`)
}

func TestAddExternalSymlinkToRegularFileCopiesSourceAndRecordsTarget(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	svc, err := InitRepo(repo, env)
	requireNoError(t, err)

	oldSource := filepath.Join(home, "old-stow", ".zshrc")
	writeTextFile(t, oldSource, "export EDITOR=nvim\n")
	target := filepath.Join(home, ".zshrc")
	requireNoError(t, os.Symlink(oldSource, target))

	result, err := svc.Add(target, "zsh")
	requireNoError(t, err)
	if result.Source != ".zshrc" || result.Target != "~/.zshrc" {
		t.Fatalf("unexpected add result: %#v", result)
	}
	assertSymlink(t, target, filepath.Join(repo, "zsh", ".zshrc"))
	requireFileContent(t, oldSource, "export EDITOR=nvim\n")
	requireFileContent(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=nvim\n")
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)
}

func TestAddAlreadyAdoptedTargetIsIdempotent(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	svc, err := InitRepo(repo, env)
	requireNoError(t, err)

	target := filepath.Join(home, ".zshrc")
	writeTextFile(t, target, "export EDITOR=vim\n")
	_, err = svc.Add(target, "zsh")
	requireNoError(t, err)
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)

	result, err := svc.Add(target, "zsh")
	requireNoError(t, err)
	if result.Source != ".zshrc" || result.Target != "~/.zshrc" {
		t.Fatalf("unexpected add result: %#v", result)
	}
	assertSymlink(t, target, filepath.Join(repo, "zsh", ".zshrc"))
	requireFileContent(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=vim\n")
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
	manifest, err := LoadManifest(repo, env)
	requireNoError(t, err)
	if got := len(manifest.Packages["zsh"].Links); got != 1 {
		t.Fatalf("expected idempotent add to keep one Link Mapping, got %d", got)
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

func TestAddRejectsRepositorySidePackageSourceConflictWithoutMutation(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	_, err := InitRepo(repo, env)
	requireNoError(t, err)
	target := filepath.Join(home, ".zshrc")
	writeTextFile(t, target, "local config\n")
	existingSource := filepath.Join(repo, "zsh", ".zshrc")
	writeTextFile(t, existingSource, "existing package source\n")
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)

	_, err = NewService(repo, env).Add(target, "zsh")
	requireErrorContains(t, err, "repo-side package source")
	requireErrorContains(t, err, "already exists")

	requireFileContent(t, target, "local config\n")
	requireFileContent(t, existingSource, "existing package source\n")
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
}

func TestAddRejectsBrokenAndLoopingSymlinkTargetsWithoutMutation(t *testing.T) {
	tests := []struct {
		name        string
		setupTarget func(t *testing.T, target string) []string
	}{
		{
			name: "broken symlink",
			setupTarget: func(t *testing.T, target string) []string {
				t.Helper()
				requireNoError(
					t,
					os.Symlink(filepath.Join(filepath.Dir(target), "missing"), target),
				)
				return []string{target}
			},
		},
		{
			name: "symlink loop",
			setupTarget: func(t *testing.T, target string) []string {
				t.Helper()
				other := filepath.Join(filepath.Dir(target), ".zshrc-other")
				requireNoError(t, os.Symlink(other, target))
				requireNoError(t, os.Symlink(target, other))
				return []string{target, other}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home, env := setupHome(t)
			repo := filepath.Join(home, "dotfiles")
			_, err := InitRepo(repo, env)
			requireNoError(t, err)
			target := filepath.Join(home, ".zshrc")
			symlinks := tt.setupTarget(t, target)
			linkTargetsBefore := map[string]string{}
			for _, symlink := range symlinks {
				linkTarget, err := os.Readlink(symlink)
				requireNoError(t, err)
				linkTargetsBefore[symlink] = linkTarget
			}
			manifestBefore, err := os.ReadFile(ManifestPath(repo))
			requireNoError(t, err)

			_, err = NewService(repo, env).Add(target, "zsh")
			requireErrorContains(t, err, "resolve symlink")

			requireFileContent(t, ManifestPath(repo), string(manifestBefore))
			requireNoPath(t, filepath.Join(repo, "zsh"))
			for symlink, wantTarget := range linkTargetsBefore {
				info, err := os.Lstat(symlink)
				requireNoError(t, err)
				if info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("%s should remain a symlink, info=%v", symlink, info)
				}
				gotTarget, err := os.Readlink(symlink)
				requireNoError(t, err)
				if gotTarget != wantTarget {
					t.Fatalf(
						"%s symlink target mismatch: want %s, got %s",
						symlink,
						wantTarget,
						gotTarget,
					)
				}
			}
		})
	}
}

func TestAddRejectsDangerousTargetPathsWithoutMutation(t *testing.T) {
	tests := []struct {
		name       string
		target     func(home, repo string) string
		setup      func(t *testing.T, target string)
		assertSafe func(t *testing.T, home, repo, target string, manifestBefore []byte)
	}{
		{
			name:   "home directory",
			target: func(home, repo string) string { return home },
			setup:  func(t *testing.T, target string) {},
			assertSafe: func(t *testing.T, home, repo, target string, manifestBefore []byte) {
				t.Helper()
				info, err := os.Lstat(home)
				requireNoError(t, err)
				if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					t.Fatalf("home should remain a real directory, info=%v", info)
				}
				requireFileContent(t, ManifestPath(repo), string(manifestBefore))
				requireNoPath(t, filepath.Join(repo, "pkg"))
			},
		},
		{
			name:   "dotfiles repository",
			target: func(home, repo string) string { return repo },
			setup:  func(t *testing.T, target string) {},
			assertSafe: func(t *testing.T, home, repo, target string, manifestBefore []byte) {
				t.Helper()
				info, err := os.Lstat(repo)
				requireNoError(t, err)
				if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					t.Fatalf("repository should remain a real directory, info=%v", info)
				}
				requireFileContent(t, ManifestPath(repo), string(manifestBefore))
				requireNoPath(t, filepath.Join(repo, "pkg"))
			},
		},
		{
			name:   "inside dotfiles repository",
			target: func(home, repo string) string { return filepath.Join(repo, "loose") },
			setup: func(t *testing.T, target string) {
				t.Helper()
				writeTextFile(t, target, "untracked repository content\n")
			},
			assertSafe: func(t *testing.T, home, repo, target string, manifestBefore []byte) {
				t.Helper()
				requireFileContent(t, target, "untracked repository content\n")
				requireFileContent(t, ManifestPath(repo), string(manifestBefore))
				requireNoPath(t, filepath.Join(repo, "pkg"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home, env := setupHome(t)
			repo := filepath.Join(home, "dotfiles")
			_, err := InitRepo(repo, env)
			requireNoError(t, err)
			target := tt.target(home, repo)
			tt.setup(t, target)
			manifestBefore, err := os.ReadFile(ManifestPath(repo))
			requireNoError(t, err)

			_, err = NewService(repo, env).Add(target, "pkg")
			requireErrorContains(t, err, "dangerous Target Path")
			tt.assertSafe(t, home, repo, target, manifestBefore)
		})
	}
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

func TestAddRefusesSymlinkedPackageSourceEscapingRepository(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	_, err := InitRepo(repo, env)
	requireNoError(t, err)

	externalSource := filepath.Join(home, "old-stow", "tmux")
	requireNoError(t, os.MkdirAll(externalSource, 0o755))
	writeTextFile(t, filepath.Join(externalSource, "tmux.conf"), "set -g status on\n")
	requireNoError(t, os.Symlink(externalSource, filepath.Join(repo, "tmux")))
	target := filepath.Join(home, ".config", "tmux")
	requireNoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	requireNoError(t, os.Symlink(externalSource, target))
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)

	_, err = NewService(repo, env).Add(target, "tmux")
	requireErrorContains(t, err, "escapes package")
	assertSymlink(t, target, externalSource)
	assertSymlink(t, filepath.Join(repo, "tmux"), externalSource)
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
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

func TestLinkRejectsDangerousTargetPathsEvenWhenForced(t *testing.T) {
	tests := []struct {
		name       string
		target     func(home, repo string) string
		assertSafe func(t *testing.T, home, repo, target string)
	}{
		{
			name:   "home directory",
			target: func(home, repo string) string { return "~" },
			assertSafe: func(t *testing.T, home, repo, target string) {
				t.Helper()
				info, err := os.Lstat(home)
				requireNoError(t, err)
				if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					t.Fatalf("home should remain a real directory, info=%v", info)
				}
				requireFileContent(t, ManifestPath(repo), dangerousTargetManifest(target))
			},
		},
		{
			name:   "dotfiles repository",
			target: func(home, repo string) string { return repo },
			assertSafe: func(t *testing.T, home, repo, target string) {
				t.Helper()
				info, err := os.Lstat(repo)
				requireNoError(t, err)
				if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
					t.Fatalf("repository should remain a real directory, info=%v", info)
				}
				requireFileContent(t, ManifestPath(repo), dangerousTargetManifest(target))
			},
		},
		{
			name:   "inside dotfiles repository",
			target: func(home, repo string) string { return filepath.Join(repo, "target") },
			assertSafe: func(t *testing.T, home, repo, target string) {
				t.Helper()
				requireNoPath(t, target)
				requireFileContent(t, ManifestPath(repo), dangerousTargetManifest(target))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home, env := setupHome(t)
			repo := filepath.Join(home, "dotfiles")
			requireNoError(t, os.MkdirAll(filepath.Join(repo, "pkg"), 0o755))
			writeTextFile(t, filepath.Join(repo, "pkg", "config"), "enabled = true\n")
			target := tt.target(home, repo)
			writeDottyManifest(t, repo, dangerousTargetManifest(target))

			_, err := NewService(
				repo,
				env,
			).Link(LinkOptions{Packages: []string{"pkg"}, Force: true})
			requireErrorContains(t, err, "dangerous Target Path")
			tt.assertSafe(t, home, repo, target)
		})
	}
}

func TestLinkValidatesOnlySelectedPackageTargets(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.danger]
links = [
  { source = ".", target = "~" },
]

[packages.safe]
links = [
  { source = ".", target = "~/.config/safe" },
]
`)
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "danger"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "safe"), 0o755))
	svc := NewService(repo, env)

	_, err := svc.Link(LinkOptions{Packages: []string{"safe"}})
	requireNoError(t, err)
	assertSymlink(t, filepath.Join(home, ".config", "safe"), filepath.Join(repo, "safe"))

	_, err = svc.Link(LinkOptions{Packages: []string{"danger"}, Force: true})
	requireErrorContains(t, err, "dangerous Target Path")
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

func TestLinkTreatsBrokenTargetSymlinkAsConflictUnlessForced(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)
	source := filepath.Join(repo, "zsh", ".zshrc")
	writeTextFile(t, source, "export EDITOR=vim\n")
	target := filepath.Join(home, ".zshrc")
	missingTarget := filepath.Join(home, "missing-zshrc")
	requireNoError(t, os.Symlink(missingTarget, target))
	svc := NewService(repo, env)

	assertZshPackageState(t, svc, StateConflict)
	_, err := svc.Link(LinkOptions{Packages: []string{"zsh"}})
	requireErrorContains(t, err, "symlink to another source")
	assertSymlink(t, target, missingTarget)

	_, err = svc.Link(LinkOptions{Packages: []string{"zsh"}, Force: true})
	requireNoError(t, err)
	assertSymlink(t, target, source)
	assertZshPackageState(t, svc, StateLinked)
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

func TestLinkForceReplacesOnlySelectedPackageConflicts(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "tmux"), 0o755))
	writeTextFile(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=vim\n")
	tmuxTarget := filepath.Join(home, ".config", "tmux")
	zshTarget := filepath.Join(home, ".zshrc")
	writeTextFile(t, filepath.Join(tmuxTarget, "tmux.conf"), "set -g status on\n")
	writeTextFile(t, zshTarget, "local zshrc\n")

	_, err := NewService(repo, env).Link(LinkOptions{
		Packages: []string{"zsh"},
		Force:    true,
	})
	requireNoError(t, err)

	assertSymlink(t, zshTarget, filepath.Join(repo, "zsh", ".zshrc"))
	requireFileContent(t, filepath.Join(tmuxTarget, "tmux.conf"), "set -g status on\n")
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

func TestLinkForceReportsMoveAsideFailureWithoutReplacingConflict(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)
	writeTextFile(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=vim\n")
	target := filepath.Join(home, ".zshrc")
	writeTextFile(t, target, "local copy\n")
	forceRenameError(t, syscall.EPERM)

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"zsh"}, Force: true})
	requireErrorContains(t, err, "move "+target+" aside")
	requireFileContent(t, target, "local copy\n")
	requireNoDottyBackups(t, home)
}

func TestLinkForceRollsBackEarlierReplacementWhenLaterMappingFailsDuringExecution(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = "config-file", target = "~/.config" },
  { source = ".zshrc", target = "~/.config/zsh/.zshrc" },
]
`)
	writeTextFile(t, filepath.Join(repo, "zsh", "config-file"), "not a directory\n")
	writeTextFile(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=vim\n")
	targetDir := filepath.Join(home, ".config")
	writeTextFile(t, filepath.Join(targetDir, "keep"), "preserve me\n")

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"zsh"}, Force: true})
	requireErrorContains(t, err, "not a directory")

	info, statErr := os.Lstat(targetDir)
	requireNoError(t, statErr)
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("%s should have rolled back to a real directory, info=%v", targetDir, info)
	}
	requireFileContent(t, filepath.Join(targetDir, "keep"), "preserve me\n")
	requireNoPath(t, filepath.Join(targetDir, "zsh", ".zshrc"))
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

func TestUnlinkRejectsDangerousTargetInsidePackageSource(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	source := filepath.Join(repo, "pkg")
	target := filepath.Join(source, "target")
	requireNoError(t, os.MkdirAll(source, 0o755))
	writeDottyManifest(t, repo, dangerousTargetManifest(target))
	requireNoError(t, os.Symlink(source, target))

	_, err := NewService(repo, env).Unlink(UnlinkOptions{Packages: []string{"pkg"}})
	requireErrorContains(t, err, "dangerous Target Path")
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

func TestSoftUnlinkPrevalidatesCopyabilityBeforeRemovingLink(t *testing.T) {
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
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)

	_, err = NewService(repo, env).Unlink(UnlinkOptions{Packages: []string{"zsh"}})
	requireErrorContains(t, err, "unsupported file type")

	assertSymlink(t, target, source)
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
}

func TestSoftUnlinkLeavesTargetCopyAsConflictForStatusAndPlainLink(t *testing.T) {
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
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)

	_, err = svc.Unlink(UnlinkOptions{Packages: []string{"zsh"}})
	requireNoError(t, err)

	requireFileContent(t, target, "export EDITOR=vim\n")
	requireFileContent(t, source, "export EDITOR=vim\n")
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
	assertZshPackageState(t, svc, StateConflict)

	_, err = svc.Link(LinkOptions{Packages: []string{"zsh"}})
	requireErrorContains(t, err, "already exists")
	requireFileContent(t, target, "export EDITOR=vim\n")
	assertZshPackageState(t, svc, StateConflict)
}

func TestHardUnlinkWithMissingSourceOnlyRemovesExpectedLinks(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, home, source, target string)
		wantErr    string
		assertSafe func(t *testing.T, target, source string)
	}{
		{
			name: "expected link",
			setup: func(t *testing.T, home, source, target string) {
				t.Helper()
				requireNoError(t, os.Symlink(source, target))
			},
			assertSafe: func(t *testing.T, target, source string) {
				t.Helper()
				requireNoPath(t, target)
			},
		},
		{
			name:  "absent target",
			setup: func(t *testing.T, home, source, target string) { t.Helper() },
			assertSafe: func(t *testing.T, target, source string) {
				t.Helper()
				requireNoPath(t, target)
			},
		},
		{
			name: "non-symlink conflict",
			setup: func(t *testing.T, home, source, target string) {
				t.Helper()
				writeTextFile(t, target, "local copy\n")
			},
			wantErr: "not an expected dotty link",
			assertSafe: func(t *testing.T, target, source string) {
				t.Helper()
				requireFileContent(t, target, "local copy\n")
			},
		},
		{
			name: "wrong symlink",
			setup: func(t *testing.T, home, source, target string) {
				t.Helper()
				wrongSource := filepath.Join(home, "wrong")
				writeTextFile(t, wrongSource, "wrong\n")
				requireNoError(t, os.Symlink(wrongSource, target))
			},
			wantErr: "symlink to another source",
			assertSafe: func(t *testing.T, target, source string) {
				t.Helper()
				assertSymlink(t, target, filepath.Join(filepath.Dir(target), "wrong"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)
			source := filepath.Join(repo, "zsh", ".zshrc")
			target := filepath.Join(home, ".zshrc")
			tt.setup(t, home, source, target)
			manifestBefore, err := os.ReadFile(ManifestPath(repo))
			requireNoError(t, err)

			_, err = NewService(repo, env).Unlink(UnlinkOptions{
				Packages: []string{"zsh"},
				Hard:     true,
			})
			if tt.wantErr == "" {
				requireNoError(t, err)
			} else {
				requireErrorContains(t, err, tt.wantErr)
			}

			tt.assertSafe(t, target, source)
			requireNoPath(t, source)
			requireFileContent(t, ManifestPath(repo), string(manifestBefore))
		})
	}
}

func TestSoftUnlinkRollsBackRemovedLinkWhenCopyFailsDuringExecution(t *testing.T) {
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
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)
	forceCopyPathErrorAfter(t, 0, errors.New("copy during unlink failed"))

	_, err = NewService(repo, env).Unlink(UnlinkOptions{Packages: []string{"zsh"}})
	requireErrorContains(t, err, "copy during unlink failed")

	assertSymlink(t, target, source)
	requireFileContent(t, source, "export EDITOR=vim\n")
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
	assertZshPackageState(t, NewService(repo, env), StateLinked)
}

func TestSoftUnlinkRollsBackEarlierTargetCopyWhenLaterMappingCopyFails(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
  { source = ".zprofile", target = "~/.zprofile" },
]
`)
	zshrcSource := filepath.Join(repo, "zsh", ".zshrc")
	zprofileSource := filepath.Join(repo, "zsh", ".zprofile")
	writeTextFile(t, zshrcSource, "export EDITOR=vim\n")
	writeTextFile(t, zprofileSource, "export PATH=$HOME/bin:$PATH\n")
	zshrcTarget := filepath.Join(home, ".zshrc")
	zprofileTarget := filepath.Join(home, ".zprofile")
	requireNoError(t, os.Symlink(zshrcSource, zshrcTarget))
	requireNoError(t, os.Symlink(zprofileSource, zprofileTarget))
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)
	forceCopyPathErrorAfter(t, 1, errors.New("second copy during unlink failed"))

	_, err = NewService(repo, env).Unlink(UnlinkOptions{Packages: []string{"zsh"}})
	requireErrorContains(t, err, "second copy during unlink failed")

	assertSymlink(t, zshrcTarget, zshrcSource)
	assertSymlink(t, zprofileTarget, zprofileSource)
	requireFileContent(t, zshrcSource, "export EDITOR=vim\n")
	requireFileContent(t, zprofileSource, "export PATH=$HOME/bin:$PATH\n")
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
	assertZshPackageState(t, NewService(repo, env), StateLinked)
}

func TestHardUnlinkRollsBackEarlierRemovalWhenLaterRemoveFails(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
  { source = ".zprofile", target = "~/.zprofile" },
]
`)
	zshrcSource := filepath.Join(repo, "zsh", ".zshrc")
	zprofileSource := filepath.Join(repo, "zsh", ".zprofile")
	writeTextFile(t, zshrcSource, "export EDITOR=vim\n")
	writeTextFile(t, zprofileSource, "export PATH=$HOME/bin:$PATH\n")
	zshrcTarget := filepath.Join(home, ".zshrc")
	zprofileTarget := filepath.Join(home, ".zprofile")
	requireNoError(t, os.Symlink(zshrcSource, zshrcTarget))
	requireNoError(t, os.Symlink(zprofileSource, zprofileTarget))
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)
	forceRemovePathErrorAfter(t, 1, errors.New("second remove during unlink failed"))

	_, err = NewService(repo, env).Unlink(UnlinkOptions{
		Packages: []string{"zsh"},
		Hard:     true,
	})
	requireErrorContains(t, err, "second remove during unlink failed")

	assertSymlink(t, zshrcTarget, zshrcSource)
	assertSymlink(t, zprofileTarget, zprofileSource)
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
	assertZshPackageState(t, NewService(repo, env), StateLinked)
}

func TestUnlinkReportsRollbackFailure(t *testing.T) {
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
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)
	forceCopyPathErrorAfter(t, 0, errors.New("copy during unlink failed"))
	forceSymlinkPathError(t, errors.New("restore symlink failed"))

	_, err = NewService(repo, env).Unlink(UnlinkOptions{Packages: []string{"zsh"}})
	requireErrorContains(t, err, "copy during unlink failed")
	requireErrorContains(t, err, "rollback failed")
	requireErrorContains(t, err, "restore symlink failed")

	requireNoPath(t, target)
	requireFileContent(t, source, "export EDITOR=vim\n")
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
}

func setupLinkedPackageTest(t *testing.T, manifest string) (home string, repo string, env Env) {
	t.Helper()
	home, env = setupHome(t)
	repo = filepath.Join(home, "dotfiles")
	requireNoError(t, os.MkdirAll(repo, 0o755))
	writeDottyManifest(t, repo, manifest)
	return home, repo, env
}

func dangerousTargetManifest(target string) string {
	return "version = 1\n\n[packages.pkg]\nlinks = [\n  { source = \".\", target = \"" + filepath.ToSlash(
		target,
	) + "\" },\n]\n"
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
