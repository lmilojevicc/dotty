package dotty

import (
	"os"
	"path/filepath"
	"testing"
)

func setupHome(t *testing.T) (string, Env) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	env := Env{
		Home:          home,
		XDGConfigHome: filepath.Join(home, ".config"),
	}
	return home, env
}

func TestAddDirectoryUnlinkAndForceRelink(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	svc, err := InitRepo(repo, env)
	if err != nil {
		t.Fatal(err)
	}

	tmuxTarget := filepath.Join(home, ".config", "tmux")
	if err := os.MkdirAll(tmuxTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(tmuxTarget, "tmux.conf"),
		[]byte("set -g mouse on\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	result, err := svc.Add(tmuxTarget, "tmux")
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != "." || result.Target != "~/.config/tmux" {
		t.Fatalf("unexpected add result: %#v", result)
	}
	assertSymlink(t, tmuxTarget, filepath.Join(repo, "tmux"))
	if _, err := os.Stat(filepath.Join(repo, "tmux", "tmux.conf")); err != nil {
		t.Fatal(err)
	}
	assertTmuxPackageState(t, svc, StateLinked)

	if _, err := svc.Unlink(UnlinkOptions{Packages: []string{"tmux"}}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(tmuxTarget); err != nil || !info.IsDir() {
		t.Fatalf("soft unlink should leave a directory copy, info=%v err=%v", info, err)
	}
	assertTmuxPackageState(t, svc, StateConflict)

	if _, err := svc.Link(LinkOptions{Packages: []string{"tmux"}, Force: true}); err != nil {
		t.Fatal(err)
	}
	assertSymlink(t, tmuxTarget, filepath.Join(repo, "tmux"))
	assertTmuxPackageState(t, svc, StateLinked)
}

func TestInPlaceAdoptionFromExistingStowSymlink(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dot-example")
	if err := os.MkdirAll(filepath.Join(repo, "tmux"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(repo, "tmux", "tmux.conf"),
		[]byte("set -g status on\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	svc, err := InitRepo(repo, env)
	if err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(home, ".config", "tmux")
	rel, err := filepath.Rel(filepath.Dir(target), filepath.Join(repo, "tmux"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rel, target); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Add(target, "tmux"); err != nil {
		t.Fatal(err)
	}
	assertSymlink(t, target, filepath.Join(repo, "tmux"))
	if _, err := os.Stat(filepath.Join(repo, "tmux", "tmux.conf")); err != nil {
		t.Fatal(err)
	}
	assertTmuxPackageState(t, svc, StateLinked)
}

func TestExternalSymlinkAdoptionCopiesSource(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	svc, err := InitRepo(repo, env)
	if err != nil {
		t.Fatal(err)
	}

	oldSource := filepath.Join(home, "old-stow", "tmux")
	if err := os.MkdirAll(oldSource, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(oldSource, "tmux.conf"),
		[]byte("set -g prefix C-a\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".config", "tmux")
	if err := os.Symlink(oldSource, target); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Add(target, "tmux"); err != nil {
		t.Fatal(err)
	}
	assertSymlink(t, target, filepath.Join(repo, "tmux"))
	if _, err := os.Stat(filepath.Join(oldSource, "tmux.conf")); err != nil {
		t.Fatalf("external stow source should remain intact: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "tmux", "tmux.conf")); err != nil {
		t.Fatalf("copied dotty source missing: %v", err)
	}
}

func TestFormatManifestWithCollection(t *testing.T) {
	manifest := NewManifest()
	manifest.Packages["ghostty"] = Package{
		Links: []LinkMapping{{Source: ".", Target: "~/.config/ghostty"}},
	}
	manifest.Packages["wezterm"] = Package{
		Links: []LinkMapping{{Source: ".", Target: "~/.config/wezterm"}},
	}
	manifest.Collections["terminal"] = Collection{Packages: []string{"ghostty", "wezterm"}}
	got := FormatManifest(manifest)
	want := "version = 1\n\n[packages.ghostty]\nlinks = [\n  { source = \".\", target = \"~/.config/ghostty\" },\n]\n\n[packages.wezterm]\nlinks = [\n  { source = \".\", target = \"~/.config/wezterm\" },\n]\n\n[collections.terminal]\npackages = [\"ghostty\", \"wezterm\"]\n"
	if got != want {
		t.Fatalf("manifest mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func assertSymlink(t *testing.T, linkPath, wantTarget string) {
	t.Helper()
	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not a symlink", linkPath)
	}
	got, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantTarget {
		t.Fatalf("symlink target mismatch: want %s, got %s", wantTarget, got)
	}
}

func assertTmuxPackageState(t *testing.T, svc Service, want State) {
	t.Helper()
	report, err := svc.Status([]string{"tmux"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Packages) != 1 {
		t.Fatalf("expected one package status, got %d", len(report.Packages))
	}
	if got := report.Packages[0].State; got != want {
		t.Fatalf("state mismatch: want %s, got %s", want, got)
	}
}
