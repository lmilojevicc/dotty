package dotty

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUntrackRemovesAllMappingsForSource(t *testing.T) {
	home, repo, env := setupUntrackTest(t)
	target := filepath.Join(home, ".local", "bin", "docx2pdf")
	writeTextFile(t, filepath.Join(repo, "scripts", "docx2pdf"), "#!/bin/sh\n")
	requireNoError(t, osSymlinkForTest(filepath.Join(repo, "scripts", "docx2pdf"), target))

	results, err := NewService(repo, env).Untrack(UntrackOptions{
		Selector: mustParseSelector(t, "scripts/docx2pdf"),
	})

	requireNoError(t, err)
	requireUntrackResults(t, results, []string{
		"scripts:docx2pdf:~/.local/bin/docx2pdf:true",
		"scripts:docx2pdf:~/bin/docx2pdf:false",
	})
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.scripts]
links = [
  { source = "sesh-fzf", target = "~/.local/bin/sesh-fzf" },
]
`)
	assertSymlink(t, target, filepath.Join(repo, "scripts", "docx2pdf"))
}

func TestUntrackRemovesOneTargetFromMultiTargetSource(t *testing.T) {
	_, repo, env := setupUntrackTest(t)

	results, err := NewService(repo, env).Untrack(UntrackOptions{
		Selector: mustParseSelector(t, "scripts/docx2pdf"),
		Targets:  []string{"~/bin/docx2pdf"},
	})

	requireNoError(t, err)
	requireUntrackResults(t, results, []string{"scripts:docx2pdf:~/bin/docx2pdf:false"})
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
  { source = "sesh-fzf", target = "~/.local/bin/sesh-fzf" },
]
`)
}

func TestUntrackPackageSelectorRemovesAllMappingsAndLeavesEmptyPackage(t *testing.T) {
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
]
`)

	results, err := NewService(repo, env).Untrack(UntrackOptions{
		Selector: mustParseSelector(t, "scripts"),
	})

	requireNoError(t, err)
	requireUntrackResults(t, results, []string{"scripts:docx2pdf:~/.local/bin/docx2pdf:false"})
	requireFileContent(t, ManifestPath(repo), `version = 1

[packages.scripts]
links = []
`)
}

func TestUntrackRejectsTargetOutsideSelectedScopeWithoutWritingManifest(t *testing.T) {
	_, repo, env := setupUntrackTest(t)
	manifestBefore := `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
  { source = "docx2pdf", target = "~/bin/docx2pdf" },
  { source = "sesh-fzf", target = "~/.local/bin/sesh-fzf" },
]
`

	_, err := NewService(repo, env).Untrack(UntrackOptions{
		Selector: mustParseSelector(t, "scripts/docx2pdf"),
		Targets:  []string{"~/.local/bin/sesh-fzf"},
	})

	requireErrorContains(t, err, "not mapped in the selected")
	requireFileContent(t, ManifestPath(repo), manifestBefore)
}

func setupUntrackTest(t *testing.T) (string, string, Env) {
	t.Helper()
	home, env := setupHome(t)
	repo := filepath.Join(home, "dotfiles")
	writeDottyManifest(t, repo, `version = 1

[packages.scripts]
links = [
  { source = "docx2pdf", target = "~/.local/bin/docx2pdf" },
  { source = "docx2pdf", target = "~/bin/docx2pdf" },
  { source = "sesh-fzf", target = "~/.local/bin/sesh-fzf" },
]
`)
	return home, repo, env
}

func osSymlinkForTest(oldname, newname string) error {
	if err := os.MkdirAll(filepath.Dir(newname), 0o755); err != nil {
		return err
	}
	return os.Symlink(oldname, newname)
}

func requireUntrackResults(t *testing.T, got []UntrackResult, want []string) {
	t.Helper()
	actual := make([]string, 0, len(got))
	for _, result := range got {
		actual = append(
			actual,
			result.Package+":"+result.Source+":"+result.Target+":"+boolString(result.LinkExists),
		)
	}
	requireEqualStrings(t, actual, want)
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
