package dotty

import (
	"testing"
	"unicode/utf8"

	"github.com/pelletier/go-toml/v2"
)

func FuzzTOMLBasicStringRoundTrip(f *testing.F) {
	for _, seed := range []string{"", "ordinary", "\x00\a\v\x1f\x7f", "\t\b\f\r\n\"\\", "é漢字😀�", "\xff"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		encoded := tomlBasicString(value)
		if !utf8.ValidString(value) {
			if utf8.ValidString(encoded) {
				t.Fatalf("invalid bytes silently replaced: %q", encoded)
			}
			return
		}
		var cfg Config
		if err := toml.Unmarshal([]byte("repo = "+encoded+"\n"), &cfg); err != nil {
			t.Fatalf("basic string does not parse: %v", err)
		}
		if cfg.Repo != value {
			t.Fatalf("basic string changed: want %q, got %q", value, cfg.Repo)
		}
	})
}
