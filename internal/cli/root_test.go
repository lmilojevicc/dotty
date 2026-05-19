package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

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
	homeBefore := snapshotTree(t, home)

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
	requireSnapshotUnchanged(t, home, homeBefore)
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

	want := "linked zsh/.zshrc -> ~/.zshrc\n" +
		"linked zsh/.zshrc_secrets -> ~/secrets/.zshrc_secrets\n"
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
	homeBefore := snapshotTree(t, home)

	out, errOut, err := executeCommand("--repo", repo, "link", "--force", "--dry-run", "zsh")
	if err != nil {
		t.Fatalf("link --dry-run failed: %v\nstderr: %s", err, errOut)
	}

	want := "would replace and link zsh/.zshrc -> ~/.zshrc\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "local copy\n" {
		t.Fatalf("target conflict changed: data=%q err=%v", string(data), err)
	}
	requireSnapshotUnchanged(t, home, homeBefore)
}

func TestLinkDryRunPlainPrintsWouldLinkAndDoesNotMutateFilesystem(t *testing.T) {
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
	homeBefore := snapshotTree(t, home)

	out, errOut, err := executeCommand("--repo", repo, "link", "--dry-run", "zsh")
	if err != nil {
		t.Fatalf("link --dry-run failed: %v\nstderr: %s", err, errOut)
	}

	want := "would link zsh/.zshrc -> ~/.zshrc\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
	if _, err := os.Lstat(target); err == nil || !os.IsNotExist(err) {
		t.Fatalf("dry-run created target: %v", err)
	}
	requireSnapshotUnchanged(t, home, homeBefore)
}

func TestLinkDryRunPrintsDetailedActions(t *testing.T) {
	home, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.config]
links = [
  { source = "absent", target = "~/.absent" },
  { source = "linked", target = "~/.linked" },
  { source = "relative", target = "~/.relative" },
  { source = "conflict", target = "~/.conflict" },
]
`)
	if err := os.MkdirAll(filepath.Join(repo, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, sourceName := range []string{"absent", "linked", "relative", "conflict"} {
		if err := os.WriteFile(
			filepath.Join(repo, "config", sourceName),
			[]byte(sourceName+"\n"),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(
		filepath.Join(repo, "config", "linked"),
		filepath.Join(home, ".linked"),
	); err != nil {
		t.Fatal(err)
	}
	relativeTarget := filepath.Join(home, ".relative")
	rel, err := filepath.Rel(
		filepath.Dir(relativeTarget),
		filepath.Join(repo, "config", "relative"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rel, relativeTarget); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(home, ".conflict"),
		[]byte("local copy\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	homeBefore := snapshotTree(t, home)

	out, errOut, err := executeCommand("--repo", repo, "link", "--force", "--dry-run", "config")
	if err != nil {
		t.Fatalf("link --force --dry-run failed: %v\nstderr: %s", err, errOut)
	}

	want := "would link config/absent -> ~/.absent\n" +
		"already linked config/linked -> ~/.linked\n" +
		"would link config/relative -> ~/.relative\n" +
		"would replace and link config/conflict -> ~/.conflict\n"
	if out != want {
		t.Fatalf("unexpected detailed link dry-run output\nwant: %q\ngot:  %q", want, out)
	}
	requireSnapshotUnchanged(t, home, homeBefore)
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

	want := "unlinked tmux -> ~/.config/tmux\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
}

func TestUnlinkDryRunPrintsWouldRemoveLinkAndLeavesLink(t *testing.T) {
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
	homeBefore := snapshotTree(t, home)

	out, errOut, err := executeCommand("--repo", repo, "unlink", "--dry-run", "zsh")
	if err != nil {
		t.Fatalf("unlink --dry-run failed: %v\nstderr: %s", err, errOut)
	}

	want := "would unlink zsh/.zshrc -> ~/.zshrc\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
	assertSymlink(t, target, source)
	requireSnapshotUnchanged(t, home, homeBefore)
}

func TestUnlinkDryRunPrintsDetailedActions(t *testing.T) {
	home, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.config]
links = [
  { source = "linked", target = "~/.linked" },
  { source = "absent", target = "~/.absent" },
]
`)
	if err := os.MkdirAll(filepath.Join(repo, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkedSource := filepath.Join(repo, "config", "linked")
	if err := os.WriteFile(linkedSource, []byte("linked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(repo, "config", "absent"),
		[]byte("absent\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkedSource, filepath.Join(home, ".linked")); err != nil {
		t.Fatal(err)
	}
	homeBefore := snapshotTree(t, home)

	out, errOut, err := executeCommand(
		"--repo",
		repo,
		"unlink",
		"--leave-copy",
		"--dry-run",
		"config",
	)
	if err != nil {
		t.Fatalf("unlink --dry-run failed: %v\nstderr: %s", err, errOut)
	}
	want := "would leave copy config/linked -> ~/.linked\n" +
		"would leave copy config/absent -> ~/.absent\n"
	if out != want {
		t.Fatalf("unexpected soft unlink dry-run output\nwant: %q\ngot:  %q", want, out)
	}

	out, errOut, err = executeCommand("--repo", repo, "unlink", "--dry-run", "config")
	if err != nil {
		t.Fatalf("unlink --dry-run failed: %v\nstderr: %s", err, errOut)
	}
	want = "would unlink config/linked -> ~/.linked\n" +
		"already absent config/absent -> ~/.absent\n"
	if out != want {
		t.Fatalf("unexpected default unlink dry-run output\nwant: %q\ngot:  %q", want, out)
	}
	requireSnapshotUnchanged(t, home, homeBefore)
}

func TestUnlinkDefaultDryRunPrintsWouldRemoveLinkAndLeavesLink(t *testing.T) {
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
	homeBefore := snapshotTree(t, home)

	out, errOut, err := executeCommand("--repo", repo, "unlink", "--dry-run", "zsh")
	if err != nil {
		t.Fatalf("unlink --dry-run failed: %v\nstderr: %s", err, errOut)
	}

	want := "would unlink zsh/.zshrc -> ~/.zshrc\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
	assertSymlink(t, target, source)
	requireSnapshotUnchanged(t, home, homeBefore)
}

func TestInitRejectsRepoFlag(t *testing.T) {
	home := setupCLIHomeOnly(t)
	repoFlag := filepath.Join(home, "override")
	repoArg := filepath.Join(home, "dotfiles")

	_, _, err := executeCommand("--repo", repoFlag, "init", repoArg)
	if err == nil || !strings.Contains(err.Error(), "--repo cannot be used with init") {
		t.Fatalf("expected init --repo rejection, got %v", err)
	}
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
	stateUsage := status.Flags().Lookup("state").Usage
	if !strings.Contains(stateUsage, "status state") || !strings.Contains(stateUsage, "repeated") {
		t.Fatalf("state help should clarify filtering scope, got %q", stateUsage)
	}
	completion, _, err := cmd.Find([]string{"completion"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "completion <bash|zsh|fish|powershell>"; completion.Use != want {
		t.Fatalf("completion usage mismatch: want %q, got %q", want, completion.Use)
	}
}

func TestCommandSurfaceInventory(t *testing.T) {
	cmd := NewRootCommand(io.Discard, io.Discard)
	if got, want := cmd.Use, "dotty"; got != want {
		t.Fatalf("root use mismatch: want %q, got %q", want, got)
	}
	if got, want := cmd.Short, "Sync configuration files across machines using a manifest"; got != want {
		t.Fatalf("root short mismatch: want %q, got %q", want, got)
	}
	if flag := cmd.PersistentFlags().Lookup("repo"); flag == nil {
		t.Fatal("root missing --repo global flag")
	} else if flag.Usage != "dotfiles repository path (overrides DOTTY_REPO and config)" {
		t.Fatalf("unexpected --repo usage: %q", flag.Usage)
	}

	tests := []struct {
		name  string
		use   string
		short string
		flags []string
	}{
		{
			name:  "init",
			use:   "init [<path>]",
			short: "Initialize a dotty repository and remember it as the default",
		},
		{
			name:  "add",
			use:   "add <path> <package>",
			short: "Adopt an existing file, directory, or symlink target into a package",
			flags: []string{"dry-run:"},
		},
		{
			name:  "link",
			use:   "link <selector>... | --all | --collection <collection>",
			short: "Create links for packages, all packages, or an explicit collection",
			flags: []string{"all:", "collection:c", "dry-run:", "force:", "target:", "track:"},
		},
		{
			name:  "unlink",
			use:   "unlink <selector>... | --all | --collection <collection>",
			short: "Remove links for packages, all packages, or an explicit collection",
			flags: []string{
				"all:",
				"collection:c",
				"dry-run:",
				"leave-copy:",
				"target:",
				"untrack:",
			},
		},
		{
			name:  "status",
			use:   "status [<selector>...]",
			short: "Show linked, unlinked, conflict, blocked, missing-source, empty, partial, and untracked states",
			flags: []string{"state:", "verbose:v"},
		},
		{
			name:  "list",
			use:   "list [<package>]",
			short: "List packages and collections defined in the manifest",
		},
		{
			name:  "repo",
			use:   "repo",
			short: "Show the resolved dotfiles repository and config path",
		},
		{
			name:  "completion",
			use:   "completion <bash|zsh|fish|powershell>",
			short: "Generate shell completion scripts",
		},
		{
			name:  "version",
			use:   "version",
			short: "Print the version number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subcommand, _, err := cmd.Find([]string{tt.name})
			if err != nil {
				t.Fatal(err)
			}
			if subcommand.Use != tt.use {
				t.Fatalf("%s use mismatch: want %q, got %q", tt.name, tt.use, subcommand.Use)
			}
			if subcommand.Short != tt.short {
				t.Fatalf(
					"%s short mismatch: want %q, got %q",
					tt.name,
					tt.short,
					subcommand.Short,
				)
			}
			got := flagSpecs(subcommand.Flags())
			if strings.Join(got, "\n") != strings.Join(tt.flags, "\n") {
				t.Fatalf("%s flags mismatch\nwant: %#v\ngot:  %#v", tt.name, tt.flags, got)
			}
		})
	}
}

func TestHelpVersionAndCompletionAreRepositoryIndependent(t *testing.T) {
	setupCLIHomeOnly(t)
	previousVersion := Version
	Version = "v9.8.7"
	t.Cleanup(func() { Version = previousVersion })

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "root help",
			args: []string{"--help"},
			want: []string{
				"Usage:\n  dotty [command]\n",
				"init",
				"add",
				"link",
				"unlink",
				"status",
				"list",
				"repo",
				"completion",
				"version",
				"Global options:\n      --repo string",
			},
		},
		{
			name: "init help",
			args: []string{"init", "--help"},
			want: []string{
				"Usage:\n  dotty init [<path>] [flags]\n",
				"Initialize a dotty repository and remember it as the default",
				"Global options:\n      --repo string",
			},
		},
		{
			name: "add help",
			args: []string{"add", "--help"},
			want: []string{
				"Usage:\n  dotty add <path> <package> [flags]\n",
				"Options:\n      --dry-run",
				"Global options:\n      --repo string",
			},
		},
		{
			name: "link help",
			args: []string{"link", "--help"},
			want: []string{
				"Usage:\n  dotty link <selector>... | --all | --collection <collection> [flags]\n",
				"Options:\n      --all",
				"  -c, --collection stringArray",
				"      --force",
				"      --target stringArray",
				"      --dry-run",
				"Global options:\n      --repo string",
			},
		},
		{
			name: "unlink help",
			args: []string{"unlink", "--help"},
			want: []string{
				"Usage:\n  dotty unlink <selector>... | --all | --collection <collection> [flags]\n",
				"Options:\n      --all",
				"  -c, --collection stringArray",
				"      --leave-copy",
				"      --target stringArray",
				"      --dry-run",
				"Global options:\n      --repo string",
			},
		},
		{
			name: "status help",
			args: []string{"status", "--help"},
			want: []string{
				"Usage:\n  dotty status [<selector>...] [flags]\n",
				"--state stringArray   filter by status state (can be repeated)",
				"  -v, --verbose",
				"Global options:\n      --repo string",
			},
		},
		{
			name: "list help",
			args: []string{"list", "--help"},
			want: []string{
				"Usage:\n  dotty list [<package>] [flags]\n",
				"Global options:\n      --repo string",
			},
		},
		{
			name: "repo help",
			args: []string{"repo", "--help"},
			want: []string{
				"Usage:\n  dotty repo [flags]\n",
				"Global options:\n      --repo string",
			},
		},
		{
			name: "completion help",
			args: []string{"completion", "--help"},
			want: []string{
				"Usage:\n  dotty completion <bash|zsh|fish|powershell> [flags]\n",
				"Global options:\n      --repo string",
			},
		},
		{
			name: "version help",
			args: []string{"version", "--help"},
			want: []string{"Usage:\n  dotty version [flags]\n"},
		},
		{
			name: "root version flag",
			args: []string{"--version"},
			want: []string{"dotty version v9.8.7\n"},
		},
		{
			name: "version command",
			args: []string{"version"},
			want: []string{"dotty version v9.8.7\n"},
		},
		{
			name: "bash completion",
			args: []string{"completion", "bash"},
			want: []string{"__start_dotty"},
		},
		{
			name: "zsh completion",
			args: []string{"completion", "zsh"},
			want: []string{"#compdef dotty"},
		},
		{
			name: "fish completion",
			args: []string{"completion", "fish"},
			want: []string{"complete -c dotty"},
		},
		{
			name: "powershell completion",
			args: []string{"completion", "powershell"},
			want: []string{"Register-ArgumentCompleter"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := executeCommand(tt.args...)
			if err != nil {
				t.Fatalf("%v failed: %v\nstderr: %s", tt.args, err, stderr)
			}
			if stderr != "" {
				t.Fatalf("expected empty stderr, got %q", stderr)
			}
			for _, want := range tt.want {
				requireOutputContains(t, stdout, want)
			}
			for _, unwanted := range []string{
				"dotty repository is not configured",
				"manifest not found",
			} {
				if strings.Contains(stdout, unwanted) {
					t.Fatalf(
						"repository-independent output should not contain %q:\n%s",
						unwanted,
						stdout,
					)
				}
			}
		})
	}
}

func TestStatusStateFilter(t *testing.T) {
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

	if _, errOut, err := executeCommand("--repo", repo, "status", "--state", "nope"); err == nil {
		t.Fatal("expected invalid state to fail")
	} else if !strings.Contains(err.Error(), "unsupported status state \"nope\"") {
		t.Fatalf("unexpected invalid-state error: %v\nstderr: %s", err, errOut)
	}

	t.Run("single-state", func(t *testing.T) {
		out, errOut, err := executeCommand("--repo", repo, "status", "--state", "linked")
		if err != nil {
			t.Fatalf("status --state linked failed: %v\nstderr: %s", err, errOut)
		}
		want := fmt.Sprintf(
			"Repository: ~/dotfiles\n\n%-24s %s\n\nSummary: 1 package: 1 linked\n",
			"zsh",
			"LINKED",
		)
		if out != want {
			t.Fatalf("unexpected linked-only output\nwant:\n%s\ngot:\n%s", want, out)
		}
	})

	t.Run("union", func(t *testing.T) {
		out, errOut, err := executeCommand(
			"--repo",
			repo,
			"status",
			"--state",
			"linked",
			"--state",
			"unlinked",
		)
		if err != nil {
			t.Fatalf("status --state linked --state unlinked failed: %v\nstderr: %s", err, errOut)
		}
		want := fmt.Sprintf(
			"Repository: ~/dotfiles\n\n%-24s %s\n%-24s %s\n\nSummary: 2 packages: 1 linked, 1 unlinked\n",
			"tmux",
			"UNLINKED",
			"zsh",
			"LINKED",
		)
		if out != want {
			t.Fatalf("unexpected union output\nwant:\n%s\ngot:\n%s", want, out)
		}
	})

	t.Run("repeated-state", func(t *testing.T) {
		out, errOut, err := executeCommand(
			"--repo",
			repo,
			"status",
			"--state",
			"linked",
			"--state",
			"linked",
		)
		if err != nil {
			t.Fatalf("status with repeated linked state failed: %v\nstderr: %s", err, errOut)
		}
		want := fmt.Sprintf(
			"Repository: ~/dotfiles\n\n%-24s %s\n\nSummary: 1 package: 1 linked\n",
			"zsh",
			"LINKED",
		)
		if out != want {
			t.Fatalf("unexpected repeated-state output\nwant:\n%s\ngot:\n%s", want, out)
		}
	})

	t.Run("untracked-only", func(t *testing.T) {
		out, errOut, err := executeCommand("--repo", repo, "status", "--state", "untracked")
		if err != nil {
			t.Fatalf("status --state untracked failed: %v\nstderr: %s", err, errOut)
		}
		want := fmt.Sprintf(
			"Repository: ~/dotfiles\n\n%s\n  %s\n  %s\n\nSummary: 0 packages; 2 untracked\n",
			"UNTRACKED",
			"ghostty",
			"zsh/.zprofile",
		)
		if out != want {
			t.Fatalf("unexpected untracked-only output\nwant:\n%s\ngot:\n%s", want, out)
		}
	})

	t.Run("positional-package-untracked-only", func(t *testing.T) {
		out, errOut, err := executeCommand("--repo", repo, "status", "zsh", "--state", "untracked")
		if err != nil {
			t.Fatalf("status zsh --state untracked failed: %v\nstderr: %s", err, errOut)
		}
		want := fmt.Sprintf(
			"Repository: ~/dotfiles\n\n%-18s %-20s %-36s %s\n\nSummary: 0 packages; 1 untracked\n",
			"zsh",
			".zprofile",
			"-",
			"UNTRACKED",
		)
		if out != want {
			t.Fatalf("unexpected positional untracked output\nwant:\n%s\ngot:\n%s", want, out)
		}
	})

	t.Run("multi-positional-package-untracked-only", func(t *testing.T) {
		out, errOut, err := executeCommand(
			"--repo",
			repo,
			"status",
			"zsh",
			"tmux",
			"--state",
			"untracked",
		)
		if err != nil {
			t.Fatalf("status zsh tmux --state untracked failed: %v\nstderr: %s", err, errOut)
		}
		want := fmt.Sprintf(
			"Repository: ~/dotfiles\n\n%-18s %-20s %-36s %s\n\nSummary: 0 packages; 1 untracked\n",
			"zsh",
			".zprofile",
			"-",
			"UNTRACKED",
		)
		if out != want {
			t.Fatalf("unexpected multi positional untracked output\nwant:\n%s\ngot:\n%s", want, out)
		}
	})

	t.Run("repeated-positional-package", func(t *testing.T) {
		out, errOut, err := executeCommand("--repo", repo, "status", "zsh", "zsh")
		if err != nil {
			t.Fatalf("status with repeated zsh package failed: %v\nstderr: %s", err, errOut)
		}
		want := fmt.Sprintf(
			"Repository: ~/dotfiles\n\n%-24s %s\n%-24s %s\n\nSummary: 2 packages: 2 linked\n",
			"zsh",
			"LINKED",
			"zsh",
			"LINKED",
		)
		if out != want {
			t.Fatalf(
				"unexpected repeated positional package output\nwant:\n%s\ngot:\n%s",
				want,
				out,
			)
		}
	})

	t.Run("positional-package", func(t *testing.T) {
		out, errOut, err := executeCommand("--repo", repo, "status", "zsh", "--state", "linked")
		if err != nil {
			t.Fatalf("status zsh --state linked failed: %v\nstderr: %s", err, errOut)
		}
		want := fmt.Sprintf(
			"Repository: ~/dotfiles\n\n%-18s %-20s %-36s %s\n\nSummary: 1 package: 1 linked\n",
			"zsh",
			".zshrc",
			"~/.zshrc",
			"LINKED",
		)
		if out != want {
			t.Fatalf("unexpected positional-package output\nwant:\n%s\ngot:\n%s", want, out)
		}
	})

	t.Run("verbose-filtered", func(t *testing.T) {
		out, errOut, err := executeCommand(
			"--repo",
			repo,
			"status",
			"--verbose",
			"--state",
			"linked",
		)
		if err != nil {
			t.Fatalf("status --verbose --state linked failed: %v\nstderr: %s", err, errOut)
		}
		want := fmt.Sprintf(
			"Repository: ~/dotfiles\n\n%-18s %-20s %-36s %s\n\nSummary: 1 package: 1 linked\n",
			"zsh",
			".zshrc",
			"~/.zshrc",
			"LINKED",
		)
		if out != want {
			t.Fatalf("unexpected verbose filtered output\nwant:\n%s\ngot:\n%s", want, out)
		}
	})

	if out, errOut, err := executeCommand(
		"--repo",
		repo,
		"status",
		"--state",
		"missing-source",
	); err != nil {
		t.Fatalf("status with missing-source failed: %v\nstderr: %s", err, errOut)
	} else if out == "" {
		t.Fatal("expected status output for accepted state")
	}
}

func TestStatusStateCompletion(t *testing.T) {
	_, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)

	choices, directive, errOut, err := executeCompletionResult(
		"--repo",
		repo,
		"status",
		"--state",
		"",
	)
	if err != nil {
		t.Fatalf("status state completion failed: %v\nstderr: %s", err, errOut)
	}
	requireChoices(
		t,
		choices,
		[]string{
			"linked",
			"unlinked",
			"partial",
			"conflict",
			"blocked",
			"missing-source",
			"empty",
			"untracked",
		},
	)
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf(
			"unexpected status state directive: want %d, got %d",
			cobra.ShellCompDirectiveNoFileComp,
			directive,
		)
	}

	choices, directive, errOut, err = executeCompletionResult(
		"--repo",
		repo,
		"status",
		"--state",
		"linked",
		"--state",
		"",
	)
	if err != nil {
		t.Fatalf("status repeated state completion failed: %v\nstderr: %s", err, errOut)
	}
	requireChoices(
		t,
		choices,
		[]string{
			"unlinked",
			"partial",
			"conflict",
			"blocked",
			"missing-source",
			"empty",
			"untracked",
		},
	)
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf(
			"unexpected repeated state directive: want %d, got %d",
			cobra.ShellCompDirectiveNoFileComp,
			directive,
		)
	}

	choices, directive, errOut, err = executeCompletionResult(
		"--repo",
		repo,
		"status",
		"--state",
		"m",
	)
	if err != nil {
		t.Fatalf("status state prefix completion failed: %v\nstderr: %s", err, errOut)
	}
	requireChoices(t, choices, []string{"missing-source"})
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf(
			"unexpected state prefix directive: want %d, got %d",
			cobra.ShellCompDirectiveNoFileComp,
			directive,
		)
	}
}

func TestHelpOutputUsesConsistentSections(t *testing.T) {
	out, errOut, err := executeCommand("--help")
	if err != nil {
		t.Fatalf("help failed: %v\nstderr: %s", err, errOut)
	}

	for _, want := range []string{
		"Sync configuration files across machines using a manifest\n\n",
		"Usage:\n  dotty [command]\n",
		"Commands:\n",
		"Options:\n",
		"Global options:\n",
		"Use `dotty [command] --help` for more information about a command.\n",
	} {
		requireOutputContains(t, out, want)
	}
	optionsStart := strings.Index(out, "Options:")
	globalStart := strings.Index(out, "Global options:")
	if optionsStart == -1 || globalStart == -1 || optionsStart > globalStart {
		t.Fatalf("expected Options before Global options, got %q", out)
	}
	if strings.Contains(out[optionsStart:globalStart], "--repo") {
		t.Fatalf("root persistent --repo flag should render under Global options, got %q", out)
	}
	if !strings.Contains(out[globalStart:], "--repo string") {
		t.Fatalf("Global options should include --repo, got %q", out)
	}
	for _, unwanted := range []string{"Available Commands:", "Flags:", "Global Flags:"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("help should not contain %q, got %q", unwanted, out)
		}
	}
}

func TestCommandHelpUsesOptionsAndGlobalOptions(t *testing.T) {
	out, errOut, err := executeCommand("list", "--help")
	if err != nil {
		t.Fatalf("list help failed: %v\nstderr: %s", err, errOut)
	}

	for _, want := range []string{
		"List packages and collections defined in the manifest\n\n",
		"Usage:\n  dotty list [<package>] [flags]\n",
		"Options:\n",
		"Global options:\n",
	} {
		requireOutputContains(t, out, want)
	}
	for _, unwanted := range []string{"Flags:", "Global Flags:"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("command help should not contain %q, got %q", unwanted, out)
		}
	}
}

func TestHelpOutputStylesSectionHeadings(t *testing.T) {
	previousProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(previousProfile) })

	out, errOut, err := executeCommandRaw("--help")
	if err != nil {
		t.Fatalf("help failed: %v\nstderr: %s", err, errOut)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("expected styled help output, got %q", out)
	}
	if !strings.Contains(stripANSI(out), "Usage:\n  dotty [command]\n") {
		t.Fatalf("styled help should preserve stripped text, got %q", stripANSI(out))
	}
}

func TestRenderErrorFormatsUsageAndHints(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "usage",
			err:  fmt.Errorf("usage: dotty link <selector>... | --all | --collection <collection>"),
			want: "error: invalid arguments\n" +
				"  usage: dotty link <selector>... | --all | --collection <collection>\n",
		},
		{
			name: "hint",
			err:  fmt.Errorf("unknown package %q (run `dotty list` to see packages)", "tmux"),
			want: "error: unknown package \"tmux\"\n" +
				"hint: run `dotty list` to see packages\n",
		},
		{
			name: "semicolon hint",
			err: fmt.Errorf(
				"manifest not found at /tmp/dotfiles/dotty.toml; run `dotty init /tmp/dotfiles`",
			),
			want: "error: manifest not found at /tmp/dotfiles/dotty.toml\n" +
				"hint: run `dotty init /tmp/dotfiles`\n",
		},
		{
			name: "choose hint",
			err:  fmt.Errorf("target /tmp/missing does not exist (choose an existing target)"),
			want: "error: target /tmp/missing does not exist\n" +
				"hint: choose an existing target\n",
		},
		{
			name: "remove hint",
			err: fmt.Errorf(
				"target /tmp/file still exists and is not a symlink (remove or move it aside before adding)",
			),
			want: "error: target /tmp/file still exists and is not a symlink\n" +
				"hint: remove or move it aside before adding\n",
		},
		{
			name: "inspect hint",
			err: fmt.Errorf(
				"target /tmp/file is not an expected dotty link (inspect with `dotty status` or remove it manually)",
			),
			want: "error: target /tmp/file is not an expected dotty link\n" +
				"hint: inspect with `dotty status` or remove it manually\n",
		},
		{
			name: "non-hint parenthetical",
			err:  fmt.Errorf("link failed (rollback failed: remove backup)"),
			want: "error: link failed (rollback failed: remove backup)\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			RenderError(&out, tt.err)
			if got := stripANSI(out.String()); got != tt.want {
				t.Fatalf("unexpected error output\nwant: %q\ngot:  %q", tt.want, got)
			}
		})
	}
}

func TestRenderErrorStylesLabels(t *testing.T) {
	previousProfile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(previousProfile) })

	var out bytes.Buffer
	RenderError(&out, fmt.Errorf("unknown package %q (run `dotty list` to see packages)", "tmux"))
	if got := out.String(); !strings.Contains(got, "\x1b[") {
		t.Fatalf("expected styled error output, got %q", got)
	}
	if got := stripANSI(out.String()); !strings.Contains(got, "hint: run `dotty list`") {
		t.Fatalf("styled error should preserve stripped text, got %q", got)
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

func TestRepoCommandExpandsRelativeRepoFlag(t *testing.T) {
	home := setupCLIHomeOnly(t)
	t.Chdir(home)

	out, errOut, err := executeCommand("--repo", "dotfiles", "repo")
	if err != nil {
		t.Fatalf("repo failed: %v\nstderr: %s", err, errOut)
	}

	want := "Repository: ~/dotfiles\nConfig: ~/.config/dotty/config.toml\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
}

func TestRepoCommandUsesCustomXDGConfigHome(t *testing.T) {
	home := setupCLIHomeOnly(t)
	xdg := filepath.Join(t.TempDir(), "xdg-config")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	writeConfig(
		t,
		filepath.Join(xdg, "dotty", "config.toml"),
		fmt.Sprintf("repo = %q\n", filepath.Join(home, "dotfiles")),
	)

	out, errOut, err := executeCommand("repo")
	if err != nil {
		t.Fatalf("repo failed: %v\nstderr: %s", err, errOut)
	}

	want := fmt.Sprintf(
		"Repository: ~/dotfiles\nConfig: %s\n",
		filepath.Join(xdg, "dotty", "config.toml"),
	)
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
}

func TestRepoCommandReportsMalformedConfigWithoutUsage(t *testing.T) {
	home := setupCLIHomeOnly(t)
	configPath := filepath.Join(home, ".config", "dotty", "config.toml")
	writeConfig(t, configPath, "repo = [\n")

	stdout, rendered, err := executeCommandRenderedError(t, "repo")
	if err == nil {
		t.Fatal("expected malformed config error")
	}
	if stdout != "" {
		t.Fatalf("expected empty stdout, got %q", stdout)
	}
	if !strings.Contains(rendered, "error: parse config "+configPath) {
		t.Fatalf("expected parse config diagnostic for %s, got %q", configPath, rendered)
	}
	requireNoUsageDiagnostic(t, rendered)
}

func TestCompletionCommandGeneratesShellScripts(t *testing.T) {
	tests := []struct {
		shell string
		want  string
	}{
		{shell: "bash", want: "__start_dotty"},
		{shell: "zsh", want: "#compdef dotty"},
		{shell: "fish", want: "complete -c dotty"},
		{shell: "powershell", want: "Register-ArgumentCompleter"},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			out, errOut, err := executeCommand("completion", tt.shell)
			if err != nil {
				t.Fatalf("completion %s failed: %v\nstderr: %s", tt.shell, err, errOut)
			}
			if errOut != "" {
				t.Fatalf("expected no stderr, got %q", errOut)
			}
			requireOutputContains(t, out, tt.want)
		})
	}
}

func TestCompletionCommandCompletesSupportedShellNames(t *testing.T) {
	setupCLIHomeOnly(t)

	choices, directive, errOut, err := executeCompletionResult("completion", "")
	if err != nil {
		t.Fatalf("completion shell completion failed: %v\nstderr: %s", err, errOut)
	}
	requireChoices(t, choices, []string{"bash", "zsh", "fish", "powershell"})
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf(
			"unexpected shell directive: want %d, got %d",
			cobra.ShellCompDirectiveNoFileComp,
			directive,
		)
	}

	choices, directive, errOut, err = executeCompletionResult("completion", "p")
	if err != nil {
		t.Fatalf("completion shell prefix completion failed: %v\nstderr: %s", err, errOut)
	}
	requireChoices(t, choices, []string{"powershell"})
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf(
			"unexpected shell prefix directive: want %d, got %d",
			cobra.ShellCompDirectiveNoFileComp,
			directive,
		)
	}
}

func TestPackageCommandsCompleteManifestPackages(t *testing.T) {
	_, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)

	for _, command := range []string{"link", "unlink", "status"} {
		t.Run(command, func(t *testing.T) {
			choices, directive, errOut, err := executeCompletionResult("--repo", repo, command, "")
			if err != nil {
				t.Fatalf("%s completion failed: %v\nstderr: %s", command, err, errOut)
			}
			requireChoices(t, choices, []string{"tmux", "zsh", "zsh/.zshrc"})
			if directive != cobra.ShellCompDirectiveNoFileComp {
				t.Fatalf(
					"%s directive mismatch: want %d, got %d",
					command,
					cobra.ShellCompDirectiveNoFileComp,
					directive,
				)
			}

			choices, directive, errOut, err = executeCompletionResult("--repo", repo, command, "t")
			if err != nil {
				t.Fatalf("%s prefix completion failed: %v\nstderr: %s", command, err, errOut)
			}
			requireChoices(t, choices, []string{"tmux"})
			if directive != cobra.ShellCompDirectiveNoFileComp {
				t.Fatalf(
					"%s prefix directive mismatch: want %d, got %d",
					command,
					cobra.ShellCompDirectiveNoFileComp,
					directive,
				)
			}
		})
	}
}

func TestManifestBackedCompletionsFailClosed(t *testing.T) {
	home := setupCLIHomeOnly(t)
	missingRepo := filepath.Join(home, "missing-dotfiles")
	missingManifestRepo := filepath.Join(home, "missing-manifest")
	if err := os.MkdirAll(missingManifestRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	invalidManifestRepo := filepath.Join(home, "invalid-manifest")
	if err := os.MkdirAll(invalidManifestRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, invalidManifestRepo, "version = \"not-a-number\"\n")

	tests := []struct {
		name string
		repo string
		args []string
	}{
		{
			name: "missing repository package",
			repo: missingRepo,
			args: []string{"link", ""},
		},
		{
			name: "missing repository collection",
			repo: missingRepo,
			args: []string{"link", "--collection", ""},
		},
		{
			name: "missing manifest package",
			repo: missingManifestRepo,
			args: []string{"unlink", ""},
		},
		{
			name: "missing manifest collection",
			repo: missingManifestRepo,
			args: []string{"unlink", "--collection", ""},
		},
		{
			name: "invalid manifest package",
			repo: invalidManifestRepo,
			args: []string{"status", ""},
		},
		{
			name: "invalid manifest collection",
			repo: invalidManifestRepo,
			args: []string{"link", "--collection", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"--repo", tt.repo}, tt.args...)
			choices, directive, errOut, err := executeCompletionResult(args...)
			if err != nil {
				t.Fatalf("completion failed: %v\nstderr: %s", err, errOut)
			}
			requireCompletionEndedOnly(t, errOut)
			requireChoices(t, choices, nil)
			if directive != cobra.ShellCompDirectiveNoFileComp {
				t.Fatalf(
					"unexpected fail-closed directive: want %d, got %d",
					cobra.ShellCompDirectiveNoFileComp,
					directive,
				)
			}
		})
	}
}

func TestPackageCompletionOmitsAlreadySelectedPackages(t *testing.T) {
	_, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)

	for _, command := range []string{"link", "unlink", "status"} {
		t.Run(command, func(t *testing.T) {
			choices, errOut, err := executeCompletion("--repo", repo, command, "tmux", "")
			if err != nil {
				t.Fatalf("%s completion failed: %v\nstderr: %s", command, err, errOut)
			}
			requireChoices(t, choices, []string{"zsh", "zsh/.zshrc"})
		})
	}
}

func TestCollectionFlagCompletesManifestCollections(t *testing.T) {
	_, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]

[collections.desktop]
packages = ["zsh"]

[collections.terminal]
packages = ["tmux", "zsh"]
`)

	for _, command := range []string{"link", "unlink"} {
		t.Run(command, func(t *testing.T) {
			choices, directive, errOut, err := executeCompletionResult(
				"--repo",
				repo,
				command,
				"--collection",
				"",
			)
			if err != nil {
				t.Fatalf("%s collection completion failed: %v\nstderr: %s", command, err, errOut)
			}
			requireChoices(t, choices, []string{"desktop", "terminal"})
			if directive != cobra.ShellCompDirectiveNoFileComp {
				t.Fatalf(
					"%s collection directive mismatch: want %d, got %d",
					command,
					cobra.ShellCompDirectiveNoFileComp,
					directive,
				)
			}

			choices, directive, errOut, err = executeCompletionResult(
				"--repo",
				repo,
				command,
				"--collection",
				"term",
			)
			if err != nil {
				t.Fatalf(
					"%s collection prefix completion failed: %v\nstderr: %s",
					command,
					err,
					errOut,
				)
			}
			requireChoices(t, choices, []string{"terminal"})
			if directive != cobra.ShellCompDirectiveNoFileComp {
				t.Fatalf(
					"%s collection prefix directive mismatch: want %d, got %d",
					command,
					cobra.ShellCompDirectiveNoFileComp,
					directive,
				)
			}

			choices, directive, errOut, err = executeCompletionResult(
				"--repo",
				repo,
				command,
				"--collection",
				"desktop",
				"--collection",
				"",
			)
			if err != nil {
				t.Fatalf(
					"%s repeated collection completion failed: %v\nstderr: %s",
					command,
					err,
					errOut,
				)
			}
			requireChoices(t, choices, []string{"terminal"})
			if directive != cobra.ShellCompDirectiveNoFileComp {
				t.Fatalf(
					"%s repeated collection directive mismatch: want %d, got %d",
					command,
					cobra.ShellCompDirectiveNoFileComp,
					directive,
				)
			}

			choices, directive, errOut, err = executeCompletionResult(
				"--repo",
				repo,
				command,
				"--all",
				"--collection",
				"",
			)
			if err != nil {
				t.Fatalf(
					"%s collection completion with --all failed: %v\nstderr: %s",
					command,
					err,
					errOut,
				)
			}
			requireChoices(t, choices, []string{"desktop", "terminal"})
			if directive != cobra.ShellCompDirectiveNoFileComp {
				t.Fatalf(
					"%s collection --all directive mismatch: want %d, got %d",
					command,
					cobra.ShellCompDirectiveNoFileComp,
					directive,
				)
			}
		})
	}
}

func TestAddCompletesPathThenManifestPackages(t *testing.T) {
	_, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.scripts]
links = []

[packages.tmux]
links = []
`)

	choices, directive, errOut, err := executeCompletionResult("--repo", repo, "add", "")
	if err != nil {
		t.Fatalf("add path completion failed: %v\nstderr: %s", err, errOut)
	}
	requireChoices(t, choices, nil)
	if directive != cobra.ShellCompDirectiveDefault {
		t.Fatalf(
			"unexpected first argument directive: want %d, got %d",
			cobra.ShellCompDirectiveDefault,
			directive,
		)
	}

	choices, directive, errOut, err = executeCompletionResult(
		"--repo",
		repo,
		"add",
		"~/.local/bin/sesh-fzf",
		"",
	)
	if err != nil {
		t.Fatalf("add package completion failed: %v\nstderr: %s", err, errOut)
	}
	requireChoices(t, choices, []string{"scripts", "tmux"})
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf(
			"unexpected package directive: want %d, got %d",
			cobra.ShellCompDirectiveNoFileComp,
			directive,
		)
	}
}

func TestSelectorCommandsCompletePackagesAndPackageSources(t *testing.T) {
	_, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
  { source = "office/docx2pdf", target = "~/.local/bin/office-docx2pdf" },
]

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`)
	if err := os.MkdirAll(filepath.Join(repo, "scripts", "office"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTextFile(t, filepath.Join(repo, "scripts", "unused"), "unused\n")
	writeTextFile(t, filepath.Join(repo, "scripts", "office", "helper"), "helper\n")
	writeTextFile(t, filepath.Join(repo, "zsh", ".zprofile"), "export PATH\n")

	want := []string{
		"scripts",
		"scripts/docx2pdf",
		"scripts/office",
		"scripts/office/docx2pdf",
		"scripts/office/helper",
		"scripts/unused",
		"zsh",
		"zsh/.zprofile",
		"zsh/.zshrc",
	}
	for _, command := range []string{"link", "unlink", "status"} {
		t.Run(command, func(t *testing.T) {
			choices, directive, errOut, err := executeCompletionResult("--repo", repo, command, "")
			if err != nil {
				t.Fatalf("%s selector completion failed: %v\nstderr: %s", command, err, errOut)
			}
			requireChoices(t, choices, want)
			if directive != cobra.ShellCompDirectiveNoFileComp {
				t.Fatalf(
					"%s selector directive mismatch: want %d, got %d",
					command,
					cobra.ShellCompDirectiveNoFileComp,
					directive,
				)
			}
		})
	}
}

func TestTrackCompletesRepoPackageSourcesAndFilesystemTargets(t *testing.T) {
	_, repo := setupCLITest(t)
	writeManifest(t, repo, "version = 1\n")
	if err := os.MkdirAll(filepath.Join(repo, "scripts", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTextFile(t, filepath.Join(repo, "scripts", "docx2pdf"), "docx2pdf\n")
	writeTextFile(t, filepath.Join(repo, "scripts", "nested", "ocli"), "ocli\n")

	choices, directive, errOut, err := executeCompletionResult("--repo", repo, "track", "")
	if err != nil {
		t.Fatalf("track selector completion failed: %v\nstderr: %s", err, errOut)
	}
	requireChoices(
		t,
		choices,
		[]string{"scripts", "scripts/docx2pdf", "scripts/nested", "scripts/nested/ocli"},
	)
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf(
			"unexpected track selector directive: want %d, got %d",
			cobra.ShellCompDirectiveNoFileComp,
			directive,
		)
	}

	choices, directive, errOut, err = executeCompletionResult(
		"--repo",
		repo,
		"track",
		"scripts/docx2pdf",
		"",
	)
	if err != nil {
		t.Fatalf("track positional target completion failed: %v\nstderr: %s", err, errOut)
	}
	requireChoices(t, choices, nil)
	if directive != cobra.ShellCompDirectiveDefault {
		t.Fatalf(
			"unexpected track target directive: want %d, got %d",
			cobra.ShellCompDirectiveDefault,
			directive,
		)
	}
}

func TestUntrackCompletesManifestSelectorsAndMappedTargets(t *testing.T) {
	_, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
  { source = "nested/ocli", target = "~/.local/bin/ocli" },
]
`)
	if err := os.MkdirAll(filepath.Join(repo, "scripts", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTextFile(t, filepath.Join(repo, "scripts", "unused"), "unused\n")

	choices, directive, errOut, err := executeCompletionResult("--repo", repo, "untrack", "")
	if err != nil {
		t.Fatalf("untrack selector completion failed: %v\nstderr: %s", err, errOut)
	}
	requireChoices(t, choices, []string{"scripts", "scripts/docx2pdf", "scripts/nested/ocli"})
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf(
			"unexpected untrack selector directive: want %d, got %d",
			cobra.ShellCompDirectiveNoFileComp,
			directive,
		)
	}

	choices, directive, errOut, err = executeCompletionResult(
		"--repo",
		repo,
		"untrack",
		"scripts/docx2pdf",
		"",
	)
	if err != nil {
		t.Fatalf("untrack target completion failed: %v\nstderr: %s", err, errOut)
	}
	requireChoices(t, choices, []string{"~/.local/bin/docx2pdf"})
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf(
			"unexpected untrack target directive: want %d, got %d",
			cobra.ShellCompDirectiveNoFileComp,
			directive,
		)
	}
}

func TestListCompletesPackagesOnly(t *testing.T) {
	_, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]

[packages.zsh]
links = []
`)
	if err := os.MkdirAll(filepath.Join(repo, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTextFile(t, filepath.Join(repo, "scripts", "unused"), "unused\n")

	choices, directive, errOut, err := executeCompletionResult("--repo", repo, "list", "")
	if err != nil {
		t.Fatalf("list completion failed: %v\nstderr: %s", err, errOut)
	}
	requireChoices(t, choices, []string{"scripts", "zsh"})
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf(
			"unexpected list directive: want %d, got %d",
			cobra.ShellCompDirectiveNoFileComp,
			directive,
		)
	}
}

func TestTargetCompletionSuppressesInvalidCombinations(t *testing.T) {
	_, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]

[collections.terminal]
packages = ["scripts", "zsh"]
`)

	for _, args := range [][]string{
		{"link", "--all", "scripts", "--target", ""},
		{"link", "--collection", "terminal", "--target", ""},
		{"link", "--track", "scripts/docx2pdf", "zsh/.zshrc", "--target", ""},
		{"unlink", "--all", "--target", ""},
		{"unlink", "--collection", "terminal", "--target", ""},
		{"unlink", "--untrack", "scripts/docx2pdf", "zsh/.zshrc", "--target", ""},
	} {
		choices, directive, errOut, err := executeCompletionResult(
			append([]string{"--repo", repo}, args...)...)
		if err != nil {
			t.Fatalf("target completion failed for %v: %v\nstderr: %s", args, err, errOut)
		}
		requireChoices(t, choices, nil)
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Fatalf(
				"unexpected invalid target directive for %v: want %d, got %d",
				args,
				cobra.ShellCompDirectiveNoFileComp,
				directive,
			)
		}
	}
}

func TestTargetFlagsUseFilesystemCompletion(t *testing.T) {
	_, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
  { source = "sesh-fzf", target = "~/.local/bin/sesh-fzf" },
]

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]
`)

	tests := []struct {
		name string
		args []string
	}{
		{name: "track", args: []string{"track", "scripts/docx2pdf", "--target", ""}},
		{name: "untrack", args: []string{"untrack", "scripts/docx2pdf", "--target", ""}},
		{name: "link", args: []string{"link", "scripts", "--target", ""}},
		{name: "unlink", args: []string{"unlink", "scripts", "--target", "~/.local/bin/s"}},
		{
			name: "repeated target still filesystem",
			args: []string{"link", "scripts", "--target", "~/.local/bin/docx2pdf", "--target", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"--repo", repo}, tt.args...)
			choices, directive, errOut, err := executeCompletionResult(args...)
			if err != nil {
				t.Fatalf("target completion failed: %v\nstderr: %s", err, errOut)
			}
			requireChoices(t, choices, nil)
			if directive != cobra.ShellCompDirectiveDefault {
				t.Fatalf(
					"unexpected target directive: want %d, got %d",
					cobra.ShellCompDirectiveDefault,
					directive,
				)
			}
		})
	}
}

func TestNoArgumentCommandsReturnNoFileCompletions(t *testing.T) {
	setupCLIHomeOnly(t)

	for _, command := range []string{"list", "repo", "version"} {
		t.Run(command, func(t *testing.T) {
			choices, directive, errOut, err := executeCompletionResult(command, "")
			if err != nil {
				t.Fatalf("%s no-argument completion failed: %v\nstderr: %s", command, err, errOut)
			}
			requireChoices(t, choices, nil)
			if directive != cobra.ShellCompDirectiveNoFileComp {
				t.Fatalf(
					"%s directive mismatch: want %d, got %d",
					command,
					cobra.ShellCompDirectiveNoFileComp,
					directive,
				)
			}
		})
	}
}

func TestPathArgsAndRepoFlagCompleteDirectories(t *testing.T) {
	cmd := NewRootCommand(io.Discard, io.Discard)
	initCmd, _, err := cmd.Find([]string{"init"})
	if err != nil {
		t.Fatal(err)
	}
	if initCmd.ValidArgsFunction == nil {
		t.Fatalf("init command missing completion function")
	}
	_, directive := initCmd.ValidArgsFunction(initCmd, nil, "")
	if directive != cobra.ShellCompDirectiveFilterDirs {
		t.Fatalf(
			"unexpected init directive: want %d, got %d",
			cobra.ShellCompDirectiveFilterDirs,
			directive,
		)
	}

	completion, ok := cmd.GetFlagCompletionFunc("repo")
	if !ok {
		t.Fatalf("--repo flag missing completion function")
	}
	_, directive = completion(cmd, nil, "")
	if directive != cobra.ShellCompDirectiveFilterDirs {
		t.Fatalf(
			"unexpected --repo directive: want %d, got %d",
			cobra.ShellCompDirectiveFilterDirs,
			directive,
		)
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

func TestListPackagePrintsDetailedSourcesAndTargets(t *testing.T) {
	_, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
  { source = "sesh-fzf", target = "~/.local/bin/sesh-fzf" },
]
`)

	out, errOut, err := executeCommand("--repo", repo, "list", "scripts")
	if err != nil {
		t.Fatalf("list scripts failed: %v\nstderr: %s", err, errOut)
	}

	want := "scripts\n" +
		"  docx2pdf -> ~/.local/bin/docx2pdf\n" +
		"  sesh-fzf -> ~/.local/bin/sesh-fzf\n"
	if out != want {
		t.Fatalf("unexpected detailed list output\nwant:\n%s\ngot:\n%s", want, out)
	}
}

func TestListRejectsPackageSourceSelector(t *testing.T) {
	_, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]
`)

	_, _, err := executeCommand("--repo", repo, "list", "scripts/docx2pdf")
	if err == nil ||
		!strings.Contains(err.Error(), "list accepts packages, not package/source selectors") {
		t.Fatalf("expected package-only list error, got %v", err)
	}
}

func TestListPrintsEmptyInventory(t *testing.T) {
	_, repo := setupCLITest(t)
	writeManifest(t, repo, "version = 1\n")

	out, errOut, err := executeCommand("--repo", repo, "list")
	if err != nil {
		t.Fatalf("list empty inventory failed: %v\nstderr: %s", err, errOut)
	}

	want := "Packages\n  none\n\nCollections\n  none\n"
	if out != want {
		t.Fatalf("unexpected empty list output\nwant:\n%s\ngot:\n%s", want, out)
	}
}

func TestListStatusAndCompletionUseManifestInventoryCoherently(t *testing.T) {
	_, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.zsh]
links = [
  { source = ".", target = "~/.config/zsh" },
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

	listOut, errOut, err := executeCommand("--repo", repo, "list")
	if err != nil {
		t.Fatalf("list failed: %v\nstderr: %s", err, errOut)
	}
	wantList := fmt.Sprintf(
		"Packages\n  %-24s %s\n  %-24s %s\n\nCollections\n  %-24s %s\n",
		"tmux",
		"1 link",
		"zsh",
		"1 link",
		"terminal",
		"tmux, zsh",
	)
	if listOut != wantList {
		t.Fatalf("unexpected coherent list output\nwant:\n%s\ngot:\n%s", wantList, listOut)
	}

	statusOut, errOut, err := executeCommand("--repo", repo, "status")
	if err != nil {
		t.Fatalf("status failed: %v\nstderr: %s", err, errOut)
	}
	wantStatus := fmt.Sprintf(
		"Repository: ~/dotfiles\n\n%-24s %s\n%-24s %s\n\nSummary: 2 packages: 2 unlinked\n",
		"tmux",
		"UNLINKED",
		"zsh",
		"UNLINKED",
	)
	if statusOut != wantStatus {
		t.Fatalf("unexpected coherent status output\nwant:\n%s\ngot:\n%s", wantStatus, statusOut)
	}

	choices, errOut, err := executeCompletion("--repo", repo, "status", "")
	if err != nil {
		t.Fatalf("status completion failed: %v\nstderr: %s", err, errOut)
	}
	requireChoices(t, choices, []string{"tmux", "zsh"})

	choices, errOut, err = executeCompletion("--repo", repo, "link", "--collection", "")
	if err != nil {
		t.Fatalf("collection completion failed: %v\nstderr: %s", err, errOut)
	}
	requireChoices(t, choices, []string{"terminal"})
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

	out, errOut, err = executeCommand("--repo", repo, "status", "zsh")
	if err != nil {
		t.Fatalf("status zsh failed: %v\nstderr: %s", err, errOut)
	}
	want = fmt.Sprintf(
		"Repository: ~/dotfiles\n\n%-18s %-20s %-36s %s\n\n%-18s %-20s %-36s %s\n\nSummary: 1 package: 1 linked; 1 untracked\n",
		"zsh",
		".zshrc",
		"~/.zshrc",
		"LINKED",
		"zsh",
		".zprofile",
		"-",
		"UNTRACKED",
	)
	if out != want {
		t.Fatalf("unexpected single-package status output\nwant:\n%s\ngot:\n%s", want, out)
	}

	out, errOut, err = executeCommand("--repo", repo, "status", "tmux", "zsh")
	if err != nil {
		t.Fatalf("status tmux zsh failed: %v\nstderr: %s", err, errOut)
	}
	want = fmt.Sprintf(
		"Repository: ~/dotfiles\n\n%-24s %s\n%-24s %s\n\nSummary: 2 packages: 1 linked, 1 unlinked\n",
		"tmux",
		"UNLINKED",
		"zsh",
		"LINKED",
	)
	if out != want {
		t.Fatalf("unexpected multi-package status output\nwant:\n%s\ngot:\n%s", want, out)
	}

	out, errOut, err = executeCommand("--repo", repo, "status", "--verbose", "tmux", "zsh")
	if err != nil {
		t.Fatalf("status --verbose tmux zsh failed: %v\nstderr: %s", err, errOut)
	}
	want = fmt.Sprintf(
		"Repository: ~/dotfiles\n\n%-18s %-20s %-36s %s\n%-18s %-20s %-36s %s\n\n%-18s %-20s %-36s %s\n\nSummary: 2 packages: 1 linked, 1 unlinked; 1 untracked\n",
		"tmux",
		".",
		"~/.config/tmux",
		"UNLINKED",
		"zsh",
		".zshrc",
		"~/.zshrc",
		"LINKED",
		"zsh",
		".zprofile",
		"-",
		"UNTRACKED",
	)
	if out != want {
		t.Fatalf("unexpected verbose multi-package status output\nwant:\n%s\ngot:\n%s", want, out)
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
		"zsh",
		".zprofile",
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

func TestLinkPackageAndCollectionSelectorsDeduplicateInOrder(t *testing.T) {
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

	out, errOut, err := executeCommand("--repo", repo, "link", "zsh", "--collection", "terminal")
	if err != nil {
		t.Fatalf("link package plus collection failed: %v\nstderr: %s", err, errOut)
	}
	want := "linked zsh/.zshrc -> ~/.zshrc\n" +
		"linked tmux -> ~/.config/tmux\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
	assertSymlink(t, filepath.Join(home, ".zshrc"), filepath.Join(repo, "zsh", ".zshrc"))
	assertSymlink(t, filepath.Join(home, ".config", "tmux"), filepath.Join(repo, "tmux"))
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
	want := "linked tmux -> ~/.config/tmux\n" +
		"linked zsh/.zshrc -> ~/.zshrc\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
	assertSymlink(t, filepath.Join(home, ".config", "tmux"), filepath.Join(repo, "tmux"))
	assertSymlink(t, filepath.Join(home, ".zshrc"), filepath.Join(repo, "zsh", ".zshrc"))
}

func TestUnlinkDefaultPrintsLinkRemoved(t *testing.T) {
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

	out, errOut, err := executeCommand("--repo", repo, "unlink", "zsh")
	if err != nil {
		t.Fatalf("unlink failed: %v\nstderr: %s", err, errOut)
	}
	want := "unlinked zsh/.zshrc -> ~/.zshrc\n"
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

	out, errOut, err := executeCommand("--repo", repo, "unlink", "--leave-copy", "--all")
	if err != nil {
		t.Fatalf("unlink --all failed: %v\nstderr: %s", err, errOut)
	}
	want := "unlinked tmux -> ~/.config/tmux (copy left)\n" +
		"unlinked zsh/.zshrc -> ~/.zshrc (copy left)\n"
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

func TestCommandArgumentDiagnosticsCoverInvalidShapes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "init too many args",
			args: []string{"init", "one", "two"},
			want: "usage: dotty init [<path>]",
		},
		{
			name: "add missing args",
			args: []string{"add"},
			want: "usage: dotty add <path> <package>",
		},
		{
			name: "link missing selector",
			args: []string{"link"},
			want: "usage: dotty link <selector>... | --all | --collection <collection>",
		},
		{
			name: "unlink missing selector",
			args: []string{"unlink"},
			want: "usage: dotty unlink <selector>... | --all | --collection <collection>",
		},
		{
			name: "list too many args",
			args: []string{"list", "extra", "again"},
			want: "usage: dotty list [<package>]",
		},
		{name: "repo too many args", args: []string{"repo", "extra"}, want: "usage: dotty repo"},
		{
			name: "completion missing shell",
			args: []string{"completion"},
			want: "usage: dotty completion <bash|zsh|fish|powershell>",
		},
		{
			name: "completion too many args",
			args: []string{"completion", "bash", "extra"},
			want: "usage: dotty completion <bash|zsh|fish|powershell>",
		},
		{
			name: "version too many args",
			args: []string{"version", "extra"},
			want: "usage: dotty version",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, rendered, err := executeCommandRenderedError(t, tt.args...)
			if err == nil {
				t.Fatal("expected argument error")
			}
			if stdout != "" {
				t.Fatalf("expected empty stdout, got %q", stdout)
			}
			want := "error: invalid arguments\n  " + tt.want + "\n"
			if rendered != want {
				t.Fatalf("unexpected rendered diagnostic\nwant: %q\ngot:  %q", want, rendered)
			}
		})
	}
}

func TestCommandDiagnosticsCoverUnknownCommandFlagAndUnsupportedShell(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "unknown command",
			args: []string{"bogus"},
			want: "error: unknown command \"bogus\" for \"dotty\"\n",
		},
		{
			name: "unknown flag",
			args: []string{"status", "--bogus"},
			want: "error: unknown flag: --bogus\n",
		},
		{
			name: "unsupported completion shell",
			args: []string{"completion", "tcsh"},
			want: "error: unsupported shell \"tcsh\"\n" +
				"hint: use bash, zsh, fish, or powershell\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, rendered, err := executeCommandRenderedError(t, tt.args...)
			if err == nil {
				t.Fatal("expected command error")
			}
			if stdout != "" {
				t.Fatalf("expected empty stdout, got %q", stdout)
			}
			if rendered != tt.want {
				t.Fatalf("unexpected rendered diagnostic\nwant: %q\ngot:  %q", tt.want, rendered)
			}
		})
	}
}

func TestRuntimeDiagnosticsRenderWithoutUsage(t *testing.T) {
	t.Run("repository not configured", func(t *testing.T) {
		setupCLIHomeOnly(t)
		stdout, rendered, err := executeCommandRenderedError(t, "list")
		if err == nil {
			t.Fatal("expected repository resolution error")
		}
		if stdout != "" {
			t.Fatalf("expected empty stdout, got %q", stdout)
		}
		want := "error: dotty repository is not configured\n" +
			"hint: run `dotty init <path>` or pass --repo\n"
		if rendered != want {
			t.Fatalf("unexpected rendered diagnostic\nwant: %q\ngot:  %q", want, rendered)
		}
		requireNoUsageDiagnostic(t, rendered)
	})

	t.Run("manifest missing", func(t *testing.T) {
		home := setupCLIHomeOnly(t)
		repo := filepath.Join(home, "missing-manifest")
		if err := os.MkdirAll(repo, 0o755); err != nil {
			t.Fatal(err)
		}
		stdout, rendered, err := executeCommandRenderedError(t, "--repo", repo, "list")
		if err == nil {
			t.Fatal("expected missing Manifest error")
		}
		if stdout != "" {
			t.Fatalf("expected empty stdout, got %q", stdout)
		}
		want := fmt.Sprintf(
			"error: manifest not found at %s\nhint: run `dotty init %s`\n",
			filepath.Join(repo, dotty.ManifestFileName),
			repo,
		)
		if rendered != want {
			t.Fatalf("unexpected rendered diagnostic\nwant: %q\ngot:  %q", want, rendered)
		}
		requireNoUsageDiagnostic(t, rendered)
	})

	t.Run("selection and runtime failures", func(t *testing.T) {
		home, repo := setupCLITest(t)
		manifest := `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]

[collections.terminal]
packages = ["zsh"]
`
		writeManifest(t, repo, manifest)
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
		conflictTarget := filepath.Join(home, ".zshrc")
		if err := os.WriteFile(conflictTarget, []byte("local copy\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		manifestBefore, err := os.ReadFile(dotty.ManifestPath(repo))
		if err != nil {
			t.Fatal(err)
		}

		tests := []struct {
			name string
			args []string
			want string
		}{
			{
				name: "unknown package",
				args: []string{"--repo", repo, "link", "ghostty"},
				want: "error: unknown package \"ghostty\"\n" +
					"hint: run `dotty list` to see packages\n",
			},
			{
				name: "unknown collection",
				args: []string{"--repo", repo, "link", "--collection", "desktop"},
				want: "error: unknown collection \"desktop\"\n" +
					"hint: run `dotty list` to see collections\n",
			},
			{
				name: "invalid status state",
				args: []string{"--repo", repo, "status", "--state", "nope"},
				want: "error: unsupported status state \"nope\" (supported values: linked, unlinked, partial, conflict, blocked, missing-source, empty, untracked)\n",
			},
			{
				name: "target conflict",
				args: []string{"--repo", repo, "link", "zsh"},
				want: "error: target ~/.zshrc already exists\n" +
					"hint: use --force --dry-run to preview replacing it\n",
			},
			{
				name: "missing source",
				args: []string{"--repo", repo, "link", "tmux"},
				want: "error: tmux is missing from the repository\n" +
					"hint: restore it, or run `dotty untrack tmux` to remove the manifest entry\n",
			},
			{
				name: "missing add target",
				args: []string{"--repo", repo, "add", filepath.Join(home, ".missing"), "ghostty"},
				want: fmt.Sprintf(
					"error: target %s does not exist\nhint: choose an existing target\n",
					filepath.Join(home, ".missing"),
				),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				stdout, rendered, err := executeCommandRenderedError(t, tt.args...)
				if err == nil {
					t.Fatal("expected runtime diagnostic")
				}
				if stdout != "" {
					t.Fatalf("expected empty stdout, got %q", stdout)
				}
				if rendered != tt.want {
					t.Fatalf(
						"unexpected rendered diagnostic\nwant: %q\ngot:  %q",
						tt.want,
						rendered,
					)
				}
				requireNoUsageDiagnostic(t, rendered)
			})
		}

		manifestAfter, err := os.ReadFile(dotty.ManifestPath(repo))
		if err != nil {
			t.Fatal(err)
		}
		if string(manifestAfter) != string(manifestBefore) {
			t.Fatalf(
				"diagnostic paths changed Manifest\nbefore: %q\nafter:  %q",
				string(manifestBefore),
				string(manifestAfter),
			)
		}
		if data, err := os.ReadFile(conflictTarget); err != nil || string(data) != "local copy\n" {
			t.Fatalf("target conflict changed: data=%q err=%v", string(data), err)
		}
	})
}

func TestBuiltBinaryWiresMainHelpVersionAndErrors(t *testing.T) {
	binary := buildDottyBinaryForTest(t)

	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "help",
			args:       []string{"--help"},
			wantStdout: "Usage:\n  dotty [command]\n",
		},
		{
			name:       "version flag",
			args:       []string{"--version"},
			wantStdout: "dotty version ",
		},
		{
			name:     "runtime error",
			args:     []string{"list"},
			wantCode: 1,
			wantStderr: "error: dotty repository is not configured\n" +
				"hint: run `dotty init <path>` or pass --repo\n",
		},
		{
			name:     "argument shape error",
			args:     []string{"add"},
			wantCode: 1,
			wantStderr: "error: invalid arguments\n" +
				"  usage: dotty add <path> <package>\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runDottyBinary(t, binary, tt.args...)
			if code != tt.wantCode {
				t.Fatalf(
					"exit code mismatch: want %d, got %d\nstderr: %s",
					tt.wantCode,
					code,
					stderr,
				)
			}
			if tt.wantStdout != "" {
				requireOutputContains(t, stdout, tt.wantStdout)
			} else if stdout != "" {
				t.Fatalf("expected empty stdout, got %q", stdout)
			}
			if stderr != tt.wantStderr {
				t.Fatalf("stderr mismatch\nwant: %q\ngot:  %q", tt.wantStderr, stderr)
			}
		})
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
			want: "usage: dotty link <selector>... | --all | --collection <collection>",
		},
		{
			name: "unlink missing selector",
			args: []string{"unlink"},
			want: "usage: dotty unlink <selector>... | --all | --collection <collection>",
		},
		{
			name: "list too many args",
			args: []string{"list", "extra", "again"},
			want: "usage: dotty list [<package>]",
		},
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
	stdout, stderr, err = executeCommandRaw(args...)
	return stripANSI(stdout), stripANSI(stderr), err
}

func executeCommandRaw(args ...string) (stdout string, stderr string, err error) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := NewRootCommand(&out, &errOut)
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func executeCommandRenderedError(
	t *testing.T,
	args ...string,
) (stdout string, rendered string, err error) {
	t.Helper()
	stdout, stderr, err := executeCommand(args...)
	if stderr != "" {
		t.Fatalf("expected command execution to leave stderr rendering to caller, got %q", stderr)
	}
	if err == nil {
		return stdout, "", nil
	}
	var errOut bytes.Buffer
	RenderError(&errOut, err)
	return stdout, stripANSI(errOut.String()), err
}

func executeCompletion(args ...string) (choices []string, stderr string, err error) {
	choices, _, stderr, err = executeCompletionResult(args...)
	return choices, stderr, err
}

func executeCompletionResult(
	args ...string,
) (choices []string, directive cobra.ShellCompDirective, stderr string, err error) {
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd := NewRootCommand(&out, &errOut)
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append([]string{cobra.ShellCompNoDescRequestCmd}, args...))
	err = cmd.Execute()
	output := stripANSI(out.String())
	return completionChoices(output), completionDirective(output), stripANSI(errOut.String()), err
}

func completionChoices(output string) []string {
	var choices []string
	for line := range strings.SplitSeq(output, "\n") {
		if line == "" || strings.HasPrefix(line, ":") ||
			strings.HasPrefix(line, "Completion ended") {
			continue
		}
		choice := strings.Split(line, "\t")[0]
		choices = append(choices, choice)
	}
	return choices
}

func completionDirective(output string) cobra.ShellCompDirective {
	for line := range strings.SplitSeq(output, "\n") {
		if !strings.HasPrefix(line, ":") {
			continue
		}
		value, err := strconv.Atoi(strings.TrimPrefix(line, ":"))
		if err != nil {
			continue
		}
		return cobra.ShellCompDirective(value)
	}
	return cobra.ShellCompDirectiveDefault
}

func requireChoices(t *testing.T, got []string, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected completions\nwant: %#v\ngot:  %#v", want, got)
	}
}

func snapshotTree(t *testing.T, root string) string {
	t.Helper()
	if _, err := os.Lstat(root); err != nil {
		if os.IsNotExist(err) {
			return "<missing>\n"
		}
		t.Fatal(err)
	}

	var snapshot strings.Builder
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		snapshot.WriteString(rel)
		snapshot.WriteString("\t")
		snapshot.WriteString(info.Mode().String())
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			snapshot.WriteString("\t->")
			snapshot.WriteString(target)
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			snapshot.WriteString("\t")
			snapshot.Write(data)
		}
		snapshot.WriteString("\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.String()
}

func requireSnapshotUnchanged(t *testing.T, root string, before string) {
	t.Helper()
	if after := snapshotTree(t, root); after != before {
		t.Fatalf("filesystem snapshot changed\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func requireCompletionEndedOnly(t *testing.T, stderr string) {
	t.Helper()
	if stderr == "" {
		return
	}
	if strings.TrimSpace(
		stderr,
	) != "Completion ended with directive: ShellCompDirectiveNoFileComp" {
		t.Fatalf("completion should only report its directive, got %q", stderr)
	}
}

func requireNoUsageDiagnostic(t *testing.T, rendered string) {
	t.Helper()
	if strings.Contains(rendered, "usage: dotty") || strings.Contains(rendered, "Usage:") {
		t.Fatalf("runtime diagnostic should not include usage, got %q", rendered)
	}
}

func flagSpecs(flags *pflag.FlagSet) []string {
	specs := []string{}
	flags.VisitAll(func(flag *pflag.Flag) {
		specs = append(specs, flag.Name+":"+flag.Shorthand)
	})
	return specs
}

func buildDottyBinaryForTest(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	binary := filepath.Join(t.TempDir(), "dotty")
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/dotty")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build dotty binary: %v\n%s", err, string(output))
	}
	return binary
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root")
		}
		dir = parent
	}
}

func runDottyBinary(
	t *testing.T,
	binary string,
	args ...string,
) (stdout string, stderr string, code int) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	xdgConfig := filepath.Join(home, ".config")
	if err := os.MkdirAll(xdgConfig, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+xdgConfig, "DOTTY_REPO=")
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	code = 0
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			t.Fatalf("run dotty binary: %v", err)
		}
		code = exitError.ExitCode()
	}
	return stripANSI(out.String()), stripANSI(errOut.String()), code
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

func writeConfig(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
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
