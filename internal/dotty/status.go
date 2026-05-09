package dotty

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type State string

const (
	StateLinked        State = "LINKED"
	StateUnlinked      State = "UNLINKED"
	StateConflict      State = "CONFLICT"
	StateMissingSource State = "MISSING SOURCE"
	StateEmpty         State = "EMPTY"
	StateUntracked     State = "UNTRACKED"
	StatePartial       State = "PARTIAL"
)

type StatusReport struct {
	Packages  []PackageStatus
	Untracked []UntrackedItem
}

type PackageStatus struct {
	Name    string
	State   State
	Entries []EntryStatus
}

type EntryStatus struct {
	Package string
	Source  string
	Target  string
	State   State
}

type UntrackedItem struct {
	Path  string
	State State
}

func (s Service) Status(packageFilter []string) (*StatusReport, error) {
	manifest, err := LoadManifest(s.Repo, s.Env)
	if err != nil {
		return nil, err
	}
	selected := sortedKeys(manifest.Packages)
	if len(packageFilter) > 0 {
		selected = []string{}
		for _, name := range packageFilter {
			if err := validateName("package", name); err != nil {
				return nil, err
			}
			if _, ok := manifest.Packages[name]; !ok {
				return nil, fmt.Errorf("unknown package %q", name)
			}
			selected = append(selected, name)
		}
	}

	report := &StatusReport{}
	for _, packageName := range selected {
		pkg := manifest.Packages[packageName]
		status := PackageStatus{Name: packageName}
		for _, mapping := range pkg.Links {
			entry := s.entryStatus(packageName, mapping)
			status.Entries = append(status.Entries, entry)
		}
		status.State = summarizePackage(status.Entries)
		report.Packages = append(report.Packages, status)
	}

	untracked, err := s.untrackedRepositoryContent(manifest)
	if err != nil {
		return nil, err
	}
	report.Untracked = untracked
	return report, nil
}

func (s Service) entryStatus(packageName string, mapping LinkMapping) EntryStatus {
	entry := EntryStatus{Package: packageName, Source: mapping.Source, Target: mapping.Target}
	sourceAbs, err := PackageSourcePath(s.Repo, packageName, mapping.Source)
	if err != nil {
		entry.State = StateMissingSource
		return entry
	}
	if exists, err := pathExists(sourceAbs); err != nil || !exists {
		entry.State = StateMissingSource
		return entry
	}
	targetAbs, err := ExpandTargetPath(mapping.Target, s.Env)
	if err != nil {
		entry.State = StateConflict
		return entry
	}
	info, err := os.Lstat(targetAbs)
	if err != nil {
		if os.IsNotExist(err) {
			entry.State = StateUnlinked
		} else {
			entry.State = StateConflict
		}
		return entry
	}
	if info.Mode()&os.ModeSymlink == 0 {
		entry.State = StateConflict
		return entry
	}
	if symlinkPointsTo(targetAbs, sourceAbs) {
		entry.State = StateLinked
	} else {
		entry.State = StateConflict
	}
	return entry
}

func summarizePackage(entries []EntryStatus) State {
	if len(entries) == 0 {
		return StateEmpty
	}
	counts := map[State]int{}
	for _, entry := range entries {
		counts[entry.State]++
	}
	if counts[StateMissingSource] > 0 {
		return StateMissingSource
	}
	if counts[StateConflict] > 0 {
		return StateConflict
	}
	if counts[StateLinked] == len(entries) {
		return StateLinked
	}
	if counts[StateUnlinked] == len(entries) {
		return StateUnlinked
	}
	return StatePartial
}

func (s Service) untrackedRepositoryContent(manifest *Manifest) ([]UntrackedItem, error) {
	entries, err := os.ReadDir(s.Repo)
	if err != nil {
		return nil, err
	}
	var untracked []UntrackedItem
	for _, entry := range entries {
		name := entry.Name()
		if isBuiltinRepoEntry(name) {
			continue
		}
		pkg, packageKnown := manifest.Packages[name]
		if !packageKnown {
			untracked = append(untracked, UntrackedItem{Path: name, State: StateUntracked})
			continue
		}
		trackedSources := trackedSourcePrefixes(pkg)
		if trackedSources["."] {
			continue
		}
		packageRoot := PackageRoot(s.Repo, name)
		items, err := untrackedUnderPackage(packageRoot, name, trackedSources)
		if err != nil {
			return nil, err
		}
		untracked = append(untracked, items...)
	}
	sort.Slice(untracked, func(i, j int) bool { return untracked[i].Path < untracked[j].Path })
	return untracked, nil
}

func trackedSourcePrefixes(pkg Package) map[string]bool {
	tracked := map[string]bool{}
	for _, link := range pkg.Links {
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(link.Source)))
		tracked[clean] = true
	}
	return tracked
}

func untrackedUnderPackage(
	packageRoot, packageName string,
	tracked map[string]bool,
) ([]UntrackedItem, error) {
	var items []UntrackedItem
	if exists, err := pathExists(packageRoot); err != nil || !exists {
		return items, err
	}
	if err := filepath.WalkDir(packageRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == packageRoot {
			return nil
		}
		rel, err := filepath.Rel(packageRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if isTrackedOrInsideTracked(rel, tracked) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		items = append(
			items,
			UntrackedItem{
				Path:  filepath.ToSlash(filepath.Join(packageName, rel)),
				State: StateUntracked,
			},
		)
		if d.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return items, nil
}

func isTrackedOrInsideTracked(rel string, tracked map[string]bool) bool {
	rel = filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	for source := range tracked {
		if source == rel || strings.HasPrefix(rel, source+"/") {
			return true
		}
	}
	return false
}

func isBuiltinRepoEntry(name string) bool {
	switch name {
	case ManifestFileName, ".git":
		return true
	default:
		return false
	}
}
