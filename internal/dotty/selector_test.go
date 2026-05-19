package dotty

import (
	"path/filepath"
	"testing"
)

func TestParseSelector(t *testing.T) {
	tests := []struct {
		name    string
		arg     string
		want    Selector
		wantErr string
	}{
		{
			name: "package selector",
			arg:  "zsh",
			want: Selector{Package: "zsh"},
		},
		{
			name: "package source selector",
			arg:  "scripts/docx2pdf",
			want: Selector{Package: "scripts", Source: "docx2pdf"},
		},
		{
			name: "nested package source selector",
			arg:  "scripts/office/docx2pdf",
			want: Selector{Package: "scripts", Source: "office/docx2pdf"},
		},
		{
			name:    "empty source selector",
			arg:     "nvim/",
			wantErr: "empty source selector",
		},
		{
			name:    "empty selector",
			arg:     "",
			wantErr: "empty selector",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSelector(tt.arg)
			if tt.wantErr != "" {
				requireErrorContains(t, err, tt.wantErr)
				return
			}
			requireNoError(t, err)
			if got != tt.want {
				t.Fatalf("selector mismatch: want %#v, got %#v", tt.want, got)
			}
			if tt.want.Source == "" && !got.IsPackage() {
				t.Fatalf("expected %#v to be a package selector", got)
			}
			if tt.want.Source != "" && !got.IsPackageSource() {
				t.Fatalf("expected %#v to be a package source selector", got)
			}
		})
	}
}

func TestResolveSelectorsExpandsPackageSelector(t *testing.T) {
	_, env := setupHome(t)
	manifest := manifestForLinkMappingSelection()

	selected, err := ResolveSelectors(manifest, ResolveOptions{
		Selectors: []Selector{mustParseSelector(t, "scripts")},
	}, env)

	requireNoError(t, err)
	requireSelectedMappings(t, selected, []string{
		"scripts:~/.local/bin/docx2pdf",
		"scripts:~/.local/bin/sesh-fzf",
	})
}

func TestResolveSelectorsExpandsPackageSourceSelector(t *testing.T) {
	_, env := setupHome(t)
	manifest := manifestForLinkMappingSelection()

	selected, err := ResolveSelectors(manifest, ResolveOptions{
		Selectors: []Selector{mustParseSelector(t, "scripts/docx2pdf")},
	}, env)

	requireNoError(t, err)
	requireSelectedMappings(t, selected, []string{"scripts:~/.local/bin/docx2pdf"})
}

func TestResolveSelectorsCollapsesDuplicateMappings(t *testing.T) {
	_, env := setupHome(t)
	manifest := manifestForLinkMappingSelection()

	selected, err := ResolveSelectors(manifest, ResolveOptions{
		Selectors: []Selector{
			mustParseSelector(t, "scripts"),
			mustParseSelector(t, "scripts/docx2pdf"),
		},
	}, env)

	requireNoError(t, err)
	requireSelectedMappings(t, selected, []string{
		"scripts:~/.local/bin/docx2pdf",
		"scripts:~/.local/bin/sesh-fzf",
	})
}

func TestResolveSelectorsRejectsUnknownPackage(t *testing.T) {
	_, env := setupHome(t)
	manifest := manifestForLinkMappingSelection()

	_, err := ResolveSelectors(manifest, ResolveOptions{
		Selectors: []Selector{mustParseSelector(t, "ghostty")},
	}, env)

	requireErrorContains(t, err, "unknown package")
}

func TestResolveSelectorsRejectsUnknownSourceInKnownPackage(t *testing.T) {
	_, env := setupHome(t)
	manifest := manifestForLinkMappingSelection()

	_, err := ResolveSelectors(manifest, ResolveOptions{
		Selectors: []Selector{mustParseSelector(t, "scripts/missing")},
	}, env)

	requireErrorContains(t, err, "unknown source")
}

func TestResolveSelectorsTargetsNarrowOneSelector(t *testing.T) {
	home, env := setupHome(t)
	manifest := manifestForLinkMappingSelection()

	selected, err := ResolveSelectors(manifest, ResolveOptions{
		Selectors: []Selector{mustParseSelector(t, "scripts")},
		Targets: []string{
			filepath.Join(home, ".local", "bin", "sesh-fzf"),
			"~/.local/bin/sesh-fzf",
		},
	}, env)

	requireNoError(t, err)
	requireSelectedMappings(t, selected, []string{"scripts:~/.local/bin/sesh-fzf"})
}

func TestResolveSelectorsRejectsTargetsWithMultipleSelectors(t *testing.T) {
	_, env := setupHome(t)
	manifest := manifestForLinkMappingSelection()

	_, err := ResolveSelectors(manifest, ResolveOptions{
		Selectors: []Selector{
			mustParseSelector(t, "scripts/docx2pdf"),
			mustParseSelector(t, "scripts/sesh-fzf"),
		},
		Targets: []string{"~/.local/bin/docx2pdf"},
	}, env)

	requireErrorContains(t, err, "--target cannot be combined with multiple selectors")
}

func mustParseSelector(t *testing.T, arg string) Selector {
	t.Helper()
	selector, err := ParseSelector(arg)
	requireNoError(t, err)
	return selector
}
