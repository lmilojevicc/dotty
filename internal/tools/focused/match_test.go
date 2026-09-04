package main

import "testing"

func TestFullSelection(t *testing.T) {
	cases := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"Plain", "TestPlain", true},
		{"Tree/branch/leaf", "TestTree", false},
		{"Tree/branch/leaf", "TestTree/branch", false},
		{"Tree/branch/leaf", "TestTree/branch/leaf", true},
		{"Tree/[b/].*/leaf", "TestTree/branch/leaf", true},
		{`Tree/branch\/leaf`, "TestTree/branch/leaf", false},
		{"Tree/(branch/leaf)", "TestTree/branch/leaf", false},
		{"Tree/(branch|other)/leaf", "TestTree/branch/leaf", true},
		{"Tree/absent|Plain", "TestPlain", true},
		{"Plain/absent|Plain", "TestPlain", false},
		{"Plain|Plain/absent", "TestPlain", true},
		{"Tree//leaf", "TestTree/branch/leaf", true},
		{"Tree/white space", "TestTree/white_space", true},
		{"Tree/white\u00a0space", "TestTree/white_space", true},
		{"Tree/white\x01space", `TestTree/white\x01space`, false},
		{"Tree/]branch", "TestTree/]branch", true},
	}
	for _, tc := range cases {
		t.Run(tc.pattern+":"+tc.name, func(t *testing.T) {
			selection, err := compileSelection(tc.pattern)
			if err != nil {
				t.Fatal(err)
			}
			if got := selection.fullMatch(tc.name); got != tc.want {
				t.Errorf("fullMatch(%q, %q) = %v, want %v", tc.pattern, tc.name, got, tc.want)
			}
		})
	}
}
