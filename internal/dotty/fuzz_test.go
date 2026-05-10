package dotty

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func FuzzValidateName(f *testing.F) {
	for _, seed := range []string{"zsh", "tmux_1", "bad.name", "", "-bad"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		err := validateName("package", name)
		if err == nil && !isValidNameByRules(name) {
			t.Fatalf("accepted invalid name %q", name)
		}
	})
}

func FuzzValidateSourcePath(f *testing.F) {
	for _, seed := range []string{".", ".zshrc", "config/tmux.conf", "../secret", "/tmp/file", "~/.zshrc"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		err := validateSourcePath(source)
		if err == nil && !isValidSourceByRules(source) {
			t.Fatalf("accepted invalid source %q", source)
		}
	})
}

func FuzzValidateTargetPath(f *testing.F) {
	for _, seed := range []string{"~", "~/.zshrc", "/tmp/file", "relative", "~other/file", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, target string) {
		err := validateTargetPath(target)
		if err == nil && !isValidTargetByRules(target) {
			t.Fatalf("accepted invalid target %q", target)
		}
	})
}

func FuzzFormatManifestRoundTrip(f *testing.F) {
	for _, seed := range []struct{ source, target string }{
		{source: ".", target: "~/.config/tmux"},
		{source: ".zshrc", target: "~/.zshrc"},
		{source: "quoted\"source", target: "~/quoted\"target"},
	} {
		f.Add(seed.source, seed.target)
	}
	f.Fuzz(func(t *testing.T, source, target string) {
		if validateSourcePath(source) != nil || validateTargetPath(target) != nil {
			return
		}
		manifest := NewManifest()
		manifest.Packages["pkg"] = Package{Links: []LinkMapping{{Source: source, Target: target}}}

		var parsed Manifest
		if err := toml.Unmarshal([]byte(FormatManifest(manifest)), &parsed); err != nil {
			t.Fatalf("formatted manifest did not parse: %v", err)
		}
		parsed.normalize()
		links := parsed.Packages["pkg"].Links
		if len(links) != 1 || links[0].Source != source || links[0].Target != target {
			t.Fatalf("manifest did not round trip: %#v", links)
		}
	})
}

func isValidNameByRules(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		valid := r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' ||
			r == '-'
		if !valid || i == 0 && (r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func isValidSourceByRules(source string) bool {
	if source == "" || filepath.IsAbs(source) || strings.HasPrefix(source, "~") {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(source))
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func isValidTargetByRules(target string) bool {
	if target == "" {
		return false
	}
	if strings.HasPrefix(target, "~") {
		return target == "~" || strings.HasPrefix(target, "~/")
	}
	return filepath.IsAbs(target)
}
