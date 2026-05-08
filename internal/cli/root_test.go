package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dotty/internal/dotty"
)

func TestAddPrintsTargetToPackageSource(t *testing.T) {
	home, repo := setupCLITest(t)
	target := filepath.Join(home, ".config", "tmux")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "tmux.conf"), []byte("set -g mouse on\n"), 0o644); err != nil {
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
	if err := os.WriteFile(filepath.Join(repo, "zsh", ".zshrc"), []byte("source ~/secrets/.zshrc_secrets\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "zsh", ".zshrc_secrets"), []byte("export TOKEN=test\n"), 0o600); err != nil {
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
	assertSymlink(t, filepath.Join(home, "secrets", ".zshrc_secrets"), filepath.Join(repo, "zsh", ".zshrc_secrets"))
}

func TestUnlinkPrintsTargetAction(t *testing.T) {
	home, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]
`)
	if err := os.MkdirAll(filepath.Join(repo, "tmux"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "tmux", "tmux.conf"), []byte("set -g mouse on\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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
	if _, err := dotty.Init(repo); err != nil {
		t.Fatal(err)
	}
	return home, repo
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
	if err := os.WriteFile(filepath.Join(repo, dotty.ManifestFileName), []byte(content), 0o644); err != nil {
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
	replacer := strings.NewReplacer("\x1b[1;32m", "", "\x1b[1;36m", "", "\x1b[35m", "", "\x1b[34m", "", "\x1b[0m", "")
	return replacer.Replace(s)
}
