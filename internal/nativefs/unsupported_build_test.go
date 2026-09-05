//go:build !android && !ios && !js && !wasip1

package nativefs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const unsupportedBuildOutputCap = 64 * 1024

// Keep draining both compiler streams after the shared byte limit is reached.
// The mutex also permits stdout and stderr to be copied concurrently.
type unsupportedBuildOutput struct {
	mu        sync.Mutex
	data      [unsupportedBuildOutputCap]byte
	used      int
	discarded uint64
}

func (o *unsupportedBuildOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := copy(o.data[o.used:], p)
	o.used += n
	o.discarded += uint64(len(p) - n)
	return len(p), nil
}

func (o *unsupportedBuildOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	output := string(o.data[:o.used])
	if o.discarded != 0 {
		output += fmt.Sprintf("\n[compiler output truncated: %d bytes discarded]\n", o.discarded)
	}
	return output
}

func configureUnsupportedBuildEnvironment(t *testing.T, root string) {
	t.Helper()
	// Never trust inherited cache paths or execute an inherited cache helper.
	for _, key := range []string{"GOCACHE", "GOMODCACHE", "GOPATH"} {
		t.Setenv(key, filepath.Join(root, key))
	}
	t.Setenv("GOCACHEPROG", "")
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("DOTTY_REPO", filepath.Join(root, "repo"))
	t.Setenv("GOENV", "off")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOTMPDIR", root)
	t.Setenv("CGO_ENABLED", "0")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
}

func TestUnsupportedBuildOutputCap(t *testing.T) {
	for _, size := range []int{0, 17, unsupportedBuildOutputCap, unsupportedBuildOutputCap + 23} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			var output unsupportedBuildOutput
			input := strings.Repeat("x", size)
			n, err := output.Write([]byte(input))
			if err != nil || n != len(input) {
				t.Fatalf("Write = %d, %v; want %d, nil", n, err, len(input))
			}
			want := input
			if size > unsupportedBuildOutputCap {
				want = input[:unsupportedBuildOutputCap] + "\n[compiler output truncated: 23 bytes discarded]\n"
			}
			if got := output.String(); got != want {
				t.Fatalf("captured output differs: got %d bytes, want %d", len(got), len(want))
			}
			if size >= unsupportedBuildOutputCap {
				n, err = output.Write([]byte("more"))
				if err != nil || n != 4 {
					t.Fatalf("write after cap = %d, %v; want 4, nil", n, err)
				}
				want = input[:unsupportedBuildOutputCap] + fmt.Sprintf(
					"\n[compiler output truncated: %d bytes discarded]\n",
					size-unsupportedBuildOutputCap+4,
				)
				if got := output.String(); got != want {
					t.Fatal("write after cap did not preserve prefix and count discarded bytes")
				}
			}
		})
	}
}

func TestUnsupportedBuildEnvironmentOverridesInheritedValues(t *testing.T) {
	root := t.TempDir()
	want := map[string]string{
		"GOCACHE":         filepath.Join(root, "GOCACHE"),
		"GOMODCACHE":      filepath.Join(root, "GOMODCACHE"),
		"GOPATH":          filepath.Join(root, "GOPATH"),
		"GOCACHEPROG":     "",
		"HOME":            root,
		"USERPROFILE":     root,
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"DOTTY_REPO":      filepath.Join(root, "repo"),
		"GOENV":           "off",
		"GOWORK":          "off",
		"GOTOOLCHAIN":     "local",
		"GOFLAGS":         "",
		"GOTMPDIR":        root,
		"CGO_ENABLED":     "0",
		"GOPROXY":         "off",
		"GOSUMDB":         "off",
	}
	for key := range want {
		t.Setenv(key, "unwanted-inherited-value")
	}
	configureUnsupportedBuildEnvironment(t, root)
	for key, value := range want {
		if got := os.Getenv(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
}

// The host Go driver compiles the unsupported stubs; it never runs the foreign
// test binaries. Keep this test common to both targets so go test -c emits an
// artifact even though native_test.go is excluded. Compile-only cannot recurse.
// Mobile and WebAssembly targets cannot host this compiler subprocess test.
func TestUnsupportedBuildTargets(t *testing.T) {
	configureUnsupportedBuildEnvironment(t, t.TempDir())

	goName := "go"
	if runtime.GOOS == "windows" {
		goName += ".exe"
	}
	goDriver, err := exec.LookPath(goName)
	if err != nil {
		t.Fatalf("find host Go driver: %v", err)
	}
	goDriver, err = filepath.Abs(goDriver)
	if err != nil {
		t.Fatalf("resolve host Go driver path: %v", err)
	}
	for _, target := range []string{"freebsd", "windows"} {
		t.Run(target+"/amd64", func(t *testing.T) {
			t.Setenv("GOOS", target)
			t.Setenv("GOARCH", "amd64")
			artifact := filepath.Join(t.TempDir(), "nativefs.test")
			if target == "windows" {
				artifact += ".exe"
			}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, goDriver, "test", "-c", "-o", artifact, ".")
			cmd.WaitDelay = 5 * time.Second
			var output unsupportedBuildOutput
			cmd.Stdout = &output
			cmd.Stderr = &output
			err := cmd.Run()
			if err != nil {
				t.Fatalf(
					"compile-only %s/amd64: %v (context: %v)\n%s",
					target,
					err,
					ctx.Err(),
					output.String(),
				)
			}
			info, err := os.Lstat(artifact)
			if err != nil {
				t.Fatalf(
					"compile-only %s/amd64 missing artifact: %v\n%s",
					target,
					err,
					output.String(),
				)
			}
			if !info.Mode().IsRegular() || info.Size() == 0 {
				t.Fatalf(
					"compile-only %s/amd64 invalid artifact: %v\n%s",
					target,
					info,
					output.String(),
				)
			}
		})
	}
}
