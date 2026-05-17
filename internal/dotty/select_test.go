package dotty

import (
	"path/filepath"
	"testing"
)

func TestResolvePackageSelectionExpandsCollectionsAndDedupesInOrder(t *testing.T) {
	manifest := NewManifest()
	manifest.Packages["zsh"] = Package{}
	manifest.Packages["tmux"] = Package{}
	manifest.Packages["ghostty"] = Package{}
	manifest.Collections["terminal"] = Collection{Packages: []string{"tmux", "ghostty"}}
	manifest.Collections["shell"] = Collection{Packages: []string{"zsh", "ghostty"}}

	selected, err := ResolvePackageSelection(
		manifest,
		[]string{"zsh", "tmux"},
		[]string{"terminal", "shell", "terminal"},
		false,
	)
	requireNoError(t, err)
	requireEqualStrings(t, selected, []string{"zsh", "tmux", "ghostty"})
}

func TestResolvePackageSelectionAllowsEmptyCollectionsAsNoOp(t *testing.T) {
	manifest := NewManifest()
	manifest.Packages["zsh"] = Package{}
	manifest.Collections["empty"] = Collection{Packages: nil}

	selected, err := ResolvePackageSelection(manifest, nil, []string{"empty"}, false)
	requireNoError(t, err)
	requireEqualStrings(t, selected, nil)
}

func TestResolvePackageSelectionSelectsAllPackagesInSortedOrder(t *testing.T) {
	manifest := NewManifest()
	manifest.Packages["zsh"] = Package{}
	manifest.Packages["tmux"] = Package{}
	manifest.Packages["ghostty"] = Package{}

	selected, err := ResolvePackageSelection(manifest, nil, nil, true)
	requireNoError(t, err)
	requireEqualStrings(t, selected, []string{"ghostty", "tmux", "zsh"})
}

func TestResolvePackageSelectionRejectsAllWithExplicitSelections(t *testing.T) {
	manifest := NewManifest()
	manifest.Packages["zsh"] = Package{}
	manifest.Collections["terminal"] = Collection{Packages: []string{"zsh"}}

	tests := []struct {
		name        string
		packages    []string
		collections []string
	}{
		{name: "package", packages: []string{"zsh"}},
		{name: "collection", collections: []string{"terminal"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolvePackageSelection(manifest, tt.packages, tt.collections, true)
			requireErrorContains(t, err, "--all cannot be combined")
		})
	}
}

func TestResolvePackageSelectionRejectsInvalidSelections(t *testing.T) {
	manifest := NewManifest()
	manifest.Packages["zsh"] = Package{}
	manifest.Collections["terminal"] = Collection{Packages: []string{"zsh"}}

	tests := []struct {
		name        string
		packages    []string
		collections []string
		wantErr     string
	}{
		{name: "no selection", wantErr: "select at least one package or collection"},
		{name: "invalid package name", packages: []string{"bad.name"}, wantErr: "package name"},
		{name: "unknown package", packages: []string{"tmux"}, wantErr: "unknown package"},
		{
			name:        "invalid collection name",
			collections: []string{"bad.name"},
			wantErr:     "collection name",
		},
		{name: "unknown collection", collections: []string{"shell"}, wantErr: "unknown collection"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolvePackageSelection(manifest, tt.packages, tt.collections, false)
			requireErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestResolveSelectedLinkMappingsDefaultsToAllMappingsInSelectedPackageOrder(t *testing.T) {
	_, env := setupHome(t)
	manifest := manifestForLinkMappingSelection()

	selected, err := ResolveSelectedLinkMappings(
		manifest,
		[]string{"scripts"},
		[]string{"terminal"},
		false,
		nil,
		env,
	)
	requireNoError(t, err)
	requireSelectedMappings(t, selected, []string{
		"scripts:~/.local/bin/docx2pdf",
		"scripts:~/.local/bin/sesh-fzf",
		"tmux:~/.config/tmux",
	})
}

func TestResolveSelectedLinkMappingsNarrowsByNormalizedTargetsAndDedupes(t *testing.T) {
	home, env := setupHome(t)
	manifest := manifestForLinkMappingSelection()

	selected, err := ResolveSelectedLinkMappings(
		manifest,
		[]string{"scripts"},
		nil,
		false,
		[]string{
			filepath.Join(home, ".local", "bin", "sesh-fzf"),
			"~/.local/bin/sesh-fzf",
		},
		env,
	)
	requireNoError(t, err)
	requireSelectedMappings(t, selected, []string{"scripts:~/.local/bin/sesh-fzf"})
}

func TestResolveSelectedLinkMappingsUsesCollectionAndAllScope(t *testing.T) {
	_, env := setupHome(t)
	manifest := manifestForLinkMappingSelection()

	selected, err := ResolveSelectedLinkMappings(
		manifest,
		nil,
		[]string{"terminal"},
		false,
		[]string{"~/.config/tmux"},
		env,
	)
	requireNoError(t, err)
	requireSelectedMappings(t, selected, []string{"tmux:~/.config/tmux"})

	selected, err = ResolveSelectedLinkMappings(
		manifest,
		nil,
		nil,
		true,
		[]string{"~/.config/tmux"},
		env,
	)
	requireNoError(t, err)
	requireSelectedMappings(t, selected, []string{"tmux:~/.config/tmux"})
}

func TestResolveSelectedLinkMappingsRejectsTargetsOutsideSelectedScope(t *testing.T) {
	_, env := setupHome(t)
	manifest := manifestForLinkMappingSelection()

	_, err := ResolveSelectedLinkMappings(
		manifest,
		[]string{"scripts"},
		nil,
		false,
		[]string{"~/.config/tmux"},
		env,
	)
	requireErrorContains(
		t,
		err,
		"target \"~/.config/tmux\" is not mapped in the selected package scope",
	)
}

func TestResolveSelectedLinkMappingsRejectsInvalidTargetSelectors(t *testing.T) {
	_, env := setupHome(t)
	manifest := manifestForLinkMappingSelection()

	_, err := ResolveSelectedLinkMappings(
		manifest,
		[]string{"scripts"},
		nil,
		false,
		[]string{"relative/target"},
		env,
	)
	requireErrorContains(t, err, "must be absolute or home-relative")
}

func manifestForLinkMappingSelection() *Manifest {
	manifest := NewManifest()
	manifest.Packages["scripts"] = Package{Links: []LinkMapping{
		{Source: "docx2pdf", Target: "~/.local/bin/docx2pdf"},
		{Source: "sesh-fzf", Target: "~/.local/bin/sesh-fzf"},
	}}
	manifest.Packages["tmux"] = Package{Links: []LinkMapping{
		{Source: ".", Target: "~/.config/tmux"},
	}}
	manifest.Collections["terminal"] = Collection{Packages: []string{"tmux"}}
	return manifest
}

func requireSelectedMappings(t *testing.T, got []SelectedLinkMapping, want []string) {
	t.Helper()
	actual := make([]string, 0, len(got))
	for _, item := range got {
		actual = append(actual, item.Package+":"+item.Link.Target)
	}
	requireEqualStrings(t, actual, want)
}
