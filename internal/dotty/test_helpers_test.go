package dotty

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func requireErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got %q", want, err.Error())
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func requireEqualStrings(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length mismatch: want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("item %d mismatch: want %v, got %v", i, want, got)
		}
	}
}

func writeTextFile(t *testing.T, path, content string) {
	t.Helper()
	requireNoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	requireNoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func requireFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	requireNoError(t, err)
	if got := string(data); got != want {
		t.Fatalf("file content mismatch for %s: want %q, got %q", path, want, got)
	}
}

func requireNoPath(t *testing.T, path string) {
	t.Helper()
	_, err := os.Lstat(path)
	if err == nil {
		t.Fatalf("expected %s to be absent", path)
	}
	if !os.IsNotExist(err) {
		t.Fatalf("inspect %s: %v", path, err)
	}
}

func writeDottyManifest(t *testing.T, repo, content string) {
	t.Helper()
	writeTextFile(t, ManifestPath(repo), content)
}

func assertZshPackageState(t *testing.T, svc Service, want State) {
	t.Helper()
	report, err := svc.Status([]string{"zsh"})
	requireNoError(t, err)
	if len(report.Packages) != 1 {
		t.Fatalf("expected one package status, got %d", len(report.Packages))
	}
	if got := report.Packages[0].State; got != want {
		t.Fatalf("state mismatch for zsh: want %s, got %s", want, got)
	}
}
