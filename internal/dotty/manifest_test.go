package dotty

import (
	"path/filepath"
	"testing"
)

func TestValidateManifestNormalizesNilMaps(t *testing.T) {
	manifest := &Manifest{Version: ManifestVersion}

	requireNoError(t, ValidateManifest(manifest))

	if manifest.Packages == nil {
		t.Fatal("expected packages map to be initialized")
	}
	if manifest.Collections == nil {
		t.Fatal("expected collections map to be initialized")
	}
}

func TestValidateManifestRejectsInvalidManifestShape(t *testing.T) {
	home := setupHome(t)
	absTarget := filepath.Join(home, ".zshrc")

	tests := []struct {
		name     string
		manifest *Manifest
		wantErr  string
	}{
		{
			name:     "unsupported version",
			manifest: &Manifest{Version: ManifestVersion + 1},
			wantErr:  "unsupported manifest version",
		},
		{
			name: "invalid package name",
			manifest: &Manifest{Version: ManifestVersion, Packages: map[string]Package{
				"bad.name": {Links: []LinkMapping{{Source: ".", Target: "~/.config/bad"}}},
			}},
			wantErr: "package name",
		},
		{
			name: "empty package source",
			manifest: &Manifest{Version: ManifestVersion, Packages: map[string]Package{
				"zsh": {Links: []LinkMapping{{Source: "", Target: "~/.zshrc"}}},
			}},
			wantErr: "source is empty",
		},
		{
			name: "absolute package source",
			manifest: &Manifest{Version: ManifestVersion, Packages: map[string]Package{
				"zsh": {Links: []LinkMapping{{Source: filepath.Join(home, ".zshrc"), Target: "~/.zshrc"}}},
			}},
			wantErr: "must be relative to the package root",
		},
		{
			name: "escaping package source",
			manifest: &Manifest{Version: ManifestVersion, Packages: map[string]Package{
				"zsh": {Links: []LinkMapping{{Source: "../.zshrc", Target: "~/.zshrc"}}},
			}},
			wantErr: "escapes the package root",
		},
		{
			name: "relative target path",
			manifest: &Manifest{Version: ManifestVersion, Packages: map[string]Package{
				"zsh": {Links: []LinkMapping{{Source: ".zshrc", Target: ".zshrc"}}},
			}},
			wantErr: "must be absolute or home-relative",
		},
		{
			name: "unsupported home target syntax",
			manifest: &Manifest{Version: ManifestVersion, Packages: map[string]Package{
				"zsh": {Links: []LinkMapping{{Source: ".zshrc", Target: "~other/.zshrc"}}},
			}},
			wantErr: "unsupported home syntax",
		},
		{
			name: "duplicate expanded target",
			manifest: &Manifest{Version: ManifestVersion, Packages: map[string]Package{
				"a": {Links: []LinkMapping{{Source: ".zshrc", Target: "~/.zshrc"}}},
				"b": {Links: []LinkMapping{{Source: ".zshrc", Target: absTarget}}},
			}},
			wantErr: "mapped more than once",
		},
		{
			name: "invalid collection name",
			manifest: &Manifest{Version: ManifestVersion, Packages: map[string]Package{
				"zsh": {Links: []LinkMapping{{Source: ".zshrc", Target: "~/.zshrc"}}},
			}, Collections: map[string]Collection{
				"bad.name": {Packages: []string{"zsh"}},
			}},
			wantErr: "collection name",
		},
		{
			name: "collection references unknown package",
			manifest: &Manifest{Version: ManifestVersion, Packages: map[string]Package{
				"zsh": {Links: []LinkMapping{{Source: ".zshrc", Target: "~/.zshrc"}}},
			}, Collections: map[string]Collection{
				"terminal": {Packages: []string{"tmux"}},
			}},
			wantErr: "references unknown package",
		},
		{
			name: "collection references package twice",
			manifest: &Manifest{Version: ManifestVersion, Packages: map[string]Package{
				"zsh": {Links: []LinkMapping{{Source: ".zshrc", Target: "~/.zshrc"}}},
			}, Collections: map[string]Collection{
				"terminal": {Packages: []string{"zsh", "zsh"}},
			}},
			wantErr: "more than once",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireErrorContains(t, ValidateManifest(tt.manifest), tt.wantErr)
		})
	}
}

func TestAddManifestLinkCreatesDedupesAndRejectsTargetReuse(t *testing.T) {
	manifest := NewManifest()
	link := LinkMapping{Source: ".zshrc", Target: "~/.zshrc"}

	requireNoError(t, AddManifestLink(manifest, "zsh", link))
	requireNoError(t, AddManifestLink(manifest, "zsh", link))

	links := manifest.Packages["zsh"].Links
	if len(links) != 1 {
		t.Fatalf("expected idempotent link add, got %d links", len(links))
	}
	requireErrorContains(t, AddManifestLink(manifest, "zsh", LinkMapping{Source: ".zshenv", Target: "~/.zshrc"}), "already maps target")
}

func TestFormatManifestSortsPackagesAndCollections(t *testing.T) {
	manifest := NewManifest()
	manifest.Packages["zsh"] = Package{Links: []LinkMapping{{Source: ".zshrc", Target: "~/.zshrc"}}}
	manifest.Packages["tmux"] = Package{Links: []LinkMapping{{Source: ".", Target: "~/.config/tmux"}}}
	manifest.Collections["terminal"] = Collection{Packages: []string{"tmux", "zsh"}}
	manifest.Collections["shell"] = Collection{Packages: []string{"zsh"}}

	got := FormatManifest(manifest)
	want := "version = 1\n\n[packages.tmux]\nlinks = [\n  { source = \".\", target = \"~/.config/tmux\" },\n]\n\n[packages.zsh]\nlinks = [\n  { source = \".zshrc\", target = \"~/.zshrc\" },\n]\n\n[collections.shell]\npackages = [\"zsh\"]\n\n[collections.terminal]\npackages = [\"tmux\", \"zsh\"]\n"
	if got != want {
		t.Fatalf("manifest mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}
