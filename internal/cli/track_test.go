package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lmilojevicc/dotty/internal/dotty"
)

func writeTextFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func requireFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != want {
		t.Fatalf("file content mismatch for %s: want %q, got %q", path, want, got)
	}
}

func TestLinkTrackCommandAddsMappingAndCreatesLink(t *testing.T) {
	home, repo := setupCLITest(t)
	writeManifest(t, repo, "version = 1\n")
	source := filepath.Join(repo, "scripts", "docx2pdf")
	writeTextFile(t, source, "#!/bin/sh\n")

	out, errOut, err := executeCommand(
		"--repo", repo,
		"link", "scripts/docx2pdf", "--target", "~/.local/bin/docx2pdf", "--track",
	)
	if err != nil {
		t.Fatalf("link --track failed: %v\nstderr: %s", err, errOut)
	}

	if out != "tracked and linked scripts/docx2pdf -> ~/.local/bin/docx2pdf\n" {
		t.Fatalf("unexpected link --track output: %q", out)
	}
	assertSymlink(t, filepath.Join(home, ".local", "bin", "docx2pdf"), source)
	requireFileContent(t, dotty.ManifestPath(repo), `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]
`)
}

func TestLinkAndUnlinkRejectTargetWithAllCollectionOrMultipleSelectors(t *testing.T) {
	_, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]

[collections.one]
packages = ["scripts"]
`)
	writeTextFile(t, filepath.Join(repo, "scripts", "docx2pdf"), "#!/bin/sh\n")
	writeTextFile(t, filepath.Join(repo, "tmux", "tmux.conf"), "set -g mouse on\n")

	tests := []struct {
		name string
		args []string
	}{
		{
			name: "link all",
			args: []string{"link", "--all", "--target", "~/.local/bin/docx2pdf", "--dry-run"},
		},
		{
			name: "link collection",
			args: []string{
				"link",
				"--collection",
				"one",
				"--target",
				"~/.local/bin/docx2pdf",
				"--dry-run",
			},
		},
		{
			name: "link multiple selectors",
			args: []string{
				"link",
				"scripts/docx2pdf",
				"tmux",
				"--target",
				"~/.local/bin/docx2pdf",
				"--dry-run",
			},
		},
		{
			name: "unlink all",
			args: []string{"unlink", "--all", "--target", "~/.local/bin/docx2pdf", "--dry-run"},
		},
		{
			name: "unlink collection",
			args: []string{
				"unlink",
				"--collection",
				"one",
				"--target",
				"~/.local/bin/docx2pdf",
				"--dry-run",
			},
		},
		{
			name: "unlink multiple selectors",
			args: []string{
				"unlink",
				"scripts/docx2pdf",
				"tmux",
				"--target",
				"~/.local/bin/docx2pdf",
				"--dry-run",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"--repo", repo}, tt.args...)
			_, _, err := executeCommand(args...)
			if err == nil ||
				!strings.Contains(err.Error(), "--target can only be used with one selector") {
				t.Fatalf("expected target scope rejection, got %v", err)
			}
		})
	}
}

func TestLinkAndUnlinkAcceptPackageSourceSelector(t *testing.T) {
	home, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]
`)
	source := filepath.Join(repo, "scripts", "docx2pdf")
	writeTextFile(t, source, "#!/bin/sh\n")
	target := filepath.Join(home, ".local", "bin", "docx2pdf")

	out, errOut, err := executeCommand("--repo", repo, "link", "scripts/docx2pdf")
	if err != nil {
		t.Fatalf("link package/source failed: %v\nstderr: %s", err, errOut)
	}
	if out != "linked scripts/docx2pdf -> ~/.local/bin/docx2pdf\n" {
		t.Fatalf("unexpected link output: %q", out)
	}
	assertSymlink(t, target, source)

	out, errOut, err = executeCommand("--repo", repo, "unlink", "scripts/docx2pdf")
	if err != nil {
		t.Fatalf("unlink package/source failed: %v\nstderr: %s", err, errOut)
	}
	if out != "unlinked scripts/docx2pdf -> ~/.local/bin/docx2pdf\n" {
		t.Fatalf("unexpected unlink output: %q", out)
	}
	if _, err := os.Lstat(target); err == nil || !os.IsNotExist(err) {
		t.Fatalf("target still exists after unlink package/source: %v", err)
	}
}

func TestTrackCommandAddsMappingsAndPrintsResults(t *testing.T) {
	home, repo := setupCLITest(t)
	writeManifest(t, repo, "version = 1\n")
	writeTextFile(t, filepath.Join(repo, "scripts", "docx2pdf"), "#!/bin/sh\n")
	target := filepath.Join(home, ".local", "bin", "docx2pdf")
	writeTextFile(t, target, "target remains untouched\n")

	out, errOut, err := executeCommand(
		"--repo", repo,
		"track", "scripts/docx2pdf", "~/.local/bin/docx2pdf", "--target", "~/bin/docx2pdf",
	)
	if err != nil {
		t.Fatalf("track failed: %v\nstderr: %s", err, errOut)
	}

	want := "tracked scripts/docx2pdf -> ~/.local/bin/docx2pdf\n" +
		"tracked scripts/docx2pdf -> ~/bin/docx2pdf\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
	requireFileContent(t, dotty.ManifestPath(repo), `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
  { source = "docx2pdf", target = "~/bin/docx2pdf" },
]
`)
	requireFileContent(t, target, "target remains untouched\n")
}

func TestUnlinkUntrackCommandRemovesMappingAndLink(t *testing.T) {
	home, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]
`)
	source := filepath.Join(repo, "scripts", "docx2pdf")
	writeTextFile(t, source, "#!/bin/sh\n")
	target := filepath.Join(home, ".local", "bin", "docx2pdf")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, target); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := executeCommand("--repo", repo, "unlink", "scripts/docx2pdf", "--untrack")
	if err != nil {
		t.Fatalf("unlink --untrack failed: %v\nstderr: %s", err, errOut)
	}

	if out != "unlinked and untracked scripts/docx2pdf -> ~/.local/bin/docx2pdf\n" {
		t.Fatalf("unexpected unlink --untrack output: %q", out)
	}
	if _, err := os.Lstat(target); err == nil || !os.IsNotExist(err) {
		t.Fatalf("target still exists after unlink --untrack: %v", err)
	}
	requireFileContent(t, dotty.ManifestPath(repo), `version = 1

[packages.scripts]
links = []
`)
}

func TestUntrackCommandRemovesMappingAndWarnsWhenLinkStillExists(t *testing.T) {
	home, repo := setupCLITest(t)
	writeManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]
`)
	source := filepath.Join(repo, "scripts", "docx2pdf")
	writeTextFile(t, source, "#!/bin/sh\n")
	target := filepath.Join(home, ".local", "bin", "docx2pdf")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, target); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := executeCommand(
		"--repo", repo,
		"untrack", "scripts/docx2pdf", "--target", "~/.local/bin/docx2pdf",
	)
	if err != nil {
		t.Fatalf("untrack failed: %v\nstderr: %s", err, errOut)
	}

	want := "untracked scripts/docx2pdf -> ~/.local/bin/docx2pdf\n" +
		"note: target-side link at ~/.local/bin/docx2pdf was not removed\n"
	if out != want {
		t.Fatalf("unexpected output\nwant: %q\ngot:  %q", want, out)
	}
	requireFileContent(t, dotty.ManifestPath(repo), `version = 1

[packages.scripts]
links = []
`)
	assertSymlink(t, target, source)
}

func TestTrackAndUntrackDryRunDoNotWriteManifest(t *testing.T) {
	_, repo := setupCLITest(t)
	manifest := "version = 1\n"
	writeManifest(t, repo, manifest)
	writeTextFile(t, filepath.Join(repo, "scripts", "docx2pdf"), "#!/bin/sh\n")

	out, errOut, err := executeCommand(
		"--repo", repo,
		"track", "--dry-run", "scripts/docx2pdf", "~/.local/bin/docx2pdf",
	)
	if err != nil {
		t.Fatalf("track --dry-run failed: %v\nstderr: %s", err, errOut)
	}
	if out != "would track scripts/docx2pdf -> ~/.local/bin/docx2pdf\n" {
		t.Fatalf("unexpected track dry-run output: %q", out)
	}
	requireFileContent(t, dotty.ManifestPath(repo), manifest)

	writeManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]
`)
	out, errOut, err = executeCommand(
		"--repo", repo,
		"untrack", "--dry-run", "scripts/docx2pdf",
	)
	if err != nil {
		t.Fatalf("untrack --dry-run failed: %v\nstderr: %s", err, errOut)
	}
	if out != "would untrack scripts/docx2pdf -> ~/.local/bin/docx2pdf\n" {
		t.Fatalf("unexpected untrack dry-run output: %q", out)
	}
	requireFileContent(t, dotty.ManifestPath(repo), `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]
`)
}
