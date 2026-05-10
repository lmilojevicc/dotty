package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/lmilojevicc/dotty/internal/dotty"
)

func TestAddPrintsTargetToPackageSource(t *testing.T) {
	home, repo := setupCLITest(t)
	target := filepath.Join(home, ".config", "tmux")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(target, "tmux.conf"),
		[]byte("set -g mouse on\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := executeCommand("--repo", repo, "add", target, "tmux")
	if err != nil {
		t.Fatalf("add failed: %v\nstderr: %s", err, errOut)
	}

	want := "added tmux: ~/.config/tmux -> ~/dotfiles/tmux\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
}

func TestAddDryRunPrintsWouldAddAndDoesNotChangeFilesystem(t *testing.T) {
	home, repo := setupCLITest(t)
	target := filepath.Join(home, ".config", "tmux")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(target, "tmux.conf"),
		[]byte("set -g mouse on\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	manifestBefore, err := os.ReadFile(dotty.ManifestPath(repo))
	if err != nil {
		t.Fatal(err)
	}

	out, errOut, err := executeCommand("--repo", repo, "add", "--dry-run", target, "tmux")
	if err != nil {
		t.Fatalf("add --dry-run failed: %v\nstderr: %s", err, errOut)
	}

	want := "would add tmux: ~/.config/tmux -> ~/dotfiles/tmux\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
	if _, err := os.Stat(filepath.Join(target, "tmux.conf")); err != nil {
		t.Fatalf("target content changed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(repo, "tmux")); err == nil || !os.IsNotExist(err) {
		t.Fatalf("dry-run created package source: %v", err)
	}
	manifestAfter, err := os.ReadFile(dotty.ManifestPath(repo))
	if err != nil {
		t.Fatal(err)
	}
	if string(manifestAfter) != string(manifestBefore) {
		t.Fatalf(
			"manifest changed\nbefore: %q\nafter:  %q",
			string(manifestBefore),
			string(manifestAfter),
		)
	}
}

func TestLinkPrintsEachCreatedLink(t *testing.T) {
	home, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
  { source = ".zshrc_secrets", target = "~/secrets/.zshrc_secrets" },
]
`)
	if err := os.MkdirAll(filepath.Join(repo, "zsh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(repo, "zsh", ".zshrc"),
		[]byte("source ~/secrets/.zshrc_secrets\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(repo, "zsh", ".zshrc_secrets"),
		[]byte("export TOKEN=test\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := executeCommand("--repo", repo, "link", "zsh")
	if err != nil {
		t.Fatalf("link failed: %v\nstderr: %s", err, errOut)
	}

	want := "linked zsh: ~/.zshrc -> ~/dotfiles/zsh/.zshrc\n" +
		"linked zsh: ~/secrets/.zshrc_secrets -> ~/dotfiles/zsh/.zshrc_secrets\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
	assertSymlink(t, filepath.Join(home, ".zshrc"), filepath.Join(repo, "zsh", ".zshrc"))
	assertSymlink(
		t,
		filepath.Join(home, "secrets", ".zshrc_secrets"),
		filepath.Join(repo, "zsh", ".zshrc_secrets"),
	)
}

func TestLinkDryRunPrintsWouldLinkAndDoesNotReplaceConflict(t *testing.T) {
	home, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)
	if err := os.MkdirAll(filepath.Join(repo, "zsh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(repo, "zsh", ".zshrc"),
		[]byte("export EDITOR=vim\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(target, []byte("local copy\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := executeCommand("--repo", repo, "link", "--force", "--dry-run", "zsh")
	if err != nil {
		t.Fatalf("link --dry-run failed: %v\nstderr: %s", err, errOut)
	}

	want := "would link zsh: ~/.zshrc -> ~/dotfiles/zsh/.zshrc\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "local copy\n" {
		t.Fatalf("target conflict changed: data=%q err=%v", string(data), err)
	}
}

func TestUnlinkPrintsTargetAction(t *testing.T) {
	_, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]
`)
	if err := os.MkdirAll(filepath.Join(repo, "tmux"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(repo, "tmux", "tmux.conf"),
		[]byte("set -g mouse on\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	home, _ := filepath.Split(repo)
	home = filepath.Dir(home)
	target := filepath.Join(home, ".config", "tmux")
	if err := os.Symlink(filepath.Join(repo, "tmux"), target); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := executeCommand("--repo", repo, "unlink", "tmux")
	if err != nil {
		t.Fatalf("unlink failed: %v\nstderr: %s", err, errOut)
	}

	want := "unlinked tmux: ~/.config/tmux (copy left)\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
}

func TestUnlinkDryRunPrintsWouldUnlinkAndLeavesLink(t *testing.T) {
	home, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)
	if err := os.MkdirAll(filepath.Join(repo, "zsh"), 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(repo, "zsh", ".zshrc")
	if err := os.WriteFile(source, []byte("export EDITOR=vim\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".zshrc")
	if err := os.Symlink(source, target); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := executeCommand("--repo", repo, "unlink", "--dry-run", "zsh")
	if err != nil {
		t.Fatalf("unlink --dry-run failed: %v\nstderr: %s", err, errOut)
	}

	want := "would unlink zsh: ~/.zshrc (copy left)\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
	assertSymlink(t, target, source)
}

func TestInitPrintsRepositoryPathAndStoresDefaultRepository(t *testing.T) {
	home := setupCLIHomeOnly(t)
	repo := filepath.Join(home, "dotfiles")

	out, errOut, err := executeCommand("init", repo)
	if err != nil {
		t.Fatalf("init failed: %v\nstderr: %s", err, errOut)
	}

	want := "initialized " + repo + "\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
	env := dotty.Env{
		Home:          home,
		XDGConfigHome: filepath.Join(home, ".config"),
	}
	cfg, err := dotty.LoadConfig(env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Repo != "~/dotfiles" {
		t.Fatalf("default repository mismatch: want ~/dotfiles, got %s", cfg.Repo)
	}
}

func TestVersionCommandPrintsVersion(t *testing.T) {
	previousVersion := Version
	Version = "v1.2.3"
	t.Cleanup(func() { Version = previousVersion })

	out, errOut, err := executeCommand("version")
	if err != nil {
		t.Fatalf("version failed: %v\nstderr: %s", err, errOut)
	}

	want := "dotty version v1.2.3\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
}

func TestRootDescriptionAndStatusVerboseHelpAreApproachable(t *testing.T) {
	cmd := NewRootCommand(io.Discard, io.Discard)
	if want := "Sync configuration files across machines using a manifest"; cmd.Short != want {
		t.Fatalf("root short mismatch: want %q, got %q", want, cmd.Short)
	}
	status, _, err := cmd.Find([]string{"status"})
	if err != nil {
		t.Fatal(err)
	}
	usage := status.Flags().Lookup("verbose").Usage
	if !strings.Contains(usage, "status output") {
		t.Fatalf("verbose help should clarify status scope, got %q", usage)
	}
}

func TestRepoCommandPrintsResolvedRepositoryAndConfigPath(t *testing.T) {
	_, repo := setupCLITest(t)

	out, errOut, err := executeCommand("--repo", repo, "repo")
	if err != nil {
		t.Fatalf("repo failed: %v\nstderr: %s", err, errOut)
	}

	want := "Repository: ~/dotfiles\nConfig: ~/.config/dotty/config.toml\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
}

func TestRepoCommandUsesConfiguredRepository(t *testing.T) {
	setupCLITest(t)

	out, errOut, err := executeCommand("repo")
	if err != nil {
		t.Fatalf("repo failed: %v\nstderr: %s", err, errOut)
	}

	want := "Repository: ~/dotfiles\nConfig: ~/.config/dotty/config.toml\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
}

func TestRepoCommandUsesDottyRepoEnvironmentOverride(t *testing.T) {
	home := setupCLIHomeOnly(t)
	repo := filepath.Join(home, "env-dotfiles")
	t.Setenv("DOTTY_REPO", repo)

	out, errOut, err := executeCommand("repo")
	if err != nil {
		t.Fatalf("repo failed: %v\nstderr: %s", err, errOut)
	}

	want := "Repository: ~/env-dotfiles\nConfig: ~/.config/dotty/config.toml\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
}

func TestListPrintsPackagesAndCollections(t *testing.T) {
	_, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

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
`)

	out, errOut, err := executeCommand("--repo", repo, "list")
	if err != nil {
		t.Fatalf("list failed: %v\nstderr: %s", err, errOut)
	}

	requireOutputContains(t, out, "Packages\n")
	requireOutputContains(t, out, "tmux")
	requireOutputContains(t, out, "1 link")
	requireOutputContains(t, out, "zsh")
	requireOutputContains(t, out, "2 links")
	requireOutputContains(t, out, "Collections\n")
	requireOutputContains(t, out, "terminal")
	requireOutputContains(t, out, "tmux, zsh")
}

func TestStatusPrintsPackageSummariesAndVerboseEntries(t *testing.T) {
	home, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]
`)
	if err := os.MkdirAll(filepath.Join(repo, "zsh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(repo, "zsh", ".zshrc"),
		[]byte("export EDITOR=vim\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(repo, "zsh", ".zprofile"),
		[]byte("export PATH\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "tmux"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "ghostty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(
		filepath.Join(repo, "zsh", ".zshrc"),
		filepath.Join(home, ".zshrc"),
	); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := executeCommand("--repo", repo, "status")
	if err != nil {
		t.Fatalf("status failed: %v\nstderr: %s", err, errOut)
	}
	want := fmt.Sprintf(
		"Repository: ~/dotfiles\n\n%-24s %s\n%-24s %s\n\n%s\n  %s\n  %s\n\nSummary: 2 packages: 1 linked, 1 unlinked; 2 untracked\n",
		"tmux",
		"UNLINKED",
		"zsh",
		"LINKED",
		"UNTRACKED",
		"ghostty",
		"zsh/.zprofile",
	)
	if out != want {
		t.Fatalf("unexpected status output\nwant:\n%s\ngot:\n%s", want, out)
	}

	out, errOut, err = executeCommand("--repo", repo, "status", "--verbose")
	if err != nil {
		t.Fatalf("status --verbose failed: %v\nstderr: %s", err, errOut)
	}
	want = fmt.Sprintf(
		"Repository: ~/dotfiles\n\n%-18s %-20s %-36s %s\n%-18s %-20s %-36s %s\n\n%-18s %-20s %-36s %s\n%-18s %-20s %-36s %s\n\nSummary: 2 packages: 1 linked, 1 unlinked; 2 untracked\n",
		"tmux",
		".",
		"~/.config/tmux",
		"UNLINKED",
		"zsh",
		".zshrc",
		"~/.zshrc",
		"LINKED",
		"-",
		"ghostty",
		"-",
		"UNTRACKED",
		"-",
		"zsh/.zprofile",
		"-",
		"UNTRACKED",
	)
	if out != want {
		t.Fatalf("unexpected verbose status output\nwant:\n%s\ngot:\n%s", want, out)
	}
}

func TestStatusRenderingKeepsLipglossStylesWithoutBorders(t *testing.T) {
	previousProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(previousProfile) })

	var out bytes.Buffer
	renderStatus(&out, &dotty.StatusReport{
		Packages:  []dotty.PackageStatus{{Name: "tmux", State: dotty.StateLinked}},
		Untracked: []dotty.UntrackedItem{{Path: "ghostty", State: dotty.StateUntracked}},
	}, false)
	got := out.String()

	if strings.ContainsAny(got, "┌┬┐├┼┤└┴┘│─") {
		t.Fatalf("expected no table borders, got %q", got)
	}
	for _, want := range []string{"\x1b[1;36mtmux", "\x1b[1;32mLINKED", "\x1b[34mghostty", "\x1b[1;34mUNTRACKED"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected styled output to contain %q, got %q", want, got)
		}
	}
}

func TestLinkCollectionPrintsAndCreatesLinks(t *testing.T) {
	home, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]

[collections.terminal]
packages = ["tmux", "zsh"]
`)
	if err := os.MkdirAll(filepath.Join(repo, "tmux"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "zsh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(repo, "zsh", ".zshrc"),
		[]byte("export EDITOR=vim\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := executeCommand("--repo", repo, "link", "--collection", "terminal")
	if err != nil {
		t.Fatalf("link collection failed: %v\nstderr: %s", err, errOut)
	}
	requireOutputContains(t, out, "linked tmux")
	requireOutputContains(t, out, "linked zsh")
	assertSymlink(t, filepath.Join(home, ".config", "tmux"), filepath.Join(repo, "tmux"))
	assertSymlink(t, filepath.Join(home, ".zshrc"), filepath.Join(repo, "zsh", ".zshrc"))
}

func TestLinkAllPrintsAndCreatesLinks(t *testing.T) {
	home, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]
`)
	if err := os.MkdirAll(filepath.Join(repo, "tmux"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "zsh"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(repo, "zsh", ".zshrc"),
		[]byte("export EDITOR=vim\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := executeCommand("--repo", repo, "link", "--all")
	if err != nil {
		t.Fatalf("link --all failed: %v\nstderr: %s", err, errOut)
	}
	want := "linked tmux: ~/.config/tmux -> ~/dotfiles/tmux\n" +
		"linked zsh: ~/.zshrc -> ~/dotfiles/zsh/.zshrc\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
	assertSymlink(t, filepath.Join(home, ".config", "tmux"), filepath.Join(repo, "tmux"))
	assertSymlink(t, filepath.Join(home, ".zshrc"), filepath.Join(repo, "zsh", ".zshrc"))
}

func TestUnlinkHardPrintsLinkRemoved(t *testing.T) {
	home, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)
	if err := os.MkdirAll(filepath.Join(repo, "zsh"), 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(repo, "zsh", ".zshrc")
	if err := os.WriteFile(source, []byte("export EDITOR=vim\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, filepath.Join(home, ".zshrc")); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := executeCommand("--repo", repo, "unlink", "--hard", "zsh")
	if err != nil {
		t.Fatalf("unlink --hard failed: %v\nstderr: %s", err, errOut)
	}
	want := "hard-unlinked zsh: ~/.zshrc (link removed)\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
}

func TestUnlinkAllPrintsAndLeavesCopies(t *testing.T) {
	home, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]
`)
	tmuxSource := filepath.Join(repo, "tmux")
	if err := os.MkdirAll(tmuxSource, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(tmuxSource, "tmux.conf"),
		[]byte("set -g mouse on\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "zsh"), 0o755); err != nil {
		t.Fatal(err)
	}
	zshSource := filepath.Join(repo, "zsh", ".zshrc")
	if err := os.WriteFile(zshSource, []byte("export EDITOR=vim\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tmuxTarget := filepath.Join(home, ".config", "tmux")
	if err := os.MkdirAll(filepath.Dir(tmuxTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(tmuxSource, tmuxTarget); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(zshSource, filepath.Join(home, ".zshrc")); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := executeCommand("--repo", repo, "unlink", "--all")
	if err != nil {
		t.Fatalf("unlink --all failed: %v\nstderr: %s", err, errOut)
	}
	want := "unlinked tmux: ~/.config/tmux (copy left)\n" +
		"unlinked zsh: ~/.zshrc (copy left)\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
	if _, err := os.Stat(filepath.Join(tmuxTarget, "tmux.conf")); err != nil {
		t.Fatalf("tmux copy missing: %v", err)
	}
	if data, err := os.ReadFile(
		filepath.Join(home, ".zshrc"),
	); err != nil ||
		string(data) != "export EDITOR=vim\n" {
		t.Fatalf("zsh copy mismatch: data=%q err=%v", string(data), err)
	}
}

func TestCommandReturnsCoreErrors(t *testing.T) {
	_, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.zsh]
links = []

[collections.terminal]
packages = ["zsh"]
`)

	_, _, err := executeCommand("--repo", repo, "link")
	if err == nil || !strings.Contains(err.Error(), "usage: dotty link") {
		t.Fatalf("expected usage error, got %v", err)
	}

	_, _, err = executeCommand("--repo", repo, "link", "tmux")
	if err == nil || !strings.Contains(err.Error(), "unknown package") {
		t.Fatalf("expected unknown package error, got %v", err)
	}

	_, _, err = executeCommand("--repo", repo, "link", "--all", "zsh")
	if err == nil || !strings.Contains(err.Error(), "--all cannot be combined") {
		t.Fatalf("expected --all package conflict error, got %v", err)
	}

	_, _, err = executeCommand("--repo", repo, "unlink", "--all", "--collection", "terminal")
	if err == nil || !strings.Contains(err.Error(), "--all cannot be combined") {
		t.Fatalf("expected --all collection conflict error, got %v", err)
	}
}

func TestCommandArgumentErrorsIncludeSampleUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "add missing args",
			args: []string{"add"},
			want: "usage: dotty add <path> <package>",
		},
		{
			name: "add too many args",
			args: []string{"add", "one", "two", "three"},
			want: "usage: dotty add <path> <package>",
		},
		{
			name: "init too many args",
			args: []string{"init", "one", "two"},
			want: "usage: dotty init [<path>]",
		},
		{
			name: "link missing selector",
			args: []string{"link"},
			want: "usage: dotty link <package>... | --all | --collection <collection>",
		},
		{
			name: "unlink missing selector",
			args: []string{"unlink"},
			want: "usage: dotty unlink <package>... | --all | --collection <collection>",
		},
		{name: "list too many args", args: []string{"list", "extra"}, want: "usage: dotty list"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := executeCommand(tt.args...)
			if err == nil {
				t.Fatalf("expected argument error")
			}
			if err.Error() != tt.want {
				t.Fatalf("unexpected error\nwant: %q\ngot:  %q", tt.want, err.Error())
			}
			if stdout != "" || stderr != "" {
				t.Fatalf("expected no command output, got stdout=%q stderr=%q", stdout, stderr)
			}
		})
	}
}

func setupCLITest(t *testing.T) (home string, repo string) {
	t.Helper()
	home = filepath.Join(t.TempDir(), "home")
	repo = filepath.Join(home, "dotfiles")
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("DOTTY_REPO", "")
	env := dotty.Env{
		Home:          home,
		XDGConfigHome: filepath.Join(home, ".config"),
	}
	if _, err := dotty.InitRepo(repo, env); err != nil {
		t.Fatal(err)
	}
	return home, repo
}

func setupCLIHomeOnly(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(filepath.Join(home, ".config"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("DOTTY_REPO", "")
	return home
}

func executeCommand(args ...string) (stdout string, stderr string, err error) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := NewRootCommand(&out, &errOut)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return stripANSI(out.String()), stripANSI(errOut.String()), err
}

func writeManifest(t *testing.T, repo string, content string) {
	t.Helper()
	if err := os.WriteFile(
		filepath.Join(repo, dotty.ManifestFileName),
		[]byte(content),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
}

func assertSymlink(t *testing.T, linkPath, wantTarget string) {
	t.Helper()
	got, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantTarget {
		t.Fatalf("symlink target mismatch: want %s, got %s", wantTarget, got)
	}
}

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func requireOutputContains(t *testing.T, output, want string) {
	t.Helper()
	if !strings.Contains(output, want) {
		t.Fatalf("expected output to contain %q, got %q", want, output)
	}
}
