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
	StateBlocked       State = "BLOCKED"
	StateMissingSource State = "MISSING SOURCE"
	StateEmpty         State = "EMPTY"
	StateUntracked     State = "UNTRACKED"
	StatePartial       State = "PARTIAL"
)

var statusFilterValues = []struct {
	value string
	state State
}{
	{value: "linked", state: StateLinked},
	{value: "unlinked", state: StateUnlinked},
	{value: "partial", state: StatePartial},
	{value: "conflict", state: StateConflict},
	{value: "blocked", state: StateBlocked},
	{value: "missing-source", state: StateMissingSource},
	{value: "empty", state: StateEmpty},
	{value: "untracked", state: StateUntracked},
}

var statusFilterValueByName = func() map[string]State {
	values := make(map[string]State, len(statusFilterValues))
	for _, item := range statusFilterValues {
		values[item.value] = item.state
	}
	return values
}()

func SupportedStatusFilterValues() []string {
	values := make([]string, 0, len(statusFilterValues))
	for _, item := range statusFilterValues {
		values = append(values, item.value)
	}
	return values
}

func ParseStatusFilterValue(value string) (State, error) {
	state, ok := statusFilterValueByName[value]
	if !ok {
		return "", fmt.Errorf(
			"unsupported status state %q (supported values: %s)",
			value,
			strings.Join(SupportedStatusFilterValues(), ", "),
		)
	}
	return state, nil
}

type StatusReport struct {
	RepoPath  string
	Packages  []PackageStatus
	Untracked []UntrackedItem
}

type PackageStatus struct {
	Name    string
	State   State
	Entries []EntryStatus
}

type EntryStatus struct {
	Package   string
	Source    string
	Target    string
	State     State
	BlockedBy string
}

type UntrackedItem struct {
	Path    string
	Package string
	Source  string
	State   State
}

func (s Service) Status(packageFilter []string) (*StatusReport, error) {
	manifest, err := LoadManifest(s.Repo, s.Env)
	if err != nil {
		return nil, err
	}
	selected, err := s.statusSelections(manifest, packageFilter)
	if err != nil {
		return nil, err
	}

	report := &StatusReport{RepoPath: HomeRelative(s.Repo, s.Env)}
	for _, selection := range selected {
		pkg := manifest.Packages[selection.Package]
		status := PackageStatus{Name: selection.Name()}
		for _, mapping := range pkg.Links {
			if !selection.IncludesSource(mapping.Source) {
				continue
			}
			entry := s.entryStatus(manifest, selection.Package, mapping)
			status.Entries = append(status.Entries, entry)
		}
		status.State = summarizePackage(status.Entries)
		report.Packages = append(report.Packages, status)
	}

	var untrackedSelections []statusSelection
	if len(packageFilter) > 0 {
		untrackedSelections = selected
	}
	untracked, err := s.untrackedContent(manifest, untrackedSelections)
	if err != nil {
		return nil, err
	}
	report.Untracked = untracked
	return report, nil
}

type statusSelection struct {
	Package string
	Source  string
}

func (s statusSelection) Name() string {
	if s.Source == "" {
		return s.Package
	}
	return s.Package + "/" + s.Source
}

func (s statusSelection) IncludesSource(source string) bool {
	if s.Source == "" {
		return true
	}
	return source == s.Source || strings.HasPrefix(source, s.Source+"/")
}

func (s Service) statusSelections(
	manifest *Manifest,
	packageFilter []string,
) ([]statusSelection, error) {
	if len(packageFilter) == 0 {
		selected := make([]statusSelection, 0, len(manifest.Packages))
		for _, packageName := range sortedKeys(manifest.Packages) {
			selected = append(selected, statusSelection{Package: packageName})
		}
		return selected, nil
	}

	selected := make([]statusSelection, 0, len(packageFilter))
	for _, raw := range packageFilter {
		selector, err := ParseSelector(raw)
		if err != nil {
			return nil, err
		}
		pkg, ok := manifest.Packages[selector.Package]
		if !ok {
			return nil, fmt.Errorf(
				"unknown package %q (run `dotty list` to see packages)",
				selector.Package,
			)
		}
		if selector.IsPackageSource() && !statusSourceExists(s.Repo, selector, pkg) {
			return nil, fmt.Errorf(
				"unknown source %q in package %q",
				selector.Source,
				selector.Package,
			)
		}
		selected = append(selected, statusSelection(selector))
	}
	return selected, nil
}

func statusSourceExists(repo string, selector Selector, pkg Package) bool {
	for _, link := range pkg.Links {
		if link.Source == selector.Source || strings.HasPrefix(link.Source, selector.Source+"/") {
			return true
		}
	}
	if exists, err := pathExists(
		filepath.Join(PackageRoot(repo, selector.Package), filepath.FromSlash(selector.Source)),
	); err == nil &&
		exists {
		return true
	}
	return false
}

func FilterStatusReport(report *StatusReport, selected []State) *StatusReport {
	if report == nil {
		return nil
	}
	filtered := &StatusReport{RepoPath: report.RepoPath}
	if len(selected) == 0 {
		filtered.Packages = clonePackageStatuses(report.Packages)
		filtered.Untracked = cloneUntrackedItems(report.Untracked)
		return filtered
	}

	selectedStates := make(map[State]bool, len(selected))
	for _, state := range selected {
		selectedStates[state] = true
	}
	for _, pkg := range report.Packages {
		if selectedStates[pkg.State] {
			filtered.Packages = append(filtered.Packages, clonePackageStatus(pkg))
		}
	}
	if selectedStates[StateUntracked] {
		filtered.Untracked = cloneUntrackedItems(report.Untracked)
	}
	return filtered
}

func clonePackageStatus(pkg PackageStatus) PackageStatus {
	cloned := pkg
	if pkg.Entries != nil {
		cloned.Entries = append([]EntryStatus(nil), pkg.Entries...)
	}
	return cloned
}

func clonePackageStatuses(packages []PackageStatus) []PackageStatus {
	if packages == nil {
		return nil
	}
	cloned := make([]PackageStatus, 0, len(packages))
	for _, pkg := range packages {
		cloned = append(cloned, clonePackageStatus(pkg))
	}
	return cloned
}

func cloneUntrackedItems(items []UntrackedItem) []UntrackedItem {
	if items == nil {
		return nil
	}
	return append([]UntrackedItem(nil), items...)
}

func (s Service) entryStatus(
	manifest *Manifest,
	packageName string,
	mapping LinkMapping,
) EntryStatus {
	entry := EntryStatus{Package: packageName, Source: mapping.Source, Target: mapping.Target}
	sourceAbs, err := PackageSourcePath(s.Repo, packageName, mapping.Source)
	if err != nil {
		entry.State = StateConflict
		return entry
	}
	if exists, err := sourcePathExists(sourceAbs); err != nil {
		entry.State = StateConflict
		return entry
	} else if !exists {
		entry.State = StateMissingSource
		return entry
	}
	if err := validateSupportedSourcePath(sourceAbs); err != nil {
		entry.State = StateConflict
		return entry
	}
	if externalHardlinks, err := hasHardlinksOutsideRoot(
		sourceAbs,
		s.Repo,
	); err != nil ||
		externalHardlinks {
		entry.State = StateConflict
		return entry
	}
	targetAbs, err := ExpandTargetPath(mapping.Target, s.Env)
	if err != nil {
		entry.State = StateConflict
		return entry
	}
	if err := validateTargetParentsAreLexicalDirectories(targetAbs, s.Env); err != nil {
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
	} else if blocker, ok, err := s.blockingPackageForTarget(manifest, packageName, targetAbs); err == nil && ok {
		entry.State = StateBlocked
		entry.BlockedBy = blocker
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
	if counts[StateBlocked] > 0 {
		return StateBlocked
	}
	if counts[StateLinked] == len(entries) {
		return StateLinked
	}
	if counts[StateUnlinked] == len(entries) {
		return StateUnlinked
	}
	return StatePartial
}

func (s Service) untrackedContent(
	manifest *Manifest,
	selections []statusSelection,
) ([]UntrackedItem, error) {
	if len(selections) == 0 {
		return s.untrackedRepositoryContent(manifest)
	}
	seen := map[string]bool{}
	var untracked []UntrackedItem
	for _, selection := range selections {
		pkg := manifest.Packages[selection.Package]
		items, err := s.untrackedPackageContent(selection.Package, pkg)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if selection.Source != "" && !selection.IncludesSource(item.Source) {
				continue
			}
			if seen[item.Path] {
				continue
			}
			seen[item.Path] = true
			untracked = append(untracked, item)
		}
	}
	sort.Slice(untracked, func(i, j int) bool { return untracked[i].Path < untracked[j].Path })
	return untracked, nil
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
		items, err := s.untrackedPackageContent(name, pkg)
		if err != nil {
			return nil, err
		}
		untracked = append(untracked, items...)
	}
	sort.Slice(untracked, func(i, j int) bool { return untracked[i].Path < untracked[j].Path })
	return untracked, nil
}

func (s Service) untrackedPackageContent(packageName string, pkg Package) ([]UntrackedItem, error) {
	trackedSources := trackedSourcePrefixes(pkg)
	if trackedSources["."] {
		return nil, nil
	}
	return untrackedUnderPackage(PackageRoot(s.Repo, packageName), packageName, trackedSources)
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
		if d.IsDir() && isParentOfTrackedSource(rel, tracked) {
			return nil
		}
		items = append(
			items,
			UntrackedItem{
				Path:    filepath.ToSlash(filepath.Join(packageName, rel)),
				Package: packageName,
				Source:  rel,
				State:   StateUntracked,
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

func isParentOfTrackedSource(rel string, tracked map[string]bool) bool {
	rel = filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	for source := range tracked {
		if strings.HasPrefix(source, rel+"/") {
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
