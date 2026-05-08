package dotty

import "testing"

func TestResolvePackageSelectionExpandsCollectionsAndDedupesInOrder(t *testing.T) {
	manifest := NewManifest()
	manifest.Packages["zsh"] = Package{}
	manifest.Packages["tmux"] = Package{}
	manifest.Packages["ghostty"] = Package{}
	manifest.Collections["terminal"] = Collection{Packages: []string{"tmux", "ghostty"}}

	selected, err := ResolvePackageSelection(manifest, []string{"zsh", "tmux"}, []string{"terminal"})
	requireNoError(t, err)
	requireEqualStrings(t, selected, []string{"zsh", "tmux", "ghostty"})
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
		{name: "invalid collection name", collections: []string{"bad.name"}, wantErr: "collection name"},
		{name: "unknown collection", collections: []string{"shell"}, wantErr: "unknown collection"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ResolvePackageSelection(manifest, tt.packages, tt.collections)
			requireErrorContains(t, err, tt.wantErr)
		})
	}
}
