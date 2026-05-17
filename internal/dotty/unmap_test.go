package dotty

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnmapRemovesSelectedLinkMappingFromManifestOnly(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
  { source = "sesh-fzf", target = "~/.local/bin/sesh-fzf" },
]
`)
	source := filepath.Join(repo, "scripts", "sesh-fzf")
	writeTextFile(t, source, "sesh-fzf\n")
	target := filepath.Join(home, ".local", "bin", "sesh-fzf")
	requireNoError(t, os.MkdirAll(filepath.Dir(target), 0o755))
	requireNoError(t, os.Symlink(source, target))

	results, err := NewService(repo, env).Unmap(UnmapOptions{
		Package: "scripts",
		Targets: []string{"~/.local/bin/sesh-fzf"},
	})
	requireNoError(t, err)
	if len(results) != 1 || results[0].Package != "scripts" || results[0].Source != "sesh-fzf" ||
		results[0].Target != "~/.local/bin/sesh-fzf" {
		t.Fatalf("unexpected unmap results: %#v", results)
	}
	assertSymlink(t, target, source)
	requireFileContent(t, source, "sesh-fzf\n")
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]
`)
}

func TestUnmapRemovesMultipleTargetsAndDedupesRepeatedSelectors(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
  { source = "ocli", target = "~/.local/bin/ocli" },
  { source = "sesh-fzf", target = "~/.local/bin/sesh-fzf" },
]
`)

	results, err := NewService(repo, env).Unmap(UnmapOptions{
		Package: "scripts",
		Targets: []string{
			"~/.local/bin/docx2pdf",
			filepath.Join(home, ".local", "bin", "docx2pdf"),
			"~/.local/bin/sesh-fzf",
		},
	})
	requireNoError(t, err)
	if len(results) != 2 {
		t.Fatalf("expected two unmapped results, got %#v", results)
	}
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.scripts]
links = [
  { source = "ocli", target = "~/.local/bin/ocli" },
]
`)
}

func TestUnmapDryRunReportsRemovalsWithoutWritingManifest(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	manifest := `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]
`
	writeDottyManifest(t, repo, manifest)

	results, err := NewService(repo, env).Unmap(UnmapOptions{
		Package: "scripts",
		Targets: []string{"~/.local/bin/docx2pdf"},
		DryRun:  true,
	})
	requireNoError(t, err)
	if len(results) != 1 || !results[0].DryRun {
		t.Fatalf("unexpected unmap dry-run results: %#v", results)
	}
	requireFileContent(t, ManifestPath(repo), manifest)
}

func TestUnmapMissingTargetFailsWithoutWritingManifest(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	manifest := `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]
`
	writeDottyManifest(t, repo, manifest)

	_, err := NewService(repo, env).Unmap(UnmapOptions{
		Package: "scripts",
		Targets: []string{"~/.local/bin/sesh-fzf"},
	})
	requireErrorContains(t, err, "target \"~/.local/bin/sesh-fzf\" is not mapped")
	requireFileContent(t, ManifestPath(repo), manifest)
}

func TestUnmapLastMappingLeavesEmptyPackageAndCollections(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "sesh-fzf", target = "~/.local/bin/sesh-fzf" },
]

[collections.terminal]
packages = ["scripts"]
`)

	_, err := NewService(repo, env).Unmap(UnmapOptions{
		Package: "scripts",
		Targets: []string{"~/.local/bin/sesh-fzf"},
	})
	requireNoError(t, err)
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.scripts]
links = []

[collections.terminal]
packages = ["scripts"]
`)
}

func TestUnmapValidatesPackageAndTargetSelection(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	manifest := `version = 1

[packages.scripts]
links = [
  { source = "sesh-fzf", target = "~/.local/bin/sesh-fzf" },
]
`
	writeDottyManifest(t, repo, manifest)
	svc := NewService(repo, env)

	tests := []struct {
		name    string
		options UnmapOptions
		wantErr string
	}{
		{
			name:    "invalid package",
			options: UnmapOptions{Package: "bad.name", Targets: []string{"~/.local/bin/sesh-fzf"}},
			wantErr: "package name",
		},
		{
			name:    "unknown package",
			options: UnmapOptions{Package: "missing", Targets: []string{"~/.local/bin/sesh-fzf"}},
			wantErr: "unknown package",
		},
		{
			name:    "missing target selector",
			options: UnmapOptions{Package: "scripts"},
			wantErr: "select at least one target",
		},
		{
			name:    "relative target selector",
			options: UnmapOptions{Package: "scripts", Targets: []string{"relative"}},
			wantErr: "must be absolute or home-relative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Unmap(tt.options)
			requireErrorContains(t, err, tt.wantErr)
			requireFileContent(t, ManifestPath(repo), manifest)
		})
	}
}
