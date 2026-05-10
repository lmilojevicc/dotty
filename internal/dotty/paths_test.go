package dotty

import (
	"os"
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

func TestPackageSourcePathRejectsSymlinkEscapingPackageRoot(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "dotfiles")
	escaped := filepath.Join(dir, "escaped")
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "pkg"), 0o755))
	requireNoError(t, os.MkdirAll(escaped, 0o755))
	writeTextFile(t, filepath.Join(escaped, "secret"), "secret\n")
	requireNoError(t, os.Symlink(escaped, filepath.Join(repo, "pkg", "outside")))

	_, err := PackageSourcePath(repo, "pkg", "outside/secret")
	requireErrorContains(t, err, "escapes package")
}

func TestPackageSourcePathRejectsMissingSourceUnderSymlinkEscapingPackageRoot(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "dotfiles")
	escaped := filepath.Join(dir, "escaped")
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "pkg"), 0o755))
	requireNoError(t, os.MkdirAll(escaped, 0o755))
	requireNoError(t, os.Symlink(escaped, filepath.Join(repo, "pkg", "outside")))

	_, err := PackageSourcePath(repo, "pkg", "outside/missing")
	requireErrorContains(t, err, "escapes package")
}

func TestPackageSourcePathAllowsOrdinaryMissingSource(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "dotfiles")
	requireNoError(t, os.MkdirAll(filepath.Join(repo, "pkg"), 0o755))

	source, err := PackageSourcePath(repo, "pkg", "missing")
	requireNoError(t, err)
	if want := filepath.Join(repo, "pkg", "missing"); source != want {
		t.Fatalf("source mismatch: want %s, got %s", want, source)
	}
}

func TestPackageSourcePathAllowsSymlinkResolvingInsidePackageRoot(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "dotfiles")
	packageRoot := filepath.Join(repo, "pkg")
	requireNoError(t, os.MkdirAll(filepath.Join(packageRoot, "real"), 0o755))
	writeTextFile(t, filepath.Join(packageRoot, "real", "config"), "config\n")
	requireNoError(t, os.Symlink("real", filepath.Join(packageRoot, "alias")))

	source, err := PackageSourcePath(repo, "pkg", "alias/config")
	requireNoError(t, err)
	if want := filepath.Join(packageRoot, "alias", "config"); source != want {
		t.Fatalf("source mismatch: want %s, got %s", want, source)
	}
}

func TestPackageSourcePathRejectsSymlinkedPackageRootOutsideRepository(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "dotfiles")
	escaped := filepath.Join(dir, "escaped")
	requireNoError(t, os.MkdirAll(repo, 0o755))
	requireNoError(t, os.MkdirAll(escaped, 0o755))
	writeTextFile(t, filepath.Join(escaped, "config"), "secret\n")
	requireNoError(t, os.Symlink(escaped, filepath.Join(repo, "pkg")))

	_, err := PackageSourcePath(repo, "pkg", "config")
	requireErrorContains(t, err, "escapes package")
}

func TestPackageSourcePathRejectsSymlinkedPackageRootOutsideRepositoryForMissingSource(
	t *testing.T,
) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "dotfiles")
	escaped := filepath.Join(dir, "escaped")
	requireNoError(t, os.MkdirAll(repo, 0o755))
	requireNoError(t, os.MkdirAll(escaped, 0o755))
	requireNoError(t, os.Symlink(escaped, filepath.Join(repo, "pkg")))

	_, err := PackageSourcePath(repo, "pkg", "missing")
	requireErrorContains(t, err, "escapes package")
}

func TestIsWithinAcceptsRootAndDescendantsButRejectsSiblings(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")

	if !isWithin(root, root) {
		t.Fatal("root should be within itself")
	}
	if !isWithin(root, filepath.Join(root, "child")) {
		t.Fatal("child should be within root")
	}
	if isWithin(root, root+"-sibling") {
		t.Fatal("sibling prefix should not be within root")
	}
	if isWithin(root, filepath.Join(root, "..", "outside")) {
		t.Fatal("parent traversal should not be within root")
	}
}

func TestExpandHomeEdges(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	env := Env{Home: home}

	got, err := expandHome("~", env)
	requireNoError(t, err)
	if got != home {
		t.Fatalf("home expansion mismatch: want %s, got %s", home, got)
	}
	got, err = expandHome("~/config", env)
	requireNoError(t, err)
	if want := filepath.Join(home, "config"); got != want {
		t.Fatalf("home child expansion mismatch: want %s, got %s", want, got)
	}
	got, err = expandHome("~other/config", env)
	requireNoError(t, err)
	if got != "~other/config" {
		t.Fatalf("unsupported tilde syntax should be unchanged, got %s", got)
	}
	_, err = expandHome("~", Env{})
	requireErrorContains(t, err, "home is empty")
}

func TestHomeRelativeEdges(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	env := Env{Home: home}

	if got := HomeRelative(home, env); got != "~" {
		t.Fatalf("home should be stored as ~, got %s", got)
	}
	sibling := home + "-sibling"
	if got := HomeRelative(sibling, env); got != sibling {
		t.Fatalf("sibling prefix should stay absolute: want %s, got %s", sibling, got)
	}
	inside := filepath.Join(home, "config", "app")
	if got := HomeRelative(inside, env); got != "~/config/app" {
		t.Fatalf("inside home mismatch: got %s", got)
	}
	if got := HomeRelative(inside, Env{}); got != filepath.Clean(inside) {
		t.Fatalf("empty home should keep absolute path, got %s", got)
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
