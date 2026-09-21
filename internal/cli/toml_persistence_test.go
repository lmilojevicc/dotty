package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestStrictTOMLPersistenceCLI(t *testing.T) {
	t.Run("manifest refusals preserve all paths", func(t *testing.T) {
		for _, extension := range []string{"extra = true\n", "[extra]\n", "[packages.zsh.extra]\n", "# invalid \xff\n"} {
			for _, args := range [][]string{
				{"add", "ADD_TARGET", "newpkg"},
				{"track", "zsh/file", "~/new-target"},
				{"untrack", "zsh/file"},
				{"link", "zsh"},
				{"link", "zsh", "--force"},
				{"link", "zsh/file", "--track", "--target", "~/new-target"},
				{"unlink", "zsh"},
				{"unlink", "zsh", "--leave-copy"},
				{"unlink", "zsh", "--untrack"},
				{"unlink", "zsh", "--untrack", "--leave-copy"},
				{"list"},
				{"status"},
			} {
				t.Run(strings.Join(args, " ")+"/"+extension, func(t *testing.T) {
					home, repo := setupCLITest(t)
					source := filepath.Join(repo, "zsh", "file")
					target := filepath.Join(home, "target")
					addTarget := filepath.Join(home, "adopt")
					if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
						t.Fatal(err)
					}
					for path, data := range map[string]string{source: "source\n", target: "target conflict\n", addTarget: "adopt me\n"} {
						if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					document := "version = 1\n" + extension + "\n[packages.zsh]\nlinks = [{source = 'file', target = '~/target'}]\n"
					writeManifest(t, repo, document)
					manifestPath := filepath.Join(repo, "dotty.toml")
					if err := os.Chmod(manifestPath, 0o640); err != nil {
						t.Fatal(err)
					}
					before := snapshotPersistenceTree(t, home)
					command := append([]string{"--repo", repo}, args...)
					for i, arg := range command {
						if arg == "ADD_TARGET" {
							command[i] = addTarget
						}
					}
					stdout, rendered, err := executeCommandRenderedError(t, command...)
					if err == nil || stdout != "" {
						t.Fatalf("expected quiet refusal, stdout=%q err=%v", stdout, err)
					}
					if !strings.Contains(rendered, manifestPath) {
						t.Fatalf("missing file in diagnostic: %q", rendered)
					}
					if !strings.Contains(extension, "\xff") &&
						(!strings.Contains(rendered, "extra") || !strings.Contains(rendered, "remove unsupported fields")) {
						t.Fatalf("missing unsupported key/remediation: %q", rendered)
					}
					if after := snapshotPersistenceTree(
						t,
						home,
					); !reflect.DeepEqual(
						before,
						after,
					) {
						t.Fatalf(
							"refusal changed filesystem\nbefore: %#v\nafter: %#v",
							before,
							after,
						)
					}
				})
			}
		}
	})
	t.Run("config preflight and precedence", func(t *testing.T) {
		for _, document := range []string{"repo = '~/old'\nextra = true\n", "repo = '~/old'\n[extra]\n", "repo = [\n", "repo = '\xff'\n"} {
			t.Run(document, func(t *testing.T) {
				home, repo := setupCLITest(t)
				config := filepath.Join(home, ".config", "dotty", "config.toml")
				if err := os.WriteFile(config, []byte(document), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(config, 0o600); err != nil {
					t.Fatal(err)
				}
				before := snapshotPersistenceTree(t, home)
				for _, args := range [][]string{{"init", filepath.Join(home, "new-parent", "repo")}, {"init", repo}, {"list"}, {"status"}} {
					stdout, rendered, err := executeCommandRenderedError(t, args...)
					if err == nil || stdout != "" || !strings.Contains(rendered, config) {
						t.Fatalf(
							"expected config refusal: stdout=%q error=%q cause=%v",
							stdout,
							rendered,
							err,
						)
					}
					if after := snapshotPersistenceTree(
						t,
						home,
					); !reflect.DeepEqual(
						before,
						after,
					) {
						t.Fatalf("config refusal changed filesystem: %#v", after)
					}
				}
				for _, args := range [][]string{{"--repo", repo, "list"}, {"--help"}, {"completion", "bash"}} {
					_, stderr, err := executeCommand(args...)
					if err != nil || stderr != "" {
						t.Fatalf("unrelated config was read for %v: %v %q", args, err, stderr)
					}
				}
				t.Setenv("DOTTY_REPO", repo)
				_, stderr, err := executeCommand("list")
				if err != nil || stderr != "" {
					t.Fatalf("environment override read config: %v %q", err, stderr)
				}
				if after := snapshotPersistenceTree(t, home); !reflect.DeepEqual(before, after) {
					t.Fatal("read-only command changed filesystem")
				}
			})
		}
	})
	t.Run("completion quiet refusal", func(t *testing.T) {
		for _, kind := range []string{"manifest", "config"} {
			for _, document := range []string{"extra = true\n", "[extra]\n", "# invalid \xff\n"} {
				t.Run(kind+"/"+document, func(t *testing.T) {
					home, repo := setupCLITest(t)
					if kind == "manifest" {
						writeManifest(t, repo, "version = 1\n"+document)
					} else {
						config := filepath.Join(home, ".config", "dotty", "config.toml")
						if err := os.WriteFile(
							config,
							[]byte("repo = '~/dotfiles'\n"+document),
							0o600,
						); err != nil {
							t.Fatal(err)
						}
					}
					before := snapshotPersistenceTree(t, home)
					for _, args := range [][]string{{"link", ""}, {"unlink", "--collection", ""}, {"status", ""}, {"track", ""}} {
						choices, directive, stderr, err := executeCompletionResult(args...)
						if err != nil {
							t.Fatalf("completion error: %v", err)
						}
						requireCompletionEndedOnly(t, stderr)
						requireChoices(t, choices, nil)
						if directive != cobra.ShellCompDirectiveNoFileComp {
							t.Fatalf("completion directive: %v", directive)
						}
					}
					if after := snapshotPersistenceTree(
						t,
						home,
					); !reflect.DeepEqual(
						before,
						after,
					) {
						t.Fatal("completion changed filesystem")
					}
				})
			}
		}
	})
}

type persistencePathSnapshot struct {
	Mode    os.FileMode
	Content string
}

func snapshotPersistenceTree(t *testing.T, root string) map[string]persistencePathSnapshot {
	t.Helper()
	snapshot := map[string]persistencePathSnapshot{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		state := persistencePathSnapshot{Mode: info.Mode()}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			state.Content = string(data)
		} else if info.Mode()&os.ModeSymlink != 0 {
			state.Content, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}
		snapshot[path] = state
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
