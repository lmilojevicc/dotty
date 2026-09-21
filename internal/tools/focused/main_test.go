package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// These regressions run the real Go test driver, independently of test:focused.
// Fixtures live outside ./... so their intentional failures cannot pollute it.
func TestFocusedRunnerGoExecution(t *testing.T) {
	requireFocusedPlatform(t)
	root := focusedFixture(t)
	cases := []struct {
		name     string
		pkg      string
		pattern  string
		goFlags  string
		wantCode int
		want     []string
		absent   []string
	}{
		{
			name:    "unanchored",
			pkg:     "./first",
			pattern: "Plain",
			want:    []string{"fixture/first TestPlain"},
		},
		{name: "zero match", pkg: "./first", pattern: "DoesNotExist", wantCode: 1},
		{name: "missing child", pkg: "./first", pattern: "^TestTree$/absent", wantCode: 1},
		{
			name:     "nested missing child",
			pkg:      "./first",
			pattern:  "^TestTree$/branch/absent",
			wantCode: 1,
		},
		{
			name:    "nested child",
			pkg:     "./first",
			pattern: "Tree/branch/leaf",
			want:    []string{"fixture/first TestTree/branch/leaf"},
			absent:  []string{"fixture/first TestTree\n", "fixture/first TestTree/branch\n"},
		},
		{name: "skipped parent", pkg: "./first", pattern: "^TestSkipped$", wantCode: 1},
		{name: "skipped child", pkg: "./first", pattern: "Tree/skipped", wantCode: 1},
		{name: "skipped ancestor", pkg: "./first", pattern: "Skipped/child", wantCode: 1},
		{
			name:    "mixed",
			pkg:     "./first",
			pattern: "Plain|Skipped",
			want:    []string{"fixture/first TestPlain"},
			absent:  []string{"fixture/first TestSkipped"},
		},
		{
			name:    "alternation",
			pkg:     "./first",
			pattern: "Tree/absent|Plain",
			want:    []string{"fixture/first TestPlain"},
		},
		{
			name:     "first partial alternation",
			pkg:      "./first",
			pattern:  "Plain/absent|Plain",
			wantCode: 1,
		},
		{
			name:    "grouped alternation",
			pkg:     "./first",
			pattern: "Tree/(branch|other)/leaf",
			want:    []string{"fixture/first TestTree/branch/leaf"},
		},
		{
			name:    "slash class",
			pkg:     "./first",
			pattern: "Tree/[b/].*/leaf",
			want:    []string{"fixture/first TestTree/branch/leaf"},
		},
		{
			name:     "escaped slash does not split",
			pkg:      "./first",
			pattern:  `Tree/branch\/leaf`,
			wantCode: 1,
		},
		{
			name:     "grouped slash does not split",
			pkg:      "./first",
			pattern:  "Tree/(branch/leaf)",
			wantCode: 1,
		},
		{
			name:    "empty hierarchy component",
			pkg:     "./first",
			pattern: "Tree//leaf",
			want:    []string{"fixture/first TestTree/branch/leaf"},
		},
		{
			name:    "name rewriting",
			pkg:     "./first",
			pattern: "Tree/white space",
			want:    []string{"fixture/first TestTree/white_space"},
		},
		{
			name:    "slash in emitted name",
			pkg:     "./first",
			pattern: "Tree/path/part",
			want:    []string{"fixture/first TestTree/path/part"},
		},
		{
			name:    "parallel child",
			pkg:     "./first",
			pattern: "Tree/parallel",
			want:    []string{"fixture/first TestTree/parallel"},
		},
		{
			name:    "quoted argument",
			pkg:     "./first",
			pattern: `Tree/quote'\";\$`,
			want:    []string{"fixture/first TestTree/quote'\";$"},
		},
		{
			name:    "package aggregation",
			pkg:     "./...",
			pattern: "OnlySecond",
			want:    []string{"fixture/second TestOnlySecond"},
		},
		{
			name:    "multiple matching packages",
			pkg:     "./...",
			pattern: "^TestPlain$",
			want:    []string{"fixture/first TestPlain", "fixture/second TestPlain"},
		},
		{name: "package error", pkg: "./missing", pattern: "Plain", wantCode: 1},
		{
			name:     "test failure",
			pkg:      "./first",
			pattern:  "^TestFailure$",
			wantCode: 1,
			want:     []string{"fixture/first TestFailure"},
		},
		{
			name:     "mixed pass failure skip",
			pkg:      "./first",
			pattern:  "Plain|Failure|Skipped",
			wantCode: 1,
			want:     []string{"fixture/first TestPlain", "fixture/first TestFailure"},
			absent:   []string{"fixture/first TestSkipped"},
		},
		{
			name:    "generated duplicate names",
			pkg:     "./first",
			pattern: "Tree/duplicate",
			want: []string{
				"fixture/first TestTree/duplicate",
				"fixture/first TestTree/duplicate#01",
			},
		},
		{
			name: "generated empty name", pkg: "./first", pattern: "Tree/#00",
			want: []string{"fixture/first TestTree/#00"},
		},
		{name: "build failure", pkg: "./broken", pattern: "Plain", wantCode: 1},
		{name: "invalid pattern", pkg: "./first", pattern: "[", wantCode: 1},
		{
			name:     "inherited skip",
			pkg:      "./first",
			pattern:  "Plain",
			goFlags:  "-skip=Plain",
			wantCode: 1,
		},
		{
			name:    "inherited zero count overridden",
			pkg:     "./first",
			pattern: "Plain",
			goFlags: "-count=0",
			want:    []string{"fixture/first TestPlain"},
		},
		{
			name:    "inherited run overridden",
			pkg:     "./first",
			pattern: "Plain",
			goFlags: "-run=DoesNotExist",
			want:    []string{"fixture/first TestPlain"},
		},
		{
			name:     "list is not execution",
			pkg:      "./first",
			pattern:  "Plain",
			goFlags:  "-list=.",
			wantCode: 1,
		},
		{
			name:     "compile is not execution",
			pkg:      "./first",
			pattern:  "Plain",
			goFlags:  "-c",
			wantCode: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The broken package is introduced only for its own case.
			if tc.pkg == "./broken" {
				writeFixtureFile(
					t,
					root,
					"broken/broken_test.go",
					"package broken\ninvalid syntax\n",
				)
				defer os.RemoveAll(filepath.Join(root, "broken"))
			}
			t.Setenv("GOFLAGS", tc.goFlags)
			var out, diagnostics bytes.Buffer
			code := run([]string{tc.pkg, tc.pattern}, &out, &diagnostics)
			if code != tc.wantCode {
				t.Fatalf(
					"exit = %d, want %d\nstdout:\n%s\nstderr:\n%s",
					code,
					tc.wantCode,
					&out,
					&diagnostics,
				)
			}
			for _, want := range tc.want {
				if !strings.Contains(out.String(), "executed: "+want+"\n") {
					t.Errorf("missing execution %q in:\n%s", want, &out)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(out.String(), "executed: "+absent) {
					t.Errorf("unexpected execution %q in:\n%s", absent, &out)
				}
			}
		})
	}
}

func TestFocusedRunnerArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"./..."}, {"", "Test"}, {"./...", ""}, {"./...", "Test", "extra"}} {
		var out bytes.Buffer
		if code := run(args, &out, &out); code != 2 {
			t.Errorf("run(%q) = %d, want usage exit 2", args, code)
		}
	}
}

func TestFocusedRunnerProtocolFailures(t *testing.T) {
	requireFocusedPlatform(t)
	for _, mode := range []string{"empty", "listing", "pass without run", "skipped", "unfinished", "malformed", "package failure", "exit"} {
		t.Run(mode, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestFocusedRunnerProcessFixture$")
			cmd.Env = append(os.Environ(), "DOTTY_FOCUSED_PROCESS_FIXTURE="+mode)
			var out bytes.Buffer
			wantCode := 1
			if mode == "exit" {
				wantCode = 7
			}
			if code := execute(cmd, "TestPlain", &out, &out); code != wantCode {
				t.Fatalf("exit = %d, want %d; output: %s", code, wantCode, &out)
			}
		})
	}
}

func TestFocusedRunnerMixedPackageCompletion(t *testing.T) {
	requireFocusedPlatform(t)
	for _, mode := range []string{"truncated", "partial parent", "complete empty", "complete partial", "complete skipped", "duplicate terminal", "terminal without start", "test after terminal", "test terminal without run"} {
		t.Run(mode, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestFocusedRunnerProcessFixture$")
			cmd.Env = append(os.Environ(), "DOTTY_FOCUSED_PROCESS_FIXTURE=mixed "+mode)
			var out bytes.Buffer
			want := 1
			if strings.HasPrefix(mode, "complete ") {
				want = 0
			}
			if code := execute(cmd, "TestPlain|TestTree/absent", &out, &out); code != want {
				t.Fatalf("exit = %d, want %d; output: %s", code, want, &out)
			}
		})
	}
}

func TestFocusedRunnerOutputFailure(t *testing.T) {
	requireFocusedPlatform(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestFocusedRunnerProcessFixture$")
	cmd.Env = append(os.Environ(), "DOTTY_FOCUSED_PROCESS_FIXTURE=success")
	var diagnostics bytes.Buffer
	if code := execute(cmd, "TestPlain", failingWriter{}, &diagnostics); code != 1 {
		t.Fatalf("exit = %d, want 1; output: %s", code, &diagnostics)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, fmt.Errorf("intentional output failure")
}

func TestFocusedRunnerInvocationFailure(t *testing.T) {
	var out bytes.Buffer
	cmd := exec.Command(filepath.Join(t.TempDir(), "missing-go"))
	if code := execute(cmd, "TestPlain", &out, &out); code != 1 {
		t.Fatalf("exit = %d, want 1; output: %s", code, &out)
	}
}

func TestFocusedRunnerProcessFixture(_ *testing.T) {
	mode := os.Getenv("DOTTY_FOCUSED_PROCESS_FIXTURE")
	if mode == "" {
		return
	}
	pkg := "fixture"
	emit := func(action, name string) {
		if err := json.NewEncoder(os.Stdout).
			Encode(map[string]string{"Action": action, "Package": pkg, "Test": name}); err != nil {
			os.Exit(8)
		}
	}
	if strings.HasPrefix(mode, "mixed ") {
		emit("start", "")
		emit("run", "TestPlain")
		emit("pass", "TestPlain")
		emit("pass", "")
		pkg = "other"
		if mode != "mixed terminal without start" {
			emit("start", "")
		}
		switch mode {
		case "mixed partial parent", "mixed complete partial":
			emit("run", "TestTree")
			emit("pass", "TestTree")
		case "mixed complete skipped":
			emit("skip", "")
			os.Exit(0)
		case "mixed test terminal without run":
			emit("pass", "TestPlain")
		}
		if mode != "mixed truncated" && mode != "mixed partial parent" {
			emit("pass", "")
		}
		if mode == "mixed duplicate terminal" {
			emit("pass", "")
		}
		if mode == "mixed test after terminal" {
			emit("run", "TestPlain")
			emit("pass", "TestPlain")
		}
		os.Exit(0)
	}
	emit("start", "")
	switch mode {
	case "listing":
		emit("output", "")
	case "success":
		emit("run", "TestPlain")
		emit("pass", "TestPlain")
		emit("pass", "")
	case "pass without run":
		emit("pass", "TestPlain")
		emit("pass", "")
	case "skipped":
		emit("run", "TestPlain")
		emit("skip", "TestPlain")
		emit("pass", "")
	case "unfinished":
		emit("run", "TestPlain")
		emit("pass", "TestPlain")
	case "malformed":
		fmt.Println("not JSON")
	case "package failure":
		emit("run", "TestPlain")
		emit("pass", "TestPlain")
		emit("fail", "")
	case "exit":
		os.Exit(7)
	}
	os.Exit(0)
}

func requireFocusedPlatform(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("focused process-group execution is supported on Linux and macOS only")
	}
}

func focusedFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// Keep the invoking toolchain's cache while isolating HOME; fixtures have no dependencies.
	if os.Getenv("GOCACHE") == "" {
		cache, err := exec.Command("go", "env", "GOCACHE").Output()
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("GOCACHE", strings.TrimSpace(string(cache)))
	}
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("DOTTY_REPO", "")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	t.Chdir(root)
	writeFixtureFile(t, root, "go.mod", "module fixture\n\ngo 1.26\n")
	writeFixtureFile(t, root, "first/first_test.go", `package first
import "testing"
func TestPlain(t *testing.T) {}
func TestFailure(t *testing.T) { t.Fatal("intentional failure") }
func TestSkipped(t *testing.T) { t.Skip("intentional skip") }
func TestTree(t *testing.T) {
 t.Run("branch", func(t *testing.T) { t.Run("leaf", func(t *testing.T) {}) })
 t.Run("skipped", func(t *testing.T) { t.Skip("intentional skip") })
 t.Run("white space", func(t *testing.T) {})
 t.Run("path/part", func(t *testing.T) {})
 t.Run("parallel", func(t *testing.T) { t.Parallel() })
 t.Run("duplicate", func(t *testing.T) {})
 t.Run("duplicate", func(t *testing.T) {})
 t.Run("", func(t *testing.T) {})
 t.Run("quote'\";$", func(t *testing.T) {})
}
`)
	writeFixtureFile(t, root, "second/second_test.go", `package second
import "testing"
func TestPlain(t *testing.T) {}
func TestOnlySecond(t *testing.T) {}
`)
	return root
}

func writeFixtureFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
