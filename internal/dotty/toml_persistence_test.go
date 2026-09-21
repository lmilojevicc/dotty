package dotty

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

func TestStrictTOMLPersistence(t *testing.T) {
	t.Run("unknown manifest fields", testStrictManifestFields)
	t.Run("unknown config fields", testStrictConfigFields)
	t.Run("dynamic inventory", testStrictDynamicInventory)
	t.Run("syntax locations", testStrictSyntaxLocations)
	t.Run("scalar round trips", testTOMLScalarRoundTrips)
	t.Run("invalid UTF8", testTOMLInvalidUTF8)
	t.Run("existing document preflight", testTOMLExistingPreflight)
	t.Run("save modes", testTOMLSaveModes)
}

func testStrictManifestFields(t *testing.T) {
	_, env := setupHome(t)
	t.Setenv("DOTTY_REPO", "")
	cases := []struct {
		name, document string
		keys           []string
	}{
		{"top scalar", "version = 1\nextra = true\n", []string{"extra"}},
		{"top table", "version = 1\n[extra]\nflag = true\n", []string{"extra"}},
		{"top empty table", "version = 1\n[extra]\n", []string{"extra"}},
		{"top empty array table", "version = 1\n[[extra]]\n", []string{"extra"}},
		{
			"package field",
			"version = 1\n[packages.zsh]\nlinks = []\nextra = true\n",
			[]string{"packages.zsh.extra"},
		},
		{
			"package empty table",
			"version = 1\n[packages.zsh.extra]\n",
			[]string{"packages.zsh.extra"},
		},
		{
			"inline mapping",
			"version = 1\n[packages.zsh]\nlinks = [{ source = '.', target = '~/.zsh', extra = true }]\n",
			[]string{"packages.zsh.links.extra"},
		},
		{
			"array table mapping",
			"version = 1\n[[packages.zsh.links]]\nsource = '.'\ntarget = '~/.zsh'\nextra = true\n",
			[]string{"packages.zsh.links.extra"},
		},
		{
			"collection field",
			"version = 1\n[collections.shell]\npackages = []\nextra = true\n",
			[]string{"collections.shell.extra"},
		},
		{
			"collection empty table",
			"version = 1\n[collections.shell.extra]\n",
			[]string{"collections.shell.extra"},
		},
		{
			"multiple",
			"version = 1\nfirst = true\nsecond = false\n[packages.zsh]\nextra = 1\n",
			[]string{"first", "second", "packages.zsh.extra"},
		},
		{
			"quoted dotted segments",
			"version = 1\n[packages.\"z.sh\"]\n\"extra.field\" = true\n",
			[]string{`packages."z.sh"."extra.field"`},
		},
		{
			"dotted field",
			"version = 1\npackages.zsh.extra.flag = true\n",
			[]string{"packages.zsh.extra.flag"},
		},
		{
			"quoted escaped segments",
			"version = 1\n\"extra\\\"field\" = true\n",
			[]string{`"extra\"field"`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			path := ManifestPath(repo)
			writeDottyManifest(t, repo, tc.document)
			requireNoError(t, os.Chmod(path, 0o640))
			assertStrictDiagnostic(t, loadManifestError(repo, env), "manifest", path, tc.keys)
			requireFileContent(t, path, tc.document)
			requirePersistenceMode(t, path, 0o640)
		})
	}
}

func testStrictConfigFields(t *testing.T) {
	cases := []struct {
		name, document string
		keys           []string
	}{
		{"scalar", "repo = '~/dots'\nextra = true\n", []string{"extra"}},
		{"table", "repo = '~/dots'\n[extra]\nflag = true\n", []string{"extra"}},
		{"empty table", "repo = '~/dots'\n[extra]\n", []string{"extra"}},
		{"nested empty table", "repo = '~/dots'\n[extra.nested]\n", []string{"extra.nested"}},
		{"inline nested", "repo = '~/dots'\nextra = { nested = {} }\n", []string{"extra"}},
		{
			"multiple quoted",
			"repo = '~/dots'\n\"extra.field\" = true\nother.field = false\n",
			[]string{`"extra.field"`, "other.field"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, env := setupHome(t)
			t.Setenv("DOTTY_REPO", "")
			path := env.ConfigFilePath()
			writeTextFile(t, path, tc.document)
			requireNoError(t, os.Chmod(path, 0o600))
			assertStrictDiagnostic(t, loadConfigError(env), "config", path, tc.keys)
			requireFileContent(t, path, tc.document)
			requirePersistenceMode(t, path, 0o600)
		})
	}
}

func assertStrictDiagnostic(t *testing.T, err error, kind, path string, keys []string) {
	t.Helper()
	var strict *toml.StrictMissingError
	if !errors.As(err, &strict) {
		t.Fatalf("expected wrapped strict decoder cause, got %v", err)
	}
	if len(strict.Errors) != len(keys) {
		t.Fatalf("unknown fields: want %v, got %#v", keys, strict.Errors)
	}
	var details []string
	for i := range strict.Errors {
		child := &strict.Errors[i]
		line, column := child.Position()
		if line < 1 || column < 1 {
			t.Fatalf("missing decoder location: %v", child)
		}
		details = append(details, fmt.Sprintf("%s (line %d, column %d)", keys[i], line, column))
	}
	want := fmt.Sprintf(
		"parse %s %s: unsupported TOML fields: %s; remove unsupported fields before retrying; third-party extensions are unsupported: %s",
		kind,
		path,
		strings.Join(details, ", "),
		strict.Error(),
	)
	if err.Error() != want {
		t.Fatalf("diagnostic mismatch\nwant: %s\ngot:  %v", want, err)
	}
}

func testStrictDynamicInventory(t *testing.T) {
	_, env := setupHome(t)
	t.Setenv("DOTTY_REPO", "")
	repo := t.TempDir()
	writeDottyManifest(
		t,
		repo,
		"version = 1\n[packages.custom-2]\nlinks = []\n[packages.Other_1]\n[collections.personal-3]\npackages = ['Other_1', 'custom-2']\n[collections.empty]\n",
	)
	manifest, err := LoadManifest(repo, env)
	requireNoError(t, err)
	if len(manifest.Packages) != 2 || len(manifest.Collections) != 2 {
		t.Fatalf("dynamic inventory lost: %#v", manifest)
	}
}

func testStrictSyntaxLocations(t *testing.T) {
	_, env := setupHome(t)
	t.Setenv("DOTTY_REPO", "")
	repo := t.TempDir()
	writeDottyManifest(t, repo, "version = [\n")
	writeTextFile(t, env.ConfigFilePath(), "repo = [\n")
	for path, err := range map[string]error{ManifestPath(repo): loadManifestError(repo, env), env.ConfigFilePath(): loadConfigError(env)} {
		var cause *toml.DecodeError
		if !errors.As(err, &cause) {
			t.Fatalf("lost syntax cause: %v", err)
		}
		line, column := cause.Position()
		requireErrorContains(t, err, path)
		requireErrorContains(t, err, fmt.Sprintf("line %d, column %d", line, column))
	}
}

func testTOMLScalarRoundTrips(t *testing.T) {
	values := []string{"quote\"backslash\\", "\t\b\f\r\n\a\v", "é漢字😀�", "\u0085\u2028\u2029"}
	for c := byte(0); c < 32; c++ {
		values = append(values, string([]byte{c}))
	}
	values = append(values, "\x7f")
	for i, value := range values {
		t.Run(fmt.Sprintf("scalar-%d", i), func(t *testing.T) {
			_, env := setupHome(t)
			t.Setenv("DOTTY_REPO", "")
			repo := t.TempDir()
			manifest := NewManifest()
			link := LinkMapping{Source: "source-" + value, Target: "~/target-" + value}
			manifest.Packages["pkg"] = Package{Links: []LinkMapping{link}}
			requireNoError(
				t,
				RunAtomic(func(tx *Tx) error { return SaveManifest(tx, repo, manifest, env) }),
			)
			got, err := LoadManifest(repo, env)
			requireNoError(t, err)
			if !reflect.DeepEqual(got.Packages["pkg"].Links, []LinkMapping{link}) {
				t.Fatalf("scalar changed: want %#v, got %#v", link, got.Packages["pkg"].Links)
			}
			cfg := &Config{Repo: "~/repo-" + value}
			requireNoError(t, RunAtomic(func(tx *Tx) error { return SaveConfig(tx, env, cfg) }))
			gotConfig, err := LoadConfig(env)
			requireNoError(t, err)
			if *gotConfig != *cfg {
				t.Fatalf("repo changed: want %q, got %q", cfg.Repo, gotConfig.Repo)
			}
		})
	}
	t.Run("control golden", func(t *testing.T) {
		manifest := NewManifest()
		manifest.Packages["pkg"] = Package{
			Links: []LinkMapping{{Source: "a\a\v\x00\x1f\x7f", Target: "~/\t\b\f\r\n\"\\é😀�"}},
		}
		want := "version = 1\n\n[packages.pkg]\nlinks = [\n  { source = \"a\\u0007\\u000B\\u0000\\u001F\\u007F\", target = \"~/\\t\\b\\f\\r\\n\\\"\\\\é😀�\" },\n]\n"
		if got := FormatManifest(manifest); got != want {
			t.Fatalf("golden mismatch\nwant: %q\ngot: %q", want, got)
		}
	})
}

func testTOMLInvalidUTF8(t *testing.T) {
	invalid := string([]byte{0xff, 0xfe})
	t.Run("disk", func(t *testing.T) {
		_, env := setupHome(t)
		t.Setenv("DOTTY_REPO", "")
		repo := t.TempDir()
		for _, suffix := range []string{"# " + invalid + "\n", "[\"" + invalid + "\"]\n"} {
			writeDottyManifest(t, repo, "version = 1\n"+suffix)
			writeTextFile(t, env.ConfigFilePath(), "repo = '~/dots'\n"+suffix)
			requireErrorContains(t, loadManifestError(repo, env), "UTF-8")
			requireErrorContains(t, loadConfigError(env), "UTF-8")
		}
		writeDottyManifest(
			t,
			repo,
			"version = 1\n[packages.pkg]\nlinks = [{source = '"+invalid+"', target = '~/file'}]\n",
		)
		writeTextFile(t, env.ConfigFilePath(), "repo = '"+invalid+"'\n")
		requireErrorContains(t, loadManifestError(repo, env), "UTF-8")
		requireErrorContains(t, loadConfigError(env), "UTF-8")
	})
	for _, field := range []string{"source", "target", "package key", "collection key", "member", "repo"} {
		t.Run(field, func(t *testing.T) {
			_, env := setupHome(t)
			t.Setenv("DOTTY_REPO", "")
			repo := filepath.Join(t.TempDir(), "missing", "repo")
			manifest := NewManifest()
			link := LinkMapping{Source: "file", Target: "~/file"}
			switch field {
			case "source":
				link.Source += invalid
			case "target":
				link.Target += invalid
			case "package key":
				manifest.Packages[invalid] = Package{}
			case "collection key":
				manifest.Collections[invalid] = Collection{}
			case "member":
				manifest.Collections["all"] = Collection{Packages: []string{invalid}}
			}
			manifest.Packages["pkg"] = Package{Links: []LinkMapping{link}}
			save := func(tx *Tx) error { return SaveManifest(tx, repo, manifest, env) }
			if field == "repo" {
				save = func(tx *Tx) error { return SaveConfig(tx, env, &Config{Repo: "~/" + invalid}) }
			}
			// Check before rollback, so a create-then-undo implementation cannot pass.
			tx := &Tx{}
			err := save(tx)
			requireErrorContains(t, err, "UTF-8")
			requireNoPath(t, filepath.Dir(repo))
			requireNoPath(t, filepath.Dir(env.ConfigFilePath()))
			if len(tx.rollbacks) != 0 {
				t.Fatal("invalid input registered filesystem mutations")
			}
			writeDottyManifest(t, repo, "version = 1\n")
			writeTextFile(t, env.ConfigFilePath(), "repo = '~/old'\n")
			requireNoError(t, os.Chmod(ManifestPath(repo), 0o640))
			requireNoError(t, os.Chmod(env.ConfigFilePath(), 0o600))
			requireErrorContains(t, save(&Tx{}), "UTF-8")
			requireFileContent(t, ManifestPath(repo), "version = 1\n")
			requireFileContent(t, env.ConfigFilePath(), "repo = '~/old'\n")
			requirePersistenceMode(t, ManifestPath(repo), 0o640)
			requirePersistenceMode(t, env.ConfigFilePath(), 0o600)
		})
	}
	t.Run("formatter retains invalid bytes outside contract", func(t *testing.T) {
		manifest := NewManifest()
		manifest.Packages["pkg"] = Package{
			Links: []LinkMapping{{Source: invalid, Target: "~/" + invalid}},
		}
		formatted := FormatManifest(manifest)
		if utf8.ValidString(formatted) || bytes.Count([]byte(formatted), []byte(invalid)) != 2 {
			t.Fatalf("formatter silently replaced invalid bytes: %q", formatted)
		}
	})
	t.Run("init repo before creation", func(t *testing.T) {
		home, env := setupHome(t)
		t.Setenv("DOTTY_REPO", "")
		repo := filepath.Join(home, "missing", invalid)
		_, err := InitRepo(repo, env)
		requireErrorContains(t, err, "UTF-8")
		requireNoPath(t, filepath.Join(home, "missing"))
		requireNoPath(t, filepath.Dir(env.ConfigFilePath()))
	})
}

func testTOMLExistingPreflight(t *testing.T) {
	for _, document := range []string{"extra = true\n", "[extra]\n", "repo = [\n", "repo = '\xff'\n"} {
		t.Run(fmt.Sprintf("config-%q", document), func(t *testing.T) {
			home, env := setupHome(t)
			t.Setenv("DOTTY_REPO", "")
			writeTextFile(t, env.ConfigFilePath(), document)
			requireNoError(t, os.Chmod(env.ConfigFilePath(), 0o600))
			repo := filepath.Join(home, "missing", "repo")
			// Init must refuse before creating the repository, not merely roll it back.
			original := persistenceRename
			persistenceRename = func(_, _ string) error { t.Fatal("preflight reached persistence mutation"); return nil }
			t.Cleanup(func() { persistenceRename = original })
			_, err := InitRepo(repo, env)
			requireErrorContains(t, err, env.ConfigFilePath())
			requireNoPath(t, filepath.Dir(repo))
			tx := &Tx{}
			requireErrorContains(
				t,
				SaveConfig(tx, env, &Config{Repo: "~/new"}),
				env.ConfigFilePath(),
			)
			if len(tx.rollbacks) != 0 {
				t.Fatal("save mutated before refusal")
			}
			requireFileContent(t, env.ConfigFilePath(), document)
			requirePersistenceMode(t, env.ConfigFilePath(), 0o600)
		})
	}
	for _, document := range []string{"version = 1\nextra = true\n", "version = 1\n[extra]\n", "version = [\n", "version = 1\n# \xff\n"} {
		t.Run(fmt.Sprintf("manifest-%q", document), func(t *testing.T) {
			_, env := setupHome(t)
			t.Setenv("DOTTY_REPO", "")
			repo := t.TempDir()
			writeDottyManifest(t, repo, document)
			requireNoError(t, os.Chmod(ManifestPath(repo), 0o640))
			tx := &Tx{}
			requireErrorContains(t, SaveManifest(tx, repo, NewManifest(), env), ManifestPath(repo))
			if len(tx.rollbacks) != 0 {
				t.Fatal("save mutated before refusal")
			}
			requireFileContent(t, ManifestPath(repo), document)
			requirePersistenceMode(t, ManifestPath(repo), 0o640)
		})
	}
}

func testTOMLSaveModes(t *testing.T) {
	_, env := setupHome(t)
	t.Setenv("DOTTY_REPO", "")
	repo := t.TempDir()
	for _, mode := range []os.FileMode{0o600, 0o640, 0o750} {
		writeDottyManifest(t, repo, "version = 1\n# original\n")
		writeTextFile(t, env.ConfigFilePath(), "repo = '~/old'\n")
		for _, path := range []string{ManifestPath(repo), env.ConfigFilePath()} {
			requireNoError(t, os.Chmod(path, mode))
		}
		stop := errors.New("rollback saves")
		err := RunAtomic(func(tx *Tx) error {
			requireNoError(t, SaveManifest(tx, repo, NewManifest(), env))
			requireNoError(t, SaveConfig(tx, env, &Config{Repo: "~/new"}))
			for _, path := range []string{ManifestPath(repo), env.ConfigFilePath()} {
				requirePersistenceMode(t, path, mode)
			}
			return stop
		})
		if !errors.Is(err, stop) {
			t.Fatalf("lost rollback cause: %v", err)
		}
		requireFileContent(t, ManifestPath(repo), "version = 1\n# original\n")
		requireFileContent(t, env.ConfigFilePath(), "repo = '~/old'\n")
		for _, path := range []string{ManifestPath(repo), env.ConfigFilePath()} {
			requirePersistenceMode(t, path, mode)
		}
	}
}

func requirePersistenceMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	requireNoError(t, err)
	if info.Mode() != want {
		t.Fatalf("mode for %s: want %v, got %v", path, want, info.Mode())
	}
}
