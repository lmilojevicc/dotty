package dotty

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestValidateManifestNormalizesNilMaps(t *testing.T) {
	_, env := setupHome(t)
	manifest := &Manifest{Version: ManifestVersion}

	requireNoError(t, ValidateManifest(manifest, env))

	if manifest.Packages == nil {
		t.Fatal("expected packages map to be initialized")
	}
	if manifest.Collections == nil {
		t.Fatal("expected collections map to be initialized")
	}
}

func TestValidateManifestRejectsInvalidManifestShape(t *testing.T) {
	home, env := setupHome(t)
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
			name: "empty package name",
			manifest: &Manifest{Version: ManifestVersion, Packages: map[string]Package{
				"": {Links: []LinkMapping{{Source: ".", Target: "~/.config/bad"}}},
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
				"zsh": {
					Links: []LinkMapping{
						{Source: filepath.Join(home, ".zshrc"), Target: "~/.zshrc"},
					},
				},
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
			name: "duplicate expanded target within same package",
			manifest: &Manifest{Version: ManifestVersion, Packages: map[string]Package{
				"zsh": {Links: []LinkMapping{
					{Source: ".zshrc", Target: "~/.zshrc"},
					{Source: ".zprofile", Target: absTarget},
				}},
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
			name: "empty collection name",
			manifest: &Manifest{Version: ManifestVersion, Packages: map[string]Package{
				"zsh": {Links: []LinkMapping{{Source: ".zshrc", Target: "~/.zshrc"}}},
			}, Collections: map[string]Collection{
				"": {Packages: []string{"zsh"}},
			}},
			wantErr: "collection name",
		},
		{
			name: "empty collection package name",
			manifest: &Manifest{Version: ManifestVersion, Packages: map[string]Package{
				"zsh": {Links: []LinkMapping{{Source: ".zshrc", Target: "~/.zshrc"}}},
			}, Collections: map[string]Collection{
				"shell": {Packages: []string{""}},
			}},
			wantErr: "package name",
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
			requireErrorContains(t, ValidateManifest(tt.manifest, env), tt.wantErr)
		})
	}
}

func TestValidateManifestDuplicateTargetErrorIncludesActionableHint(t *testing.T) {
	home, env := setupHome(t)
	manifest := &Manifest{Version: ManifestVersion, Packages: map[string]Package{
		"a": {Links: []LinkMapping{{Source: ".zshrc", Target: "~/.zshrc"}}},
		"b": {Links: []LinkMapping{{Source: ".zshrc", Target: filepath.Join(home, ".zshrc")}}},
	}}

	err := ValidateManifest(manifest, env)
	requireErrorContains(t, err, "mapped more than once")
	requireErrorContains(t, err, "edit dotty.toml")
}

func TestAddManifestLinkCreatesDedupesAndRejectsTargetReuse(t *testing.T) {
	_, env := setupHome(t)
	manifest := NewManifest()
	link := LinkMapping{Source: ".zshrc", Target: "~/.zshrc"}

	requireNoError(t, AddManifestLink(manifest, "zsh", link, env))
	requireNoError(t, AddManifestLink(manifest, "zsh", link, env))

	links := manifest.Packages["zsh"].Links
	if len(links) != 1 {
		t.Fatalf("expected idempotent link add, got %d links", len(links))
	}
	requireErrorContains(
		t,
		AddManifestLink(manifest, "zsh", LinkMapping{Source: ".zshenv", Target: "~/.zshrc"}, env),
		"already maps target",
	)
	requireErrorContains(
		t,
		AddManifestLink(manifest, "zsh", LinkMapping{Source: ".zshenv", Target: "~/.zshrc"}, env),
		"edit dotty.toml",
	)
}

func TestFormatManifestSortsPackagesAndCollections(t *testing.T) {
	manifest := NewManifest()
	manifest.Packages["zsh"] = Package{Links: []LinkMapping{{Source: ".zshrc", Target: "~/.zshrc"}}}
	manifest.Packages["tmux"] = Package{
		Links: []LinkMapping{{Source: ".", Target: "~/.config/tmux"}},
	}
	manifest.Collections["terminal"] = Collection{Packages: []string{"tmux", "zsh"}}
	manifest.Collections["shell"] = Collection{Packages: []string{"zsh"}}

	got := FormatManifest(manifest)
	want := "version = 1\n\n[packages.tmux]\nlinks = [\n  { source = \".\", target = \"~/.config/tmux\" },\n]\n\n[packages.zsh]\nlinks = [\n  { source = \".zshrc\", target = \"~/.zshrc\" },\n]\n\n[collections.shell]\npackages = [\"zsh\"]\n\n[collections.terminal]\npackages = [\"tmux\", \"zsh\"]\n"
	if got != want {
		t.Fatalf("manifest mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestLoadManifestRejectsEmptyFileAndInvalidTOML(t *testing.T) {
	_, env := setupHome(t)
	repo := t.TempDir()

	writeDottyManifest(t, repo, "")
	requireErrorContains(t, loadManifestError(repo, env), "unsupported manifest version")

	writeDottyManifest(t, repo, "version = [\n")
	requireErrorContains(t, loadManifestError(repo, env), "parse manifest")
}

func TestLoadManifestReportsReadError(t *testing.T) {
	_, env := setupHome(t)
	repo := t.TempDir()
	requireNoError(t, os.Mkdir(ManifestPath(repo), 0o755))

	requireErrorContains(t, loadManifestError(repo, env), "read manifest")
}

func TestSaveManifestWritesFormattedManifest(t *testing.T) {
	_, env := setupHome(t)
	repo := t.TempDir()
	manifest := NewManifest()
	manifest.Packages["zsh"] = Package{Links: []LinkMapping{{Source: ".zshrc", Target: "~/.zshrc"}}}

	requireNoError(t, RunAtomic(func(tx *Tx) error {
		return SaveManifest(tx, repo, manifest, env)
	}))

	requireFileContent(t, ManifestPath(repo), FormatManifest(manifest))
}

func TestSaveManifestRollbackRestoresPreviousManifestBytes(t *testing.T) {
	_, env := setupHome(t)
	repo := t.TempDir()
	original := "version = 1\n# hand-written comment kept only by rollback\n"
	writeDottyManifest(t, repo, original)
	manifest := NewManifest()
	manifest.Packages["zsh"] = Package{Links: []LinkMapping{{Source: ".zshrc", Target: "~/.zshrc"}}}

	err := RunAtomic(func(tx *Tx) error {
		requireNoError(t, SaveManifest(tx, repo, manifest, env))
		requireFileContent(t, ManifestPath(repo), FormatManifest(manifest))
		return errors.New("stop")
	})
	requireErrorContains(t, err, "stop")

	requireFileContent(t, ManifestPath(repo), original)
}

func TestSaveManifestRejectsInvalidManifest(t *testing.T) {
	_, env := setupHome(t)
	repo := t.TempDir()
	manifest := NewManifest()
	manifest.Version = ManifestVersion + 1

	err := RunAtomic(func(tx *Tx) error {
		return SaveManifest(tx, repo, manifest, env)
	})
	requireErrorContains(t, err, "unsupported manifest version")
	requireNoPath(t, ManifestPath(repo))
}

func TestFormatManifestRoundTripAndEscapesLinkStrings(t *testing.T) {
	manifest := NewManifest()
	manifest.Packages["weird"] = Package{Links: []LinkMapping{
		{Source: "quoted\"source", Target: "~/quoted\"target"},
		{Source: "backslash\\source", Target: "~/backslash\\target"},
	}}
	manifest.Collections["all"] = Collection{Packages: []string{"weird"}}

	var parsed Manifest
	requireNoError(t, toml.Unmarshal([]byte(FormatManifest(manifest)), &parsed))
	parsed.normalize()
	if parsed.Version != manifest.Version {
		t.Fatalf("version mismatch: want %d, got %d", manifest.Version, parsed.Version)
	}
	if len(parsed.Packages["weird"].Links) != 2 {
		t.Fatalf("links did not round trip: %#v", parsed.Packages["weird"].Links)
	}
	requireEqualStrings(t, parsed.Collections["all"].Packages, []string{"weird"})
}

func TestFormatManifestEmptyManifest(t *testing.T) {
	if got := FormatManifest(NewManifest()); got != "version = 1\n" {
		t.Fatalf("empty manifest mismatch: %q", got)
	}
}

func loadManifestError(repo string, env Env) error {
	_, err := LoadManifest(repo, env)
	return err
}
