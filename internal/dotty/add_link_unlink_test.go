package dotty

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const manifestWithSingleZshrcLink = `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`

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

func TestAddDirectoryPreservesInternalHardlinksWhileBreakingExternalAliases(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	svc, err := InitRepo(repo, env)
	requireNoError(t, err)

	protected := filepath.Join(home, "protected-config")
	target := filepath.Join(home, ".config", "app")
	writeTextFile(t, protected, "secret=keep\n")
	requireNoError(t, os.MkdirAll(target, 0o755))
	config := filepath.Join(target, "config")
	if err := os.Link(protected, config); err != nil {
		t.Skipf("hardlinks are not supported in test filesystem: %v", err)
	}
	requireNoError(t, os.Link(config, filepath.Join(target, "config-alias")))

	_, err = svc.Add(target, "app")
	requireNoError(t, err)

	source := filepath.Join(repo, "app", "config")
	sourceAlias := filepath.Join(repo, "app", "config-alias")
	assertSymlink(t, target, filepath.Join(repo, "app"))
	requireFileContent(t, source, "secret=keep\n")
	requireFileContent(t, sourceAlias, "secret=keep\n")
	requireFileContent(t, protected, "secret=keep\n")
	protectedInfo, err := os.Stat(protected)
	requireNoError(t, err)
	sourceInfo, err := os.Stat(source)
	requireNoError(t, err)
	sourceAliasInfo, err := os.Stat(sourceAlias)
	requireNoError(t, err)
	if os.SameFile(protectedInfo, sourceInfo) {
		t.Fatalf("repo nested Package Source should not share an inode with external hardlink")
	}
	if !os.SameFile(sourceInfo, sourceAliasInfo) {
		t.Fatalf("repo copy should preserve internal hardlink relationship")
	}

	writeTextFile(t, source, "managed edit\n")
	requireFileContent(t, sourceAlias, "managed edit\n")
	requireFileContent(t, protected, "secret=keep\n")
}

func TestAddDirectoryCopiesNestedHardlinksIntoRepositoryWithoutAliasingExternalInode(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	svc, err := InitRepo(repo, env)
	requireNoError(t, err)

	protected := filepath.Join(home, "protected-config")
	target := filepath.Join(home, ".config", "app")
	writeTextFile(t, protected, "secret=keep\n")
	requireNoError(t, os.MkdirAll(target, 0o755))
	if err := os.Link(protected, filepath.Join(target, "config")); err != nil {
		t.Skipf("hardlinks are not supported in test filesystem: %v", err)
	}

	_, err = svc.Add(target, "app")
	requireNoError(t, err)

	source := filepath.Join(repo, "app", "config")
	assertSymlink(t, target, filepath.Join(repo, "app"))
	requireFileContent(t, source, "secret=keep\n")
	requireFileContent(t, protected, "secret=keep\n")
	protectedInfo, err := os.Stat(protected)
	requireNoError(t, err)
	sourceInfo, err := os.Stat(source)
	requireNoError(t, err)
	if os.SameFile(protectedInfo, sourceInfo) {
		t.Fatalf("repo nested Package Source should not share an inode with external hardlink")
	}

	writeTextFile(t, source, "managed edit\n")
	requireFileContent(t, protected, "secret=keep\n")
}

func TestAddHardlinkedFileCopiesIntoRepositoryWithoutAliasingExternalInode(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	svc, err := InitRepo(repo, env)
	requireNoError(t, err)

	protected := filepath.Join(home, "protected-zshrc")
	target := filepath.Join(home, ".zshrc")
	writeTextFile(t, protected, "export TOKEN=keep\n")
	if err := os.Link(protected, target); err != nil {
		t.Skipf("hardlinks are not supported in test filesystem: %v", err)
	}

	_, err = svc.Add(target, "zsh")
	requireNoError(t, err)

	source := filepath.Join(repo, "zsh", ".zshrc")
	assertSymlink(t, target, source)
	requireFileContent(t, source, "export TOKEN=keep\n")
	requireFileContent(t, protected, "export TOKEN=keep\n")
	protectedInfo, err := os.Stat(protected)
	requireNoError(t, err)
	sourceInfo, err := os.Stat(source)
	requireNoError(t, err)
	if os.SameFile(protectedInfo, sourceInfo) {
		t.Fatalf("repo Package Source should not share an inode with external hardlink")
	}

	writeTextFile(t, source, "managed edit\n")
	requireFileContent(t, protected, "export TOKEN=keep\n")
}

func TestAddDirectoryNestedHardlinkRollbackPreservesExternalInode(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	svc, err := InitRepo(repo, env)
	requireNoError(t, err)

	protected := filepath.Join(home, "protected-config")
	target := filepath.Join(home, ".config", "app")
	writeTextFile(t, protected, "secret=keep\n")
	requireNoError(t, os.MkdirAll(target, 0o755))
	if err := os.Link(protected, filepath.Join(target, "config")); err != nil {
		t.Skipf("hardlinks are not supported in test filesystem: %v", err)
	}
	externalManifest := filepath.Join(home, "external-dotty.toml")
	requireNoError(t, os.Rename(ManifestPath(repo), externalManifest))
	requireNoError(t, os.Symlink(externalManifest, ManifestPath(repo)))

	_, err = svc.Add(target, "app")
	requireErrorContains(t, err, "refuse to replace symlink")

	targetInfo, err := os.Stat(filepath.Join(target, "config"))
	requireNoError(t, err)
	protectedInfo, err := os.Stat(protected)
	requireNoError(t, err)
	if !os.SameFile(targetInfo, protectedInfo) {
		t.Fatalf("rollback should preserve nested hardlink relationship")
	}
	requireFileContent(t, filepath.Join(target, "config"), "secret=keep\n")
	requireFileContent(t, protected, "secret=keep\n")
	requireNoPath(t, filepath.Join(repo, "app"))
	assertSymlink(t, ManifestPath(repo), externalManifest)
}

func TestAddHardlinkedFileRollbackPreservesExternalHardlink(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	svc, err := InitRepo(repo, env)
	requireNoError(t, err)

	protected := filepath.Join(home, "protected-zshrc")
	target := filepath.Join(home, ".zshrc")
	writeTextFile(t, protected, "export TOKEN=keep\n")
	if err := os.Link(protected, target); err != nil {
		t.Skipf("hardlinks are not supported in test filesystem: %v", err)
	}
	externalManifest := filepath.Join(home, "external-dotty.toml")
	requireNoError(t, os.Rename(ManifestPath(repo), externalManifest))
	requireNoError(t, os.Symlink(externalManifest, ManifestPath(repo)))

	_, err = svc.Add(target, "zsh")
	requireErrorContains(t, err, "refuse to replace symlink")

	targetInfo, err := os.Stat(target)
	requireNoError(t, err)
	protectedInfo, err := os.Stat(protected)
	requireNoError(t, err)
	if !os.SameFile(targetInfo, protectedInfo) {
		t.Fatalf("rollback should preserve original hardlink relationship")
	}
	requireFileContent(t, target, "export TOKEN=keep\n")
	requireFileContent(t, protected, "export TOKEN=keep\n")
	requireNoPath(t, filepath.Join(repo, "zsh"))
	assertSymlink(t, ManifestPath(repo), externalManifest)
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

func TestAddExternalSymlinkToDirectoryRejectsNestedSymlinkResolvingToUnsupportedSpecialFile(
	t *testing.T,
) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	svc, err := InitRepo(repo, env)
	requireNoError(t, err)

	oldSource := filepath.Join(home, "old-stow", "app")
	requireNoError(t, os.MkdirAll(oldSource, 0o755))
	writeTextFile(t, filepath.Join(oldSource, "app.conf"), "regular config\n")
	fifo := filepath.Join(home, "external-fifo")
	requireNoError(t, syscall.Mkfifo(fifo, 0o600))
	nestedLink := filepath.Join(oldSource, "current.pipe")
	requireNoError(t, os.Symlink(fifo, nestedLink))
	target := filepath.Join(home, ".config", "app")
	requireNoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	requireNoError(t, os.Symlink(oldSource, target))
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)

	_, err = svc.Add(target, "app")
	requireErrorContains(t, err, "unsupported file type")

	assertSymlink(t, target, oldSource)
	assertSymlink(t, nestedLink, fifo)
	info, err := os.Lstat(fifo)
	requireNoError(t, err)
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("external FIFO referent should remain unchanged, mode=%v", info.Mode())
	}
	requireNoPath(t, filepath.Join(repo, "app"))
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
}

func TestAddExternalSymlinkToDirectoryRejectsAbsoluteInternalSymlinkToExternalHardlink(
	t *testing.T,
) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	svc, err := InitRepo(repo, env)
	requireNoError(t, err)

	protected := filepath.Join(home, "protected-token")
	writeTextFile(t, protected, "token=keep\n")
	oldSource := filepath.Join(home, "old-stow", "app")
	requireNoError(t, os.MkdirAll(oldSource, 0o755))
	token := filepath.Join(oldSource, "token")
	if err := os.Link(protected, token); err != nil {
		t.Skipf("hardlinks are not supported in test filesystem: %v", err)
	}
	nestedLink := filepath.Join(oldSource, "current-token")
	requireNoError(t, os.Symlink(token, nestedLink))
	target := filepath.Join(home, ".config", "app")
	requireNoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	requireNoError(t, os.Symlink(oldSource, target))
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)

	_, err = svc.Add(target, "app")
	requireErrorContains(t, err, "external hardlink")

	assertSymlink(t, target, oldSource)
	assertSymlink(t, nestedLink, token)
	requireFileContent(t, protected, "token=keep\n")
	tokenInfo, err := os.Stat(token)
	requireNoError(t, err)
	protectedInfo, err := os.Stat(protected)
	requireNoError(t, err)
	if !os.SameFile(tokenInfo, protectedInfo) {
		t.Fatalf("failed add should not alter external hardlink topology")
	}
	requireNoPath(t, filepath.Join(repo, "app"))
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
}

func TestAddInPlaceSymlinkAdoptionRefusesExternalHardlinkSource(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	svc, err := InitRepo(repo, env)
	requireNoError(t, err)

	protected := filepath.Join(home, "protected-zshrc")
	writeTextFile(t, protected, "export TOKEN=keep\n")
	source := filepath.Join(repo, "zsh", ".zshrc")
	requireNoError(t, os.MkdirAll(filepath.Dir(source), 0o755))
	if err := os.Link(protected, source); err != nil {
		t.Skipf("hardlinks are not supported in test filesystem: %v", err)
	}
	target := filepath.Join(home, ".zshrc")
	requireNoError(t, os.Symlink(source, target))
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)

	_, err = svc.Add(target, "zsh")
	requireErrorContains(t, err, "external hardlink")

	assertSymlink(t, target, source)
	requireFileContent(t, protected, "export TOKEN=keep\n")
	sourceInfo, err := os.Stat(source)
	requireNoError(t, err)
	protectedInfo, err := os.Stat(protected)
	requireNoError(t, err)
	if !os.SameFile(sourceInfo, protectedInfo) {
		t.Fatalf("failed add should not alter the repo hardlink source")
	}
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
}

func TestAddInPlaceSymlinkAdoptionRefusesSourceSymlinkResolvingToUnsupportedSpecialFile(
	t *testing.T,
) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	svc, err := InitRepo(repo, env)
	requireNoError(t, err)

	realSource := filepath.Join(repo, "pipes", "real.pipe")
	requireNoError(t, os.MkdirAll(filepath.Dir(realSource), 0o755))
	requireNoError(t, syscall.Mkfifo(realSource, 0o600))
	sourceLink := filepath.Join(repo, "pipes", "app.pipe")
	requireNoError(t, os.Symlink("real.pipe", sourceLink))
	target := filepath.Join(home, ".config", "app.pipe")
	requireNoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	requireNoError(t, os.Symlink(sourceLink, target))
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)

	_, err = svc.Add(target, "pipes")
	requireErrorContains(t, err, "unsupported file type")

	assertSymlink(t, target, sourceLink)
	assertSymlink(t, sourceLink, "real.pipe")
	info, err := os.Lstat(realSource)
	requireNoError(t, err)
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("repo FIFO referent should remain unchanged, mode=%v", info.Mode())
	}
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
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

func TestAddRejectsSymlinkToUnsupportedSpecialFileTargetWithoutMutation(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	_, err := InitRepo(repo, env)
	requireNoError(t, err)
	fifo := filepath.Join(home, "external-fifo")
	requireNoError(t, syscall.Mkfifo(fifo, 0o600))
	target := filepath.Join(home, ".config", "target")
	requireNoError(t, os.Symlink(fifo, target))
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)

	_, err = NewService(repo, env).Add(target, "pkg")
	requireErrorContains(t, err, "unsupported file type")

	assertSymlink(t, target, fifo)
	info, err := os.Lstat(fifo)
	requireNoError(t, err)
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("FIFO referent should remain unchanged, mode=%v", info.Mode())
	}
	requireNoPath(t, filepath.Join(repo, "pkg"))
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
}

func TestAddRejectsUnsupportedSpecialFileTargetsWithoutMutation(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, target string)
		assertSafe func(t *testing.T, target, repo string)
	}{
		{
			name: "fifo target",
			setup: func(t *testing.T, target string) {
				t.Helper()
				requireNoError(t, syscall.Mkfifo(target, 0o600))
			},
			assertSafe: func(t *testing.T, target, repo string) {
				t.Helper()
				info, err := os.Lstat(target)
				requireNoError(t, err)
				if info.Mode()&os.ModeNamedPipe == 0 {
					t.Fatalf("target FIFO should remain unchanged, mode=%v", info.Mode())
				}
				requireNoPath(t, filepath.Join(repo, "pkg"))
			},
		},
		{
			name: "directory containing fifo",
			setup: func(t *testing.T, target string) {
				t.Helper()
				writeTextFile(t, filepath.Join(target, "config"), "regular config\n")
				requireNoError(t, syscall.Mkfifo(filepath.Join(target, "socket"), 0o600))
			},
			assertSafe: func(t *testing.T, target, repo string) {
				t.Helper()
				requireFileContent(t, filepath.Join(target, "config"), "regular config\n")
				info, err := os.Lstat(filepath.Join(target, "socket"))
				requireNoError(t, err)
				if info.Mode()&os.ModeNamedPipe == 0 {
					t.Fatalf("nested FIFO should remain unchanged, mode=%v", info.Mode())
				}
				requireNoPath(t, filepath.Join(repo, "pkg"))
			},
		},
		{
			name: "directory containing symlink to fifo",
			setup: func(t *testing.T, target string) {
				t.Helper()
				fifo := filepath.Join(filepath.Dir(target), "external-fifo")
				requireNoError(t, syscall.Mkfifo(fifo, 0o600))
				writeTextFile(t, filepath.Join(target, "config"), "regular config\n")
				requireNoError(t, os.Symlink(fifo, filepath.Join(target, "current.pipe")))
			},
			assertSafe: func(t *testing.T, target, repo string) {
				t.Helper()
				requireFileContent(t, filepath.Join(target, "config"), "regular config\n")
				current := filepath.Join(target, "current.pipe")
				fifo, err := os.Readlink(current)
				requireNoError(t, err)
				assertSymlink(t, current, fifo)
				info, err := os.Lstat(fifo)
				requireNoError(t, err)
				if info.Mode()&os.ModeNamedPipe == 0 {
					t.Fatalf("symlink FIFO referent should remain unchanged, mode=%v", info.Mode())
				}
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
			target := filepath.Join(home, ".config", "target")
			tt.setup(t, target)
			manifestBefore, err := os.ReadFile(ManifestPath(repo))
			requireNoError(t, err)

			_, err = NewService(repo, env).Add(target, "pkg")
			requireErrorContains(t, err, "unsupported file type")

			tt.assertSafe(t, target, repo)
			requireFileContent(t, ManifestPath(repo), string(manifestBefore))
		})
	}
}

func TestAddRejectsDirectoryContainingSymlinkToExternalHardlinkWithoutMutation(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	_, err := InitRepo(repo, env)
	requireNoError(t, err)
	protected := filepath.Join(home, "protected-token")
	externalAlias := filepath.Join(home, "external-token")
	writeTextFile(t, protected, "token=keep\n")
	if err := os.Link(protected, externalAlias); err != nil {
		t.Skipf("hardlinks are not supported in test filesystem: %v", err)
	}
	target := filepath.Join(home, ".config", "target")
	writeTextFile(t, filepath.Join(target, "config"), "regular config\n")
	requireNoError(t, os.Symlink(externalAlias, filepath.Join(target, "current-token")))
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)

	_, err = NewService(repo, env).Add(target, "pkg")
	requireErrorContains(t, err, "external hardlink")

	requireFileContent(t, filepath.Join(target, "config"), "regular config\n")
	assertSymlink(t, filepath.Join(target, "current-token"), externalAlias)
	requireFileContent(t, protected, "token=keep\n")
	aliasInfo, err := os.Stat(externalAlias)
	requireNoError(t, err)
	protectedInfo, err := os.Stat(protected)
	requireNoError(t, err)
	if !os.SameFile(aliasInfo, protectedInfo) {
		t.Fatalf("failed add should not alter external hardlink alias")
	}
	requireNoPath(t, filepath.Join(repo, "pkg"))
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
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
	requireErrorContains(t, err, "choose an existing target")

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
	requireErrorContains(t, err, "already has tracked content")
	requireErrorContains(t, err, "choose another source path")

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

func TestAddRejectsSymlinkedTargetParentInsideRepositoryWithoutMutation(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	_, err := InitRepo(repo, env)
	requireNoError(t, err)
	protectedContent := filepath.Join(repo, "loose")
	writeTextFile(t, protectedContent, "protected repository content\n")
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)

	targetParent := filepath.Join(home, ".config", "repo-alias")
	requireNoError(t, os.Symlink(repo, targetParent))
	target := filepath.Join(targetParent, "loose")

	_, err = NewService(repo, env).Add(target, "pkg")
	requireErrorContains(t, err, "dangerous Target Path")
	requireFileContent(t, protectedContent, "protected repository content\n")
	assertSymlink(t, targetParent, repo)
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
	requireNoPath(t, filepath.Join(repo, "pkg"))
}

func TestAddDryRunRejectsSymlinkedTargetParentWithoutPlanningReferentAdoption(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	_, err := InitRepo(repo, env)
	requireNoError(t, err)
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)
	externalParent := filepath.Join(filepath.Dir(home), "external-config")
	requireNoError(t, os.MkdirAll(externalParent, 0o755))
	writeTextFile(t, filepath.Join(externalParent, "config"), "external config\n")
	targetParent := filepath.Join(home, ".config")
	requireNoError(t, os.RemoveAll(targetParent))
	requireNoError(t, os.Symlink(externalParent, targetParent))
	target := filepath.Join(targetParent, "config")

	_, err = NewService(repo, env).AddWithOptions(AddOptions{
		Target:  target,
		Package: "app",
		DryRun:  true,
	})
	requireErrorContains(t, err, "Target Path")
	assertSymlink(t, targetParent, externalParent)
	requireFileContent(t, filepath.Join(externalParent, "config"), "external config\n")
	requireNoPath(t, filepath.Join(repo, "app"))
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
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

func TestLinkRefusesUnsupportedSpecialFilePackageSource(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.pipes]
links = [
  { source = "app.pipe", target = "~/.config/app.pipe" },
]
`)
	source := filepath.Join(repo, "pipes", "app.pipe")
	requireNoError(t, os.MkdirAll(filepath.Dir(source), 0o755))
	requireNoError(t, syscall.Mkfifo(source, 0o600))

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"pipes"}})
	requireErrorContains(t, err, "unsupported file type")

	requireNoPath(t, filepath.Join(home, ".config", "app.pipe"))
	info, err := os.Lstat(source)
	requireNoError(t, err)
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("repo FIFO Package Source should remain unchanged, mode=%v", info.Mode())
	}
}

func TestLinkRefusesDirectorySourceWithNestedSymlinkResolvingToUnsupportedSpecialFile(
	t *testing.T,
) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.app]
links = [
  { source = "config", target = "~/.config/app" },
]
`)
	realSource := filepath.Join(repo, "app", "pipes", "real.pipe")
	requireNoError(t, os.MkdirAll(filepath.Dir(realSource), 0o755))
	requireNoError(t, syscall.Mkfifo(realSource, 0o600))
	sourceDir := filepath.Join(repo, "app", "config")
	requireNoError(t, os.MkdirAll(sourceDir, 0o755))
	nestedLink := filepath.Join(sourceDir, "current.pipe")
	requireNoError(t, os.Symlink("../pipes/real.pipe", nestedLink))

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"app"}})
	requireErrorContains(t, err, "unsupported file type")

	requireNoPath(t, filepath.Join(home, ".config", "app"))
	assertSymlink(t, nestedLink, "../pipes/real.pipe")
	info, err := os.Lstat(realSource)
	requireNoError(t, err)
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("repo FIFO referent should remain unchanged, mode=%v", info.Mode())
	}
}

func TestLinkRefusesDirectorySourceWithNestedSymlinkResolvingToExternalHardlink(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.app]
links = [
  { source = "config", target = "~/.config/app" },
]
`)
	protected := filepath.Join(home, "protected-token")
	writeTextFile(t, protected, "token=keep\n")
	realSource := filepath.Join(repo, "app", "shared", "token")
	requireNoError(t, os.MkdirAll(filepath.Dir(realSource), 0o755))
	if err := os.Link(protected, realSource); err != nil {
		t.Skipf("hardlinks are not supported in test filesystem: %v", err)
	}
	sourceDir := filepath.Join(repo, "app", "config")
	requireNoError(t, os.MkdirAll(sourceDir, 0o755))
	nestedLink := filepath.Join(sourceDir, "current-token")
	requireNoError(t, os.Symlink("../shared/token", nestedLink))

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"app"}})
	requireErrorContains(t, err, "external hardlink")

	requireNoPath(t, filepath.Join(home, ".config", "app"))
	assertSymlink(t, nestedLink, "../shared/token")
	requireFileContent(t, protected, "token=keep\n")
	realInfo, err := os.Stat(realSource)
	requireNoError(t, err)
	protectedInfo, err := os.Stat(protected)
	requireNoError(t, err)
	if !os.SameFile(realInfo, protectedInfo) {
		t.Fatalf("failed link should not alter the repo hardlink referent")
	}
}

func TestLinkRefusesSourceSymlinkResolvingToUnsupportedSpecialFile(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.pipes]
links = [
  { source = "current.pipe", target = "~/.config/app.pipe" },
]
`)
	realSource := filepath.Join(repo, "pipes", "real.pipe")
	requireNoError(t, os.MkdirAll(filepath.Dir(realSource), 0o755))
	requireNoError(t, syscall.Mkfifo(realSource, 0o600))
	sourceLink := filepath.Join(repo, "pipes", "current.pipe")
	requireNoError(t, os.Symlink("real.pipe", sourceLink))

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"pipes"}})
	requireErrorContains(t, err, "unsupported file type")

	requireNoPath(t, filepath.Join(home, ".config", "app.pipe"))
	assertSymlink(t, sourceLink, "real.pipe")
	info, err := os.Lstat(realSource)
	requireNoError(t, err)
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("repo FIFO referent should remain unchanged, mode=%v", info.Mode())
	}
}

func TestLinkRefusesPackageSourceHardlinkedToExternalFile(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, manifestWithSingleZshrcLink)
	protected := filepath.Join(home, "protected-zshrc")
	writeTextFile(t, protected, "export TOKEN=keep\n")
	source := filepath.Join(repo, "zsh", ".zshrc")
	requireNoError(t, os.MkdirAll(filepath.Dir(source), 0o755))
	if err := os.Link(protected, source); err != nil {
		t.Skipf("hardlinks are not supported in test filesystem: %v", err)
	}

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"zsh"}})
	requireErrorContains(t, err, "external hardlink")

	requireNoPath(t, filepath.Join(home, ".zshrc"))
	requireFileContent(t, protected, "export TOKEN=keep\n")
	sourceInfo, err := os.Stat(source)
	requireNoError(t, err)
	protectedInfo, err := os.Stat(protected)
	requireNoError(t, err)
	if !os.SameFile(sourceInfo, protectedInfo) {
		t.Fatalf("failed link should not alter the repo hardlink source")
	}
}

func TestLinkRefusesSourceSymlinkResolvingToExternalHardlink(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = "current-zshrc", target = "~/.zshrc" },
]
`)
	protected := filepath.Join(home, "protected-zshrc")
	writeTextFile(t, protected, "export TOKEN=keep\n")
	realSource := filepath.Join(repo, "zsh", "real-zshrc")
	requireNoError(t, os.MkdirAll(filepath.Dir(realSource), 0o755))
	if err := os.Link(protected, realSource); err != nil {
		t.Skipf("hardlinks are not supported in test filesystem: %v", err)
	}
	sourceLink := filepath.Join(repo, "zsh", "current-zshrc")
	requireNoError(t, os.Symlink("real-zshrc", sourceLink))

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"zsh"}})
	requireErrorContains(t, err, "external hardlink")

	requireNoPath(t, filepath.Join(home, ".zshrc"))
	assertSymlink(t, sourceLink, "real-zshrc")
	requireFileContent(t, protected, "export TOKEN=keep\n")
	realInfo, err := os.Stat(realSource)
	requireNoError(t, err)
	protectedInfo, err := os.Stat(protected)
	requireNoError(t, err)
	if !os.SameFile(realInfo, protectedInfo) {
		t.Fatalf("failed link should not alter the repo hardlink referent")
	}
}

func TestLinkReportsDanglingPackageSourceSymlinkAsMissingSource(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.ghost]
links = [
  { source = "config", target = "~/.config/ghost" },
]
`)
	packageRoot := filepath.Join(repo, "ghost")
	requireNoError(t, os.MkdirAll(packageRoot, 0o755))
	requireNoError(t, os.Symlink("missing-config", filepath.Join(packageRoot, "config")))

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"ghost"}})
	requireErrorContains(t, err, "ghost/config is missing from the repository")
	requireNoPath(t, filepath.Join(home, ".config", "ghost"))
}

func TestLinkRejectsPackageSourceSymlinkEscapingPackageRoot(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.ghost]
links = [
  { source = "config", target = "~/.config/ghost" },
]
`)
	external := filepath.Join(home, "external", "ghost-config")
	writeTextFile(t, external, "external config\n")
	packageRoot := filepath.Join(repo, "ghost")
	requireNoError(t, os.MkdirAll(packageRoot, 0o755))
	requireNoError(t, os.Symlink(external, filepath.Join(packageRoot, "config")))

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"ghost"}})
	requireErrorContains(t, err, "escapes package")
	requireNoPath(t, filepath.Join(home, ".config", "ghost"))
}

func TestLinkRefusesTargetConflictUnlessForced(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, manifestWithSingleZshrcLink)
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

func TestLinkRejectsAbsoluteTargetWithSymlinkedParent(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.app]
links = [
  { source = "config", target = "PLACEHOLDER" },
]
`)
	source := filepath.Join(repo, "app", "config")
	writeTextFile(t, source, "managed config\n")
	externalParent := filepath.Join(filepath.Dir(home), "external-absolute-target")
	targetParent := filepath.Join(filepath.Dir(home), "absolute-target-parent")
	requireNoError(t, os.MkdirAll(externalParent, 0o755))
	requireNoError(t, os.Symlink(externalParent, targetParent))
	target := filepath.Join(targetParent, "config")
	writeDottyManifest(t, repo, `version = 1

[packages.app]
links = [
  { source = "config", target = "`+filepath.ToSlash(target)+`" },
]
`)

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"app"}})
	requireErrorContains(t, err, "Target Path")
	assertSymlink(t, targetParent, externalParent)
	requireNoPath(t, filepath.Join(externalParent, "config"))
	requireFileContent(t, source, "managed config\n")
}

func TestLinkRejectsAbsoluteTargetWithSymlinkedAncestor(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.app]
links = [
  { source = "config", target = "PLACEHOLDER" },
]
`)
	source := filepath.Join(repo, "app", "config")
	writeTextFile(t, source, "managed config\n")
	externalRoot := filepath.Join(filepath.Dir(home), "external-absolute-root")
	targetRoot := filepath.Join(filepath.Dir(home), "absolute-target-root")
	referentTargetParent := filepath.Join(externalRoot, "nested")
	requireNoError(t, os.MkdirAll(referentTargetParent, 0o755))
	requireNoError(t, os.Symlink(externalRoot, targetRoot))
	target := filepath.Join(targetRoot, "nested", "config")
	writeDottyManifest(t, repo, `version = 1

[packages.app]
links = [
  { source = "config", target = "`+filepath.ToSlash(target)+`" },
]
`)

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"app"}})
	requireErrorContains(t, err, "Target Path")
	assertSymlink(t, targetRoot, externalRoot)
	requireNoPath(t, filepath.Join(referentTargetParent, "config"))
	requireFileContent(t, source, "managed config\n")
}

func TestLinkDryRunRejectsSymlinkedTargetParent(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.app]
links = [
  { source = "config", target = "~/.config/config" },
]
`)
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "app"), 0o755))
	writeTextFile(t, filepath.Join(repo, "app", "config"), "managed config\n")
	externalParent := filepath.Join(home, "external-config")
	requireNoError(t, os.MkdirAll(externalParent, 0o755))
	requireNoError(t, os.RemoveAll(filepath.Join(home, ".config")))
	requireNoError(t, os.Symlink(externalParent, filepath.Join(home, ".config")))

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"app"}, DryRun: true})
	requireErrorContains(t, err, "Target Path")
	assertSymlink(t, filepath.Join(home, ".config"), externalParent)
	requireNoPath(t, filepath.Join(externalParent, "config"))
	requireFileContent(t, filepath.Join(repo, "app", "config"), "managed config\n")
}

func TestLinkRejectsSymlinkedTargetParentInsideRepositoryEvenWhenForced(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "pkg"), 0o755))
	writeTextFile(t, filepath.Join(repo, "pkg", "config"), "enabled = true\n")
	targetParent := filepath.Join(home, ".config", "repo-alias")
	requireNoError(t, os.Symlink(repo, targetParent))
	target := filepath.Join(targetParent, "target")
	writeDottyManifest(t, repo, dangerousTargetManifest(target))

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"pkg"}, Force: true})
	requireErrorContains(t, err, "dangerous Target Path")
	assertSymlink(t, targetParent, repo)
	requireNoPath(t, filepath.Join(repo, "target"))
	requireFileContent(t, ManifestPath(repo), dangerousTargetManifest(target))
}

func TestLinkAllowsTargetWithRepositorySiblingPrefix(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	target := filepath.Join(home, "dotfiles-sibling", "tmux")
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "pkg"), 0o755))
	writeDottyManifest(t, repo, dangerousTargetManifest(target))

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"pkg"}})
	requireNoError(t, err)
	assertSymlink(t, target, filepath.Join(repo, "pkg"))
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
	home, repo, env := setupLinkedPackageTest(t, manifestWithSingleZshrcLink)
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

func TestLinkDryRunReportsPlannedActionDetails(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.config]
links = [
  { source = "absent", target = "~/.absent" },
  { source = "linked", target = "~/.linked" },
  { source = "relative", target = "~/.relative" },
  { source = "conflict", target = "~/.conflict" },
  { source = "wrong", target = "~/.wrong" },
]
`)
	for _, sourceName := range []string{"absent", "linked", "relative", "conflict", "wrong"} {
		writeTextFile(t, filepath.Join(repo, "config", sourceName), sourceName+"\n")
	}
	requireNoError(
		t,
		os.Symlink(filepath.Join(repo, "config", "linked"), filepath.Join(home, ".linked")),
	)
	relativeTarget := filepath.Join(home, ".relative")
	rel, err := filepath.Rel(
		filepath.Dir(relativeTarget),
		filepath.Join(repo, "config", "relative"),
	)
	requireNoError(t, err)
	requireNoError(t, os.Symlink(rel, relativeTarget))
	writeTextFile(t, filepath.Join(home, ".conflict"), "local copy\n")
	wrongSource := filepath.Join(home, "wrong-source")
	writeTextFile(t, wrongSource, "wrong\n")
	requireNoError(t, os.Symlink(wrongSource, filepath.Join(home, ".wrong")))

	results, err := NewService(repo, env).Link(LinkOptions{
		Packages: []string{"config"},
		Force:    true,
		DryRun:   true,
	})
	requireNoError(t, err)

	if got, want := linkResultActions(results), []string{
		"create",
		"noop",
		"normalize",
		"replace-conflict",
		"replace-conflict",
	}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected link dry-run actions\nwant: %#v\ngot:  %#v", want, got)
	}
	requireNoPath(t, filepath.Join(home, ".absent"))
	assertSymlink(t, filepath.Join(home, ".linked"), filepath.Join(repo, "config", "linked"))
	assertSymlink(t, relativeTarget, rel)
	requireFileContent(t, filepath.Join(home, ".conflict"), "local copy\n")
	assertSymlink(t, filepath.Join(home, ".wrong"), wrongSource)
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
	home, repo, env := setupLinkedPackageTest(t, manifestWithSingleZshrcLink)
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
	home, repo, env := setupLinkedPackageTest(t, manifestWithSingleZshrcLink)
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
	assertSymlink(t, target, rel)

	svc := NewService(repo, env)
	report, err := svc.Status([]string{"tmux"})
	requireNoError(t, err)
	if len(report.Packages) != 1 || report.Packages[0].State != StateLinked {
		t.Fatalf(
			"relative expected symlink should be reported LINKED before normalization, got %#v",
			report.Packages,
		)
	}
	if len(report.Packages[0].Entries) != 1 || report.Packages[0].Entries[0].State != StateLinked {
		t.Fatalf(
			"relative expected symlink entry should be reported LINKED before normalization, got %#v",
			report.Packages[0].Entries,
		)
	}

	_, err = svc.Link(LinkOptions{Packages: []string{"tmux"}})
	requireNoError(t, err)
	assertSymlink(t, target, source)
	assertTmuxPackageState(t, svc, StateLinked)

	_, err = svc.Link(LinkOptions{Packages: []string{"tmux"}})
	requireNoError(t, err)
	assertSymlink(t, target, source)
	assertTmuxPackageState(t, svc, StateLinked)
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
	requireErrorContains(t, err, "zsh/.missing is missing from the repository")
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
	requireErrorContains(t, err, "zsh/.missing is missing from the repository")
	requireFileContent(t, target, "local copy\n")
	requireNoDottyBackups(t, home)
}

func TestLinkForceReportsMoveAsideFailureWithoutReplacingConflict(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, manifestWithSingleZshrcLink)
	writeTextFile(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=vim\n")
	target := filepath.Join(home, ".zshrc")
	writeTextFile(t, target, "local copy\n")
	forceRenameError(t, syscall.EPERM)

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"zsh"}, Force: true})
	requireErrorContains(t, err, "move "+target+" aside")
	requireFileContent(t, target, "local copy\n")
	requireNoDottyBackups(t, home)
}

func TestLinkForceReportsCleanupFailureAfterReplacement(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, manifestWithSingleZshrcLink)
	source := filepath.Join(repo, "zsh", ".zshrc")
	writeTextFile(t, source, "export EDITOR=vim\n")
	target := filepath.Join(home, ".zshrc")
	writeTextFile(t, target, "local copy\n")
	forceRemoveAllPathError(t, errors.New("cleanup backup failed"))

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"zsh"}, Force: true})
	requireErrorContains(t, err, "cleanup backup failed")
	assertSymlink(t, target, source)
	requireFileContent(t, source, "export EDITOR=vim\n")
	requireDottyBackupContent(t, home, "local copy\n")
}

func TestLinkForceRollsBackEarlierReplacementWhenLaterMappingFailsDuringExecution(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
  { source = "tool", target = "~/.local/bin/tool" },
]
`)
	writeTextFile(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=vim\n")
	writeTextFile(t, filepath.Join(repo, "zsh", "tool"), "#!/bin/sh\n")
	earlierTarget := filepath.Join(home, ".zshrc")
	writeTextFile(t, earlierTarget, "local copy\n")
	blockedParent := filepath.Join(home, ".local")
	writeTextFile(t, blockedParent, "not a directory\n")

	_, err := NewService(repo, env).Link(LinkOptions{Packages: []string{"zsh"}, Force: true})
	requireErrorContains(t, err, "not a directory")

	requireFileContent(t, earlierTarget, "local copy\n")
	requireFileContent(t, blockedParent, "not a directory\n")
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

func TestLinkRejectsSelectedCompetingMappings(t *testing.T) {
	_, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.tmux-macos]
links = [
  { source = ".", target = "~/.config/tmux" },
]

[packages.tmux-linux]
links = [
  { source = ".", target = "~/.config/tmux" },
]

[collections.tmux]
packages = ["tmux-macos", "tmux-linux"]
`)
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "tmux-macos"), 0o755))
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "tmux-linux"), 0o755))
	svc := NewService(repo, env)

	tests := []struct {
		name    string
		options LinkOptions
	}{
		{
			name:    "explicit packages",
			options: LinkOptions{Packages: []string{"tmux-macos", "tmux-linux"}, DryRun: true},
		},
		{
			name:    "all packages",
			options: LinkOptions{All: true, DryRun: true},
		},
		{
			name:    "collection",
			options: LinkOptions{Collections: []string{"tmux"}, DryRun: true},
		},
		{
			name: "force still rejected",
			options: LinkOptions{
				Packages: []string{"tmux-macos", "tmux-linux"},
				Force:    true,
				DryRun:   true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Link(tt.options)
			requireErrorContains(t, err, "selected packages compete")
		})
	}
}

func TestLinkAlternativeBlockedByManagedSourceRequiresForceAndReportsBlocked(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

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
	svc := NewService(repo, env)

	_, err := svc.Link(LinkOptions{Packages: []string{"tmux-macos"}})
	requireNoError(t, err)
	assertSymlink(t, target, macosSource)

	_, err = svc.Link(LinkOptions{Packages: []string{"tmux-linux"}})
	requireErrorContains(t, err, "linked by tmux-macos")
	assertSymlink(t, target, macosSource)

	_, err = svc.Link(LinkOptions{Packages: []string{"tmux-linux"}, Force: true})
	requireNoError(t, err)
	assertSymlink(t, target, linuxSource)

	report, err := svc.Status([]string{"tmux-macos", "tmux-linux"})
	requireNoError(t, err)
	if report.Packages[0].Name != "tmux-macos" || report.Packages[0].State != StateBlocked {
		t.Fatalf("expected tmux-macos to be BLOCKED, got %#v", report.Packages[0])
	}
	if report.Packages[1].Name != "tmux-linux" || report.Packages[1].State != StateLinked {
		t.Fatalf("expected tmux-linux to be LINKED, got %#v", report.Packages[1])
	}
}

func TestLinkTrackCreatesMappingAndLinksSelectedSource(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, "version = 1\n")
	writeTextFile(t, filepath.Join(repo, "scripts", "docx2pdf"), "#!/bin/sh\n")

	results, err := NewService(repo, env).Link(LinkOptions{
		Selectors: []Selector{mustParseSelector(t, "scripts/docx2pdf")},
		Targets:   []string{"~/.local/bin/docx2pdf"},
		Track:     true,
	})

	requireNoError(t, err)
	if len(results) != 1 || results[0].Package != "scripts" ||
		results[0].Target != "~/.local/bin/docx2pdf" {
		t.Fatalf("unexpected link --track results: %#v", results)
	}
	assertSymlink(
		t,
		filepath.Join(home, ".local", "bin", "docx2pdf"),
		filepath.Join(repo, "scripts", "docx2pdf"),
	)
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]
`)
}

func TestLinkTrackPackageSelectorCreatesRootMappingAndLink(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, "version = 1\n")
	writeTextFile(t, filepath.Join(repo, "tmux", "tmux.conf"), "set -g mouse on\n")

	_, err := NewService(repo, env).Link(LinkOptions{
		Selectors: []Selector{mustParseSelector(t, "tmux")},
		Targets:   []string{"~/.config/tmux"},
		Track:     true,
	})

	requireNoError(t, err)
	assertSymlink(t, filepath.Join(home, ".config", "tmux"), filepath.Join(repo, "tmux"))
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]
`)
}

func TestLinkTrackRejectsSymlinkedTargetParentAndLeavesManifestUnchanged(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	manifest := "version = 1\n"
	writeDottyManifest(t, repo, manifest)
	writeTextFile(t, filepath.Join(repo, "app", "config"), "managed config\n")

	externalConfig := filepath.Join(filepath.Dir(home), "external-config")
	requireNoError(t, os.MkdirAll(externalConfig, 0o755))
	configLink := filepath.Join(home, ".config")
	requireNoError(t, os.Remove(configLink))
	requireNoError(t, os.Symlink(externalConfig, configLink))

	_, err := NewService(repo, env).Link(LinkOptions{
		Selectors: []Selector{mustParseSelector(t, "app/config")},
		Targets:   []string{"~/.config/config"},
		Track:     true,
	})

	requireErrorContains(t, err, "symlinked parent")
	requireFileContent(t, ManifestPath(repo), manifest)
	assertSymlink(t, configLink, externalConfig)
	requireNoPath(t, filepath.Join(externalConfig, "config"))
}

func TestLinkTrackRejectsInvalidSelectionCombinations(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, "version = 1\n")
	writeTextFile(t, filepath.Join(repo, "scripts", "docx2pdf"), "#!/bin/sh\n")

	tests := []struct {
		name    string
		options LinkOptions
		wantErr string
	}{
		{
			name: "without target",
			options: LinkOptions{
				Selectors: []Selector{mustParseSelector(t, "scripts/docx2pdf")},
				Track:     true,
			},
			wantErr: "--track requires --target",
		},
		{
			name: "multiple selectors",
			options: LinkOptions{
				Selectors: []Selector{
					mustParseSelector(t, "scripts/docx2pdf"),
					mustParseSelector(t, "scripts/other"),
				},
				Targets: []string{"~/.local/bin/docx2pdf"},
				Track:   true,
			},
			wantErr: "--track accepts exactly one selector",
		},
		{
			name: "all",
			options: LinkOptions{
				All:     true,
				Targets: []string{"~/.local/bin/docx2pdf"},
				Track:   true,
			},
			wantErr: "--track cannot be combined with --all",
		},
		{
			name: "collection",
			options: LinkOptions{
				Collections: []string{"tools"},
				Targets:     []string{"~/.local/bin/docx2pdf"},
				Track:       true,
			},
			wantErr: "--track cannot be combined with --collection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewService(repo, env).Link(tt.options)
			requireErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestLinkTrackIsAtomicAndForceCanReplaceConflicts(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	manifest := "version = 1\n"
	writeDottyManifest(t, repo, manifest)
	writeTextFile(t, filepath.Join(repo, "scripts", "docx2pdf"), "#!/bin/sh\n")
	target := filepath.Join(home, ".local", "bin", "docx2pdf")
	writeTextFile(t, target, "local conflict\n")

	_, err := NewService(repo, env).Link(LinkOptions{
		Selectors: []Selector{mustParseSelector(t, "scripts/docx2pdf")},
		Targets:   []string{"~/.local/bin/docx2pdf"},
		Track:     true,
	})
	requireErrorContains(t, err, "already exists")
	requireFileContent(t, ManifestPath(repo), manifest)
	requireFileContent(t, target, "local conflict\n")

	_, err = NewService(repo, env).Link(LinkOptions{
		Selectors: []Selector{mustParseSelector(t, "scripts/docx2pdf")},
		Targets:   []string{"~/.local/bin/docx2pdf"},
		Track:     true,
		Force:     true,
	})
	requireNoError(t, err)
	assertSymlink(t, target, filepath.Join(repo, "scripts", "docx2pdf"))
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]
`)
}

func TestLinkTrackOnlyLinksExplicitTargetsNotExistingMappings(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.scripts]
links = [
  { source = "old", target = "~/.local/bin/old" },
]
`)
	writeTextFile(t, filepath.Join(repo, "scripts", "old"), "old\n")
	writeTextFile(t, filepath.Join(repo, "scripts", "new"), "new\n")

	_, err := NewService(repo, env).Link(LinkOptions{
		Selectors: []Selector{mustParseSelector(t, "scripts/new")},
		Targets:   []string{"~/.local/bin/new"},
		Track:     true,
	})

	requireNoError(t, err)
	requireNoPath(t, filepath.Join(home, ".local", "bin", "old"))
	assertSymlink(
		t,
		filepath.Join(home, ".local", "bin", "new"),
		filepath.Join(repo, "scripts", "new"),
	)
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.scripts]
links = [
  { source = "old", target = "~/.local/bin/old" },
  { source = "new", target = "~/.local/bin/new" },
]
`)
}

func TestLinkTargetsOnlySelectedLinkMappings(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
  { source = "sesh-fzf", target = "~/.local/bin/sesh-fzf" },
]
`)
	writeTextFile(t, filepath.Join(repo, "scripts", "docx2pdf"), "docx2pdf\n")
	writeTextFile(t, filepath.Join(repo, "scripts", "sesh-fzf"), "sesh-fzf\n")

	results, err := NewService(repo, env).Link(LinkOptions{
		Packages: []string{"scripts"},
		Targets:  []string{"~/.local/bin/sesh-fzf"},
	})
	requireNoError(t, err)
	if len(results) != 1 || results[0].Target != "~/.local/bin/sesh-fzf" {
		t.Fatalf("unexpected partial link results: %#v", results)
	}

	requireNoPath(t, filepath.Join(home, ".local", "bin", "docx2pdf"))
	assertSymlink(
		t,
		filepath.Join(home, ".local", "bin", "sesh-fzf"),
		filepath.Join(repo, "scripts", "sesh-fzf"),
	)
}

func TestLinkTargetSelectionFailsBeforeChangingFilesystem(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
  { source = "sesh-fzf", target = "~/.local/bin/sesh-fzf" },
]
`)
	writeTextFile(t, filepath.Join(repo, "scripts", "docx2pdf"), "docx2pdf\n")
	writeTextFile(t, filepath.Join(repo, "scripts", "sesh-fzf"), "sesh-fzf\n")

	_, err := NewService(repo, env).Link(LinkOptions{
		Packages: []string{"scripts"},
		Targets:  []string{"~/.local/bin/docx2pdf", "~/.missing-target"},
	})
	requireErrorContains(t, err, "target \"~/.missing-target\" is not mapped")
	requireNoPath(t, filepath.Join(home, ".local", "bin", "docx2pdf"))
	requireNoPath(t, filepath.Join(home, ".local", "bin", "sesh-fzf"))
}

func TestLinkTargetSelectionRejectsCollectionsAndAll(t *testing.T) {
	_, _, env := setupLinkedPackageTest(t, `version = 1

[packages.scripts]
links = [
  { source = "sesh-fzf", target = "~/.local/bin/sesh-fzf" },
]

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]

[collections.terminal]
packages = ["tmux", "scripts"]
`)
	manifest := manifestForLinkMappingSelection()
	svc := NewService("/tmp/dotfiles", env)

	_, err := svc.resolveLinkSelections(manifest, LinkOptions{
		Collections: []string{"terminal"},
		Targets:     []string{"~/.local/bin/sesh-fzf"},
	})
	requireErrorContains(t, err, "--target can only be used with one selector")

	_, err = svc.resolveLinkSelections(manifest, LinkOptions{
		All:     true,
		Targets: []string{"~/.config/tmux"},
	})
	requireErrorContains(t, err, "--target can only be used with one selector")
}

func TestUnlinkUntrackSourceRemovesLinkAndMapping(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
  { source = "sesh-fzf", target = "~/.local/bin/sesh-fzf" },
]
`)
	docxSource := filepath.Join(repo, "scripts", "docx2pdf")
	writeTextFile(t, docxSource, "docx2pdf\n")
	writeTextFile(t, filepath.Join(repo, "scripts", "sesh-fzf"), "sesh\n")
	docxTarget := filepath.Join(home, ".local", "bin", "docx2pdf")
	requireNoError(t, os.MkdirAll(filepath.Dir(docxTarget), 0o755))
	requireNoError(t, os.Symlink(docxSource, docxTarget))

	_, err := NewService(repo, env).Unlink(UnlinkOptions{
		Selectors: []Selector{mustParseSelector(t, "scripts/docx2pdf")},
		Untrack:   true,
	})

	requireNoError(t, err)
	requireNoPath(t, docxTarget)
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.scripts]
links = [
  { source = "sesh-fzf", target = "~/.local/bin/sesh-fzf" },
]
`)
}

func TestUnlinkUntrackPackageSelectorRemovesAllMappingsAndLeavesEmptyPackage(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]
`)
	source := filepath.Join(repo, "scripts", "docx2pdf")
	writeTextFile(t, source, "docx2pdf\n")
	target := filepath.Join(home, ".local", "bin", "docx2pdf")
	requireNoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	requireNoError(t, os.Symlink(source, target))

	_, err := NewService(repo, env).Unlink(UnlinkOptions{
		Selectors: []Selector{mustParseSelector(t, "scripts")},
		Untrack:   true,
	})

	requireNoError(t, err)
	requireNoPath(t, target)
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.scripts]
links = []
`)
}

func TestUnlinkUntrackOnlySelectedTarget(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
  { source = "docx2pdf", target = "~/bin/docx2pdf" },
]
`)
	source := filepath.Join(repo, "scripts", "docx2pdf")
	writeTextFile(t, source, "docx2pdf\n")
	target := filepath.Join(home, ".local", "bin", "docx2pdf")
	requireNoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	requireNoError(t, os.Symlink(source, target))

	_, err := NewService(repo, env).Unlink(UnlinkOptions{
		Selectors: []Selector{mustParseSelector(t, "scripts/docx2pdf")},
		Targets:   []string{"~/.local/bin/docx2pdf"},
		Untrack:   true,
	})

	requireNoError(t, err)
	requireNoPath(t, target)
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/bin/docx2pdf" },
]
`)
}

func TestUnlinkUntrackRejectsInvalidSelectionCombinations(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, "version = 1\n")

	tests := []struct {
		name    string
		options UnlinkOptions
		wantErr string
	}{
		{
			name: "multiple selectors",
			options: UnlinkOptions{
				Selectors: []Selector{
					mustParseSelector(t, "scripts/a"),
					mustParseSelector(t, "scripts/b"),
				},
				Untrack: true,
			},
			wantErr: "--untrack accepts exactly one selector",
		},
		{
			name:    "all",
			options: UnlinkOptions{All: true, Untrack: true},
			wantErr: "--untrack cannot be combined with --all",
		},
		{
			name:    "collection",
			options: UnlinkOptions{Collections: []string{"tools"}, Untrack: true},
			wantErr: "--untrack cannot be combined with --collection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewService(repo, env).Unlink(tt.options)
			requireErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestUnlinkUntrackRemovesMappingWhenTargetAbsentOrNotDottyLink(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.scripts]
links = [
  { source = "absent", target = "~/.local/bin/absent" },
  { source = "conflict", target = "~/.local/bin/conflict" },
]
`)
	writeTextFile(t, filepath.Join(repo, "scripts", "absent"), "absent\n")
	writeTextFile(t, filepath.Join(repo, "scripts", "conflict"), "conflict\n")
	conflictTarget := filepath.Join(home, ".local", "bin", "conflict")
	writeTextFile(t, conflictTarget, "local conflict\n")

	results, err := NewService(repo, env).Unlink(UnlinkOptions{
		Selectors: []Selector{mustParseSelector(t, "scripts/absent")},
		Untrack:   true,
	})
	requireNoError(t, err)
	if len(results) != 1 || results[0].Action != UnlinkResultActionNoop {
		t.Fatalf("unexpected absent untrack results: %#v", results)
	}

	_, err = NewService(repo, env).Unlink(UnlinkOptions{
		Selectors: []Selector{mustParseSelector(t, "scripts/conflict")},
		Untrack:   true,
	})
	requireNoError(t, err)
	requireFileContent(t, conflictTarget, "local conflict\n")
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.scripts]
links = []
`)
}

func TestLeaveCopyUnlinkUntrackEjectsMappingAndLeavesCopy(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, manifestWithSingleZshrcLink)
	source := filepath.Join(repo, "zsh", ".zshrc")
	writeTextFile(t, source, "export EDITOR=vim\n")
	target := filepath.Join(home, ".zshrc")
	requireNoError(t, os.Symlink(source, target))

	_, err := NewService(repo, env).Unlink(UnlinkOptions{
		Selectors: []Selector{mustParseSelector(t, "zsh/.zshrc")},
		LeaveCopy: true,
		Untrack:   true,
	})

	requireNoError(t, err)
	requireFileContent(t, target, "export EDITOR=vim\n")
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.zsh]
links = []
`)
}

func TestUnlinkHandlesAbsentTargetsAndDefaultUnlinkWithoutSource(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, manifestWithSingleZshrcLink)
	svc := NewService(repo, env)

	results, err := svc.Unlink(UnlinkOptions{Packages: []string{"zsh"}})
	requireNoError(t, err)
	if len(results) != 1 || results[0].Target != "~/.zshrc" ||
		results[0].Action != UnlinkResultActionNoop {
		t.Fatalf("unexpected unlink results: %#v", results)
	}
	requireNoPath(t, filepath.Join(home, ".zshrc"))

	target := filepath.Join(home, ".zshrc")
	expectedSource := filepath.Join(repo, "zsh", ".zshrc")
	requireNoError(t, os.Symlink(expectedSource, target))
	_, err = svc.Unlink(UnlinkOptions{Packages: []string{"zsh"}})
	requireNoError(t, err)
	requireNoPath(t, target)
}

func TestLeaveCopyUnlinkCopiesSourceWhenTargetIsAbsent(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, manifestWithSingleZshrcLink)
	source := filepath.Join(repo, "zsh", ".zshrc")
	writeTextFile(t, source, "export EDITOR=vim\n")
	target := filepath.Join(home, ".zshrc")

	results, err := NewService(repo, env).Unlink(UnlinkOptions{
		Packages:  []string{"zsh"},
		LeaveCopy: true,
	})

	requireNoError(t, err)
	if len(results) != 1 || results[0].Action != UnlinkResultActionCopySource ||
		!results[0].LeaveCopy {
		t.Fatalf("unexpected leave-copy unlink results: %#v", results)
	}
	requireFileContent(t, target, "export EDITOR=vim\n")
}

func TestUnlinkDryRunLeavesLeaveCopyAndDefaultTargetsUnchanged(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, manifestWithSingleZshrcLink)
	source := filepath.Join(repo, "zsh", ".zshrc")
	writeTextFile(t, source, "export EDITOR=vim\n")
	target := filepath.Join(home, ".zshrc")
	requireNoError(t, os.Symlink(source, target))
	svc := NewService(repo, env)

	results, err := svc.Unlink(
		UnlinkOptions{Packages: []string{"zsh"}, LeaveCopy: true, DryRun: true},
	)
	requireNoError(t, err)
	if len(results) != 1 || !results[0].DryRun || !results[0].LeaveCopy {
		t.Fatalf("unexpected leave-copy unlink dry-run results: %#v", results)
	}
	assertSymlink(t, target, source)

	results, err = svc.Unlink(UnlinkOptions{Packages: []string{"zsh"}, DryRun: true})
	requireNoError(t, err)
	if len(results) != 1 || !results[0].DryRun || results[0].LeaveCopy {
		t.Fatalf("unexpected default unlink dry-run results: %#v", results)
	}
	assertSymlink(t, target, source)
}

func TestUnlinkDryRunReportsPlannedActionDetails(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.config]
links = [
  { source = "linked", target = "~/.linked" },
  { source = "absent", target = "~/.absent" },
]
`)
	linkedSource := filepath.Join(repo, "config", "linked")
	writeTextFile(t, linkedSource, "linked\n")
	writeTextFile(t, filepath.Join(repo, "config", "absent"), "absent\n")
	linkedTarget := filepath.Join(home, ".linked")
	requireNoError(t, os.Symlink(linkedSource, linkedTarget))

	leaveCopyResults, err := NewService(repo, env).Unlink(UnlinkOptions{
		Packages:  []string{"config"},
		LeaveCopy: true,
		DryRun:    true,
	})
	requireNoError(t, err)
	if got, want := unlinkResultActions(
		leaveCopyResults,
	), []string{
		"copy-source",
		"copy-source",
	}; strings.Join(
		got,
		"\n",
	) != strings.Join(
		want,
		"\n",
	) {
		t.Fatalf("unexpected leave-copy unlink dry-run actions\nwant: %#v\ngot:  %#v", want, got)
	}

	defaultResults, err := NewService(repo, env).Unlink(UnlinkOptions{
		Packages: []string{"config"},
		DryRun:   true,
	})
	requireNoError(t, err)
	if got, want := unlinkResultActions(
		defaultResults,
	), []string{
		"remove-link",
		"noop",
	}; strings.Join(
		got,
		"\n",
	) != strings.Join(
		want,
		"\n",
	) {
		t.Fatalf("unexpected default unlink dry-run actions\nwant: %#v\ngot:  %#v", want, got)
	}
	assertSymlink(t, linkedTarget, linkedSource)
	requireNoPath(t, filepath.Join(home, ".absent"))
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

	_, err := NewService(
		repo,
		env,
	).Unlink(UnlinkOptions{Packages: []string{"zsh"}, LeaveCopy: true, DryRun: true})
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

	results, err := NewService(repo, env).Unlink(UnlinkOptions{All: true})
	requireNoError(t, err)
	requireUnlinkResultPackages(t, results, []string{"tmux", "zsh"})
	requireNoPath(t, filepath.Join(home, ".config", "tmux"))
	requireNoPath(t, filepath.Join(home, ".zshrc"))
}

func TestUnlinkTargetsOnlySelectedLinkMappings(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
  { source = "sesh-fzf", target = "~/.local/bin/sesh-fzf" },
]
`)
	docxSource := filepath.Join(repo, "scripts", "docx2pdf")
	seshSource := filepath.Join(repo, "scripts", "sesh-fzf")
	writeTextFile(t, docxSource, "docx2pdf\n")
	writeTextFile(t, seshSource, "sesh-fzf\n")
	docxTarget := filepath.Join(home, ".local", "bin", "docx2pdf")
	seshTarget := filepath.Join(home, ".local", "bin", "sesh-fzf")
	requireNoError(t, os.MkdirAll(filepath.Dir(docxTarget), 0o755))
	requireNoError(t, os.Symlink(docxSource, docxTarget))
	requireNoError(t, os.Symlink(seshSource, seshTarget))

	results, err := NewService(repo, env).Unlink(UnlinkOptions{
		Packages: []string{"scripts"},
		Targets:  []string{"~/.local/bin/sesh-fzf"},
	})
	requireNoError(t, err)
	if len(results) != 1 || results[0].Target != "~/.local/bin/sesh-fzf" || results[0].LeaveCopy {
		t.Fatalf("unexpected partial unlink results: %#v", results)
	}

	assertSymlink(t, docxTarget, docxSource)
	requireNoPath(t, seshTarget)
}

func TestDefaultUnlinkTargetsOnlySelectedLinkMappings(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
  { source = "sesh-fzf", target = "~/.local/bin/sesh-fzf" },
]
`)
	docxSource := filepath.Join(repo, "scripts", "docx2pdf")
	seshSource := filepath.Join(repo, "scripts", "sesh-fzf")
	writeTextFile(t, docxSource, "docx2pdf\n")
	writeTextFile(t, seshSource, "sesh-fzf\n")
	docxTarget := filepath.Join(home, ".local", "bin", "docx2pdf")
	seshTarget := filepath.Join(home, ".local", "bin", "sesh-fzf")
	requireNoError(t, os.MkdirAll(filepath.Dir(docxTarget), 0o755))
	requireNoError(t, os.Symlink(docxSource, docxTarget))
	requireNoError(t, os.Symlink(seshSource, seshTarget))

	results, err := NewService(repo, env).Unlink(UnlinkOptions{
		Packages: []string{"scripts"},
		Targets:  []string{"~/.local/bin/sesh-fzf"},
	})
	requireNoError(t, err)
	if len(results) != 1 || results[0].Target != "~/.local/bin/sesh-fzf" || results[0].LeaveCopy {
		t.Fatalf("unexpected partial default unlink results: %#v", results)
	}

	assertSymlink(t, docxTarget, docxSource)
	requireNoPath(t, seshTarget)
}

func TestUnlinkRefusesConflictsAndWrongSymlinks(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, manifestWithSingleZshrcLink)
	writeTextFile(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=vim\n")
	svc := NewService(repo, env)
	target := filepath.Join(home, ".zshrc")

	writeTextFile(t, target, "local copy\n")
	_, err := svc.Unlink(UnlinkOptions{Packages: []string{"zsh"}})
	requireErrorContains(t, err, "not an expected dotty link")
	requireErrorContains(t, err, "dotty status")
	requireFileContent(t, target, "local copy\n")

	requireNoError(t, os.Remove(target))
	wrongSource := filepath.Join(home, "wrong")
	writeTextFile(t, wrongSource, "wrong\n")
	requireNoError(t, os.Symlink(wrongSource, target))
	_, err = svc.Unlink(UnlinkOptions{Packages: []string{"zsh"}})
	requireErrorContains(t, err, "symlink to another source")
	assertSymlink(t, target, wrongSource)
}

func TestLeaveCopyUnlinkCopiesSourceAndFailsWhenSourceIsMissing(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, manifestWithSingleZshrcLink)
	source := filepath.Join(repo, "zsh", ".zshrc")
	writeTextFile(t, source, "export EDITOR=vim\n")
	target := filepath.Join(home, ".zshrc")
	requireNoError(t, os.Symlink(source, target))
	svc := NewService(repo, env)

	_, err := svc.Unlink(UnlinkOptions{Packages: []string{"zsh"}, LeaveCopy: true})
	requireNoError(t, err)
	requireFileContent(t, target, "export EDITOR=vim\n")

	requireNoError(t, os.Remove(target))
	requireNoError(t, os.Remove(source))
	requireNoError(t, os.Symlink(source, target))
	_, err = svc.Unlink(UnlinkOptions{Packages: []string{"zsh"}, LeaveCopy: true})
	requireErrorContains(t, err, "zsh/.zshrc is missing from the repository")
	assertSymlink(t, target, source)
}

func TestLeaveCopyUnlinkPrevalidatesCopyabilityBeforeRemovingLink(t *testing.T) {
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

	_, err = NewService(repo, env).Unlink(UnlinkOptions{Packages: []string{"zsh"}, LeaveCopy: true})
	requireErrorContains(t, err, "unsupported file type")

	assertSymlink(t, target, source)
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
}

func TestLeaveCopyUnlinkPrevalidatesNestedSymlinkReferentBeforeRemovingLink(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.app]
links = [
  { source = "config", target = "~/.config/app" },
]
`)
	realSource := filepath.Join(repo, "app", "pipes", "real.pipe")
	requireNoError(t, os.MkdirAll(filepath.Dir(realSource), 0o755))
	requireNoError(t, syscall.Mkfifo(realSource, 0o600))
	source := filepath.Join(repo, "app", "config")
	requireNoError(t, os.MkdirAll(source, 0o755))
	writeTextFile(t, filepath.Join(source, "app.conf"), "regular config\n")
	nestedLink := filepath.Join(source, "current.pipe")
	requireNoError(t, os.Symlink("../pipes/real.pipe", nestedLink))
	target := filepath.Join(home, ".config", "app")
	requireNoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	requireNoError(t, os.Symlink(source, target))
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)

	_, err = NewService(repo, env).Unlink(UnlinkOptions{Packages: []string{"app"}, LeaveCopy: true})
	requireErrorContains(t, err, "unsupported file type")

	assertSymlink(t, target, source)
	assertSymlink(t, nestedLink, "../pipes/real.pipe")
	info, err := os.Lstat(realSource)
	requireNoError(t, err)
	if info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("repo FIFO referent should remain unchanged, mode=%v", info.Mode())
	}
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
}

func TestLeaveCopyUnlinkAllowsInternalSymlinkToHardlinkedFileAndBreaksTargetAlias(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.app]
links = [
  { source = "config", target = "~/.config/app" },
]
`)
	protected := filepath.Join(home, "protected-token")
	writeTextFile(t, protected, "token=keep\n")
	source := filepath.Join(repo, "app", "config")
	requireNoError(t, os.MkdirAll(source, 0o755))
	token := filepath.Join(source, "token")
	if err := os.Link(protected, token); err != nil {
		t.Skipf("hardlinks are not supported in test filesystem: %v", err)
	}
	nestedLink := filepath.Join(source, "current-token")
	requireNoError(t, os.Symlink("token", nestedLink))
	target := filepath.Join(home, ".config", "app")
	requireNoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	requireNoError(t, os.Symlink(source, target))
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)

	_, err = NewService(repo, env).Unlink(UnlinkOptions{Packages: []string{"app"}, LeaveCopy: true})
	requireNoError(t, err)

	targetToken := filepath.Join(target, "token")
	requireFileContent(t, targetToken, "token=keep\n")
	assertSymlink(t, filepath.Join(target, "current-token"), "token")
	requireFileContent(t, protected, "token=keep\n")
	targetInfo, err := os.Stat(targetToken)
	requireNoError(t, err)
	protectedInfo, err := os.Stat(protected)
	requireNoError(t, err)
	if os.SameFile(targetInfo, protectedInfo) {
		t.Fatalf("soft unlink target copy should break external hardlink alias")
	}
	repoInfo, err := os.Stat(token)
	requireNoError(t, err)
	if !os.SameFile(repoInfo, protectedInfo) {
		t.Fatalf("soft unlink should not rewrite the repo Package Source hardlink")
	}
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
}

func TestLeaveCopyUnlinkPrevalidatesNestedSymlinkReferentHardlinksBeforeRemovingLink(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.app]
links = [
  { source = "config", target = "~/.config/app" },
]
`)
	protected := filepath.Join(home, "protected-token")
	writeTextFile(t, protected, "token=keep\n")
	realSource := filepath.Join(repo, "app", "shared", "token")
	requireNoError(t, os.MkdirAll(filepath.Dir(realSource), 0o755))
	if err := os.Link(protected, realSource); err != nil {
		t.Skipf("hardlinks are not supported in test filesystem: %v", err)
	}
	source := filepath.Join(repo, "app", "config")
	requireNoError(t, os.MkdirAll(source, 0o755))
	writeTextFile(t, filepath.Join(source, "app.conf"), "regular config\n")
	nestedLink := filepath.Join(source, "current-token")
	requireNoError(t, os.Symlink("../shared/token", nestedLink))
	target := filepath.Join(home, ".config", "app")
	requireNoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	requireNoError(t, os.Symlink(source, target))
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)

	_, err = NewService(repo, env).Unlink(UnlinkOptions{Packages: []string{"app"}, LeaveCopy: true})
	requireErrorContains(t, err, "external hardlink")

	assertSymlink(t, target, source)
	assertSymlink(t, nestedLink, "../shared/token")
	requireFileContent(t, protected, "token=keep\n")
	realInfo, err := os.Stat(realSource)
	requireNoError(t, err)
	protectedInfo, err := os.Stat(protected)
	requireNoError(t, err)
	if !os.SameFile(realInfo, protectedInfo) {
		t.Fatalf("failed unlink should not alter the repo hardlink referent")
	}
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
}

func TestLeaveCopyUnlinkRejectsAbsoluteInternalSymlinkToExternalHardlinkReferent(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.app]
links = [
  { source = "config", target = "~/.config/app" },
]
`)
	protected := filepath.Join(home, "protected-token")
	writeTextFile(t, protected, "token=keep\n")
	source := filepath.Join(repo, "app", "config")
	requireNoError(t, os.MkdirAll(source, 0o755))
	token := filepath.Join(source, "token")
	if err := os.Link(protected, token); err != nil {
		t.Skipf("hardlinks are not supported in test filesystem: %v", err)
	}
	nestedLink := filepath.Join(source, "current-token")
	requireNoError(t, os.Symlink(token, nestedLink))
	target := filepath.Join(home, ".config", "app")
	requireNoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	requireNoError(t, os.Symlink(source, target))
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)

	_, err = NewService(repo, env).Unlink(UnlinkOptions{Packages: []string{"app"}, LeaveCopy: true})
	requireErrorContains(t, err, "external hardlink")

	assertSymlink(t, target, source)
	assertSymlink(t, nestedLink, token)
	requireFileContent(t, protected, "token=keep\n")
	tokenInfo, err := os.Stat(token)
	requireNoError(t, err)
	protectedInfo, err := os.Stat(protected)
	requireNoError(t, err)
	if !os.SameFile(tokenInfo, protectedInfo) {
		t.Fatalf("failed unlink should not alter repo hardlink topology")
	}
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
}

func TestLeaveCopyUnlinkLeavesTargetCopyAsConflictForStatusAndPlainLink(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, manifestWithSingleZshrcLink)
	source := filepath.Join(repo, "zsh", ".zshrc")
	writeTextFile(t, source, "export EDITOR=vim\n")
	target := filepath.Join(home, ".zshrc")
	requireNoError(t, os.Symlink(source, target))
	svc := NewService(repo, env)
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)

	_, err = svc.Unlink(UnlinkOptions{Packages: []string{"zsh"}, LeaveCopy: true})
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

func TestLeaveCopyUnlinkMaterializesTopLevelSourceSymlinkReferent(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

[packages.zsh]
links = [
  { source = "current-zshrc", target = "~/.zshrc" },
]
`)
	realSource := filepath.Join(repo, "zsh", "real-zshrc")
	linkSource := filepath.Join(repo, "zsh", "current-zshrc")
	writeTextFile(t, realSource, "source inside package\n")
	rel, err := filepath.Rel(filepath.Dir(linkSource), realSource)
	requireNoError(t, err)
	requireNoError(t, os.Symlink(rel, linkSource))
	target := filepath.Join(home, ".zshrc")
	svc := NewService(repo, env)

	_, err = svc.Link(LinkOptions{Packages: []string{"zsh"}})
	requireNoError(t, err)
	assertSymlink(t, target, linkSource)

	_, err = svc.Unlink(UnlinkOptions{Packages: []string{"zsh"}, LeaveCopy: true})
	requireNoError(t, err)

	info, err := os.Lstat(target)
	requireNoError(t, err)
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf(
			"soft unlink should materialize source symlink referent, got symlink at %s",
			target,
		)
	}
	requireFileContent(t, target, "source inside package\n")
	assertSymlink(t, linkSource, rel)
	requireFileContent(t, realSource, "source inside package\n")
}

func TestDefaultUnlinkWithMissingSourceOnlyRemovesExpectedLinks(t *testing.T) {
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
			home, repo, env := setupLinkedPackageTest(t, manifestWithSingleZshrcLink)
			source := filepath.Join(repo, "zsh", ".zshrc")
			target := filepath.Join(home, ".zshrc")
			tt.setup(t, home, source, target)
			manifestBefore, err := os.ReadFile(ManifestPath(repo))
			requireNoError(t, err)

			_, err = NewService(repo, env).Unlink(UnlinkOptions{
				Packages: []string{"zsh"},
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

func TestDefaultUnlinkDryRunRejectsAbsentTargetUnderSymlinkedParent(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

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

	_, err := NewService(repo, env).Unlink(UnlinkOptions{
		Packages: []string{"app"},
		DryRun:   true,
	})
	requireErrorContains(t, err, "Target Path")
	assertSymlink(t, targetParent, externalParent)
	requireNoPath(t, filepath.Join(externalParent, "config"))
	requireFileContent(t, source, "managed config\n")
}

func TestDefaultUnlinkRejectsSymlinkedTargetParentWithoutRemovingReferentLink(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, `version = 1

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

	_, err := NewService(repo, env).Unlink(UnlinkOptions{Packages: []string{"app"}})
	requireErrorContains(t, err, "Target Path")
	assertSymlink(t, targetParent, externalParent)
	assertSymlink(t, referentLink, source)
	requireFileContent(t, source, "managed config\n")
}

func TestDefaultUnlinkWithBrokenPackageSourceSymlinkOnlyRemovesExpectedLink(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, manifestWithSingleZshrcLink)
	source := filepath.Join(repo, "zsh", ".zshrc")
	missing := filepath.Join(home, "missing-zshrc")
	requireNoError(t, os.MkdirAll(filepath.Dir(source), 0o755))
	requireNoError(t, os.Symlink(missing, source))
	target := filepath.Join(home, ".zshrc")
	requireNoError(t, os.Symlink(source, target))
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)

	_, err = NewService(repo, env).Unlink(UnlinkOptions{
		Packages: []string{"zsh"},
	})
	requireNoError(t, err)

	requireNoPath(t, target)
	assertSymlink(t, source, missing)
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
}

func TestLeaveCopyUnlinkRollsBackRemovedLinkWhenCopyFailsDuringExecution(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, manifestWithSingleZshrcLink)
	source := filepath.Join(repo, "zsh", ".zshrc")
	writeTextFile(t, source, "export EDITOR=vim\n")
	target := filepath.Join(home, ".zshrc")
	requireNoError(t, os.Symlink(source, target))
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)
	forceCopyPathErrorAfter(t, 0, errors.New("copy during unlink failed"))

	_, err = NewService(repo, env).Unlink(UnlinkOptions{Packages: []string{"zsh"}, LeaveCopy: true})
	requireErrorContains(t, err, "copy during unlink failed")

	assertSymlink(t, target, source)
	requireFileContent(t, source, "export EDITOR=vim\n")
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
	assertZshPackageState(t, NewService(repo, env), StateLinked)
}

func TestLeaveCopyUnlinkRollsBackEarlierTargetCopyWhenLaterMappingCopyFails(t *testing.T) {
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

	_, err = NewService(repo, env).Unlink(UnlinkOptions{Packages: []string{"zsh"}, LeaveCopy: true})
	requireErrorContains(t, err, "second copy during unlink failed")

	assertSymlink(t, zshrcTarget, zshrcSource)
	assertSymlink(t, zprofileTarget, zprofileSource)
	requireFileContent(t, zshrcSource, "export EDITOR=vim\n")
	requireFileContent(t, zprofileSource, "export PATH=$HOME/bin:$PATH\n")
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
	assertZshPackageState(t, NewService(repo, env), StateLinked)
}

func TestDefaultUnlinkRollsBackEarlierRemovalWhenLaterRemoveFails(t *testing.T) {
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
	})
	requireErrorContains(t, err, "second remove during unlink failed")

	assertSymlink(t, zshrcTarget, zshrcSource)
	assertSymlink(t, zprofileTarget, zprofileSource)
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
	assertZshPackageState(t, NewService(repo, env), StateLinked)
}

func TestLeaveCopyUnlinkReportsRollbackFailure(t *testing.T) {
	home, repo, env := setupLinkedPackageTest(t, manifestWithSingleZshrcLink)
	source := filepath.Join(repo, "zsh", ".zshrc")
	writeTextFile(t, source, "export EDITOR=vim\n")
	target := filepath.Join(home, ".zshrc")
	requireNoError(t, os.Symlink(source, target))
	manifestBefore, err := os.ReadFile(ManifestPath(repo))
	requireNoError(t, err)
	forceCopyPathErrorAfter(t, 0, errors.New("copy during unlink failed"))
	forceSymlinkPathError(t, errors.New("restore symlink failed"))

	_, err = NewService(repo, env).Unlink(UnlinkOptions{Packages: []string{"zsh"}, LeaveCopy: true})
	requireErrorContains(t, err, "copy during unlink failed")
	requireErrorContains(t, err, "rollback failed")
	requireErrorContains(t, err, "restore symlink failed")

	requireNoPath(t, target)
	requireFileContent(t, source, "export EDITOR=vim\n")
	requireFileContent(t, ManifestPath(repo), string(manifestBefore))
}

func linkResultActions(results []LinkResult) []string {
	actions := make([]string, 0, len(results))
	for _, result := range results {
		actions = append(actions, result.Action)
	}
	return actions
}

func unlinkResultActions(results []UnlinkResult) []string {
	actions := make([]string, 0, len(results))
	for _, result := range results {
		actions = append(actions, result.Action)
	}
	return actions
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
