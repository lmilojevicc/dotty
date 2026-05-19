package dotty

import (
	"path/filepath"
	"testing"
)

func TestTrackAddsNewSourceInExistingPackage(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, `version = 1

[packages.zsh]
links = []
`)
	writeTextFile(t, filepath.Join(repo, "zsh", ".zprofile"), "export PATH=$HOME/bin:$PATH\n")
	target := filepath.Join(home, ".zprofile")
	writeTextFile(t, target, "local file stays put\n")

	results, err := NewService(repo, env).Track(TrackOptions{
		Selector: mustParseSelector(t, "zsh/.zprofile"),
		Targets:  []string{"~/.zprofile"},
	})

	requireNoError(t, err)
	requireTrackResults(t, results, []string{"zsh:.zprofile:~/.zprofile"})
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.zsh]
links = [
  { source = ".zprofile", target = "~/.zprofile" },
]
`)
	requireFileContent(t, target, "local file stays put\n")
}

func TestTrackAddsSourceInNewPackageWhenPackageRootExists(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, "version = 1\n")
	writeTextFile(t, filepath.Join(repo, "scripts", "docx2pdf"), "#!/bin/sh\n")

	results, err := NewService(repo, env).Track(TrackOptions{
		Selector: mustParseSelector(t, "scripts/docx2pdf"),
		Targets:  []string{"~/.local/bin/docx2pdf"},
	})

	requireNoError(t, err)
	requireTrackResults(t, results, []string{"scripts:docx2pdf:~/.local/bin/docx2pdf"})
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]
`)
}

func TestTrackAddsMultipleTargetsForOneSource(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, `version = 1

[packages.scripts]
links = []
`)
	writeTextFile(t, filepath.Join(repo, "scripts", "docx2pdf"), "#!/bin/sh\n")

	results, err := NewService(repo, env).Track(TrackOptions{
		Selector: mustParseSelector(t, "scripts/docx2pdf"),
		Targets: []string{
			"~/.local/bin/docx2pdf",
			"~/bin/docx2pdf",
		},
	})

	requireNoError(t, err)
	requireTrackResults(t, results, []string{
		"scripts:docx2pdf:~/.local/bin/docx2pdf",
		"scripts:docx2pdf:~/bin/docx2pdf",
	})
}

func TestTrackPackageSelectorTracksPackageRoot(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, "version = 1\n")
	writeTextFile(t, filepath.Join(repo, "tmux", "tmux.conf"), "set -g mouse on\n")

	results, err := NewService(repo, env).Track(TrackOptions{
		Selector: mustParseSelector(t, "tmux"),
		Targets:  []string{"~/.config/tmux"},
	})

	requireNoError(t, err)
	requireTrackResults(t, results, []string{"tmux:.:~/.config/tmux"})
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]
`)
}

func TestTrackRejectsInvalidInputsWithoutWritingManifest(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	manifest := `version = 1

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
]
`
	writeDottyManifest(t, repo, manifest)
	writeTextFile(t, filepath.Join(repo, "zsh", ".zshrc"), "export EDITOR=vim\n")
	writeTextFile(t, filepath.Join(repo, "zsh", ".zprofile"), "export PATH=$HOME/bin:$PATH\n")

	tests := []struct {
		name    string
		options TrackOptions
		wantErr string
	}{
		{
			name: "missing source",
			options: TrackOptions{
				Selector: mustParseSelector(t, "zsh/.missing"),
				Targets:  []string{"~/.missing"},
			},
			wantErr: "is missing from the repository",
		},
		{
			name: "missing package root",
			options: TrackOptions{
				Selector: mustParseSelector(t, "ghostty/config"),
				Targets:  []string{"~/.config/ghostty"},
			},
			wantErr: "package root",
		},
		{
			name: "invalid target syntax",
			options: TrackOptions{
				Selector: mustParseSelector(t, "zsh/.zshrc"),
				Targets:  []string{"relative/target"},
			},
			wantErr: "must be absolute or home-relative",
		},
		{
			name: "target already mapped in same package",
			options: TrackOptions{
				Selector: mustParseSelector(t, "zsh/.zprofile"),
				Targets:  []string{"~/.zshrc"},
			},
			wantErr: "is already mapped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewService(repo, env).Track(tt.options)
			requireErrorContains(t, err, tt.wantErr)
			requireFileContent(t, ManifestPath(repo), manifest)
		})
	}
}

func requireTrackResults(t *testing.T, got []TrackResult, want []string) {
	t.Helper()
	actual := make([]string, 0, len(got))
	for _, result := range got {
		actual = append(actual, result.Package+":"+result.Source+":"+result.Target)
	}
	requireEqualStrings(t, actual, want)
}
