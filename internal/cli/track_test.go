package cli

import (
	"os"
	"path/filepath"
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

	if out != "linked scripts: ~/.local/bin/docx2pdf -> ~/dotfiles/scripts/docx2pdf\n" {
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

	want := "tracked scripts: ~/.local/bin/docx2pdf -> docx2pdf\n" +
		"tracked scripts: ~/bin/docx2pdf -> docx2pdf\n"
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

	if out != "unlinked scripts: ~/.local/bin/docx2pdf (link removed)\n" {
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

	want := "untracked scripts: ~/.local/bin/docx2pdf -> docx2pdf (link still exists)\n"
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
	if out != "would track scripts: ~/.local/bin/docx2pdf -> docx2pdf\n" {
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
	if out != "would untrack scripts: ~/.local/bin/docx2pdf -> docx2pdf\n" {
		t.Fatalf("unexpected untrack dry-run output: %q", out)
	}
	requireFileContent(t, dotty.ManifestPath(repo), `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]
`)
}
