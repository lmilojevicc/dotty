package dotty

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type LinkOptions struct {
	Packages    []string
	Selectors   []Selector
	Collections []string
	Targets     []string
	All         bool
	Force       bool
	Track       bool
	DryRun      bool
}

type LinkResult struct {
	Package    string
	Source     string
	Target     string
	SourcePath string
	Action     string
	Tracked    bool
	DryRun     bool
}

type linkAction struct {
	packageName string
	mapping     LinkMapping
	sourceAbs   string
	targetAbs   string
	state       linkTargetState
	tracked     bool
}

type linkTargetState int

const (
	linkTargetAbsent          linkTargetState = iota // target does not exist, create freely
	linkTargetAlreadyCorrect                         // target is the expected absolute symlink
	linkTargetRelativeCorrect                        // target points to source via relative path
	linkTargetWrongSymlink                           // target is a symlink to something else
	linkTargetNonSymlink                             // target is a regular file/dir (conflict)
)

func (s Service) Link(options LinkOptions) ([]LinkResult, error) {
	var plan *linkPlan
	if options.DryRun {
		var err error
		plan, err = s.planLink(options)
		if err != nil {
			return nil, err
		}
		return s.linkResults(plan, true), nil
	}
	if err := withRepositoryLock(s.Repo, func() error {
		return RunAtomic(func(tx *Tx) error {
			manifest, err := LoadManifest(s.Repo, s.Env)
			if err != nil {
				return err
			}
			plan, err = s.planLinkWithManifest(manifest, options)
			if err != nil {
				return err
			}
			if options.Track {
				if err := SaveManifest(tx, s.Repo, manifest, s.Env); err != nil {
					return err
				}
			}
			for i := range plan.actions {
				if err := s.executeLinkAction(tx, &plan.actions[i]); err != nil {
					return err
				}
			}
			return nil
		})
	}); err != nil {
		return nil, err
	}
	return s.linkResults(plan, false), nil
}

type linkPlan struct {
	actions []linkAction
}

func (s Service) planLink(options LinkOptions) (*linkPlan, error) {
	manifest, err := LoadManifest(s.Repo, s.Env)
	if err != nil {
		return nil, err
	}
	return s.planLinkWithManifest(manifest, options)
}

func (s Service) planLinkWithManifest(manifest *Manifest, options LinkOptions) (*linkPlan, error) {
	selected, err := s.resolveLinkSelections(manifest, options)
	if err != nil {
		return nil, err
	}
	if err := rejectCompetingSelectedLinkMappings(selected, s.Env); err != nil {
		return nil, err
	}

	plan := &linkPlan{}
	for _, mapping := range selected {
		action, err := s.classifyLinkAction(manifest, mapping.Package, mapping.Link, options.Force)
		if err != nil {
			return nil, err
		}
		action.tracked = options.Track && mapping.Added
		plan.actions = append(plan.actions, action)
	}
	return plan, nil
}

func (s Service) resolveLinkSelections(
	manifest *Manifest,
	options LinkOptions,
) ([]SelectedLinkMapping, error) {
	if options.Track {
		return s.resolveTrackedLinkSelections(manifest, options)
	}
	if len(options.Selectors) > 0 {
		selected, err := ResolveSelectors(manifest, ResolveOptions{
			Selectors:   options.Selectors,
			Collections: options.Collections,
			Targets:     options.Targets,
			All:         options.All,
		}, s.Env)
		if err != nil {
			return nil, s.hintTrackForUnknownSource(err, options)
		}
		return selected, nil
	}
	return ResolveSelectedLinkMappings(
		manifest,
		options.Packages,
		options.Collections,
		options.All,
		options.Targets,
		s.Env,
	)
}

func (s Service) hintTrackForUnknownSource(err error, options LinkOptions) error {
	if len(options.Targets) == 0 {
		return err
	}
	var unknownSource UnknownSourceError
	if !errors.As(err, &unknownSource) {
		return err
	}
	sourcePath, pathErr := PackageSourcePath(s.Repo, unknownSource.Package, unknownSource.Source)
	if pathErr != nil {
		return err
	}
	if exists, pathErr := pathExists(sourcePath); pathErr != nil || !exists {
		return err
	}
	return fmt.Errorf("%w (use --track if this is new repository content)", err)
}

func (s Service) resolveTrackedLinkSelections(
	manifest *Manifest,
	options LinkOptions,
) ([]SelectedLinkMapping, error) {
	if len(options.Targets) == 0 {
		return nil, fmt.Errorf("--track requires --target")
	}
	if options.All {
		return nil, fmt.Errorf("--track cannot be combined with --all")
	}
	if len(options.Collections) > 0 {
		return nil, fmt.Errorf("--track cannot be combined with --collection")
	}
	selectors := append([]Selector{}, options.Selectors...)
	for _, packageName := range options.Packages {
		selectors = append(selectors, Selector{Package: packageName})
	}
	if len(selectors) != 1 {
		return nil, fmt.Errorf("--track accepts exactly one selector")
	}
	tracked, err := s.planTrack(manifest, TrackOptions{
		Selector: selectors[0],
		Targets:  options.Targets,
		DryRun:   options.DryRun,
	})
	if err != nil {
		return nil, err
	}
	selected := make([]SelectedLinkMapping, 0, len(tracked))
	for _, item := range tracked {
		selected = append(selected, SelectedLinkMapping{
			Package: item.Package,
			Link: LinkMapping{
				Source: item.Source,
				Target: item.Target,
			},
			Added: item.Added,
		})
	}
	return selected, nil
}

func rejectCompetingSelectedLinkMappings(selected []SelectedLinkMapping, env Env) error {
	seen := map[string]SelectedLinkMapping{}
	for _, item := range selected {
		key, err := targetKey(item.Link.Target, env)
		if err != nil {
			return err
		}
		if previous, ok := seen[key]; ok {
			return fmt.Errorf(
				"selected packages compete for %s (link only one of: %s, %s)",
				item.Link.Target,
				selectorLabel(previous.Package, previous.Link.Source),
				selectorLabel(item.Package, item.Link.Source),
			)
		}
		seen[key] = item
	}
	return nil
}

func (s Service) classifyLinkAction(
	manifest *Manifest,
	packageName string,
	mapping LinkMapping,
	force bool,
) (linkAction, error) {
	action := linkAction{packageName: packageName, mapping: mapping}

	sourceAbs, err := PackageSourcePath(s.Repo, packageName, mapping.Source)
	if err != nil {
		return action, err
	}
	action.sourceAbs = sourceAbs
	if err := validateLinkMappingTopology(s.Repo, packageName, mapping, s.Env); err != nil {
		return action, err
	}

	if exists, err := sourcePathExists(sourceAbs); err != nil {
		return action, err
	} else if !exists {
		return action, fmt.Errorf(
			"%s is missing from the repository (restore it, or run `dotty untrack %s` to remove the manifest entry)",
			selectorLabel(packageName, mapping.Source),
			selectorLabel(packageName, mapping.Source),
		)
	}
	if err := validateSupportedSourcePath(sourceAbs); err != nil {
		return action, err
	}
	if externalHardlinks, err := hasHardlinksOutsideRoot(sourceAbs, s.Repo); err != nil {
		return action, err
	} else if externalHardlinks {
		return action, fmt.Errorf(
			"%s has external hardlink aliases (copy it into the repository before linking)",
			selectorLabel(packageName, mapping.Source),
		)
	}

	targetAbs, err := ExpandTargetPath(mapping.Target, s.Env)
	if err != nil {
		return action, err
	}
	action.targetAbs = targetAbs
	if err := validateTargetParentsAreLexicalDirectories(targetAbs, s.Env); err != nil {
		return action, err
	}

	info, err := os.Lstat(targetAbs)
	if err != nil {
		if os.IsNotExist(err) {
			action.state = linkTargetAbsent
			return action, nil
		}
		return action, fmt.Errorf("inspect target %s: %w", targetAbs, err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		if symlinkPointsTo(targetAbs, sourceAbs) {
			targetText, _ := os.Readlink(targetAbs)
			if targetText == sourceAbs {
				action.state = linkTargetAlreadyCorrect
			} else {
				action.state = linkTargetRelativeCorrect
			}
		} else {
			blocker, blocked, err := s.blockingPackageForTarget(manifest, packageName, targetAbs)
			if err != nil {
				return action, err
			}
			if !force {
				if blocked {
					return action, fmt.Errorf(
						"%s is linked by %s (use --force to switch it to %s, or link only one alternative)",
						HomeRelative(targetAbs, s.Env),
						blocker,
						selectorLabel(packageName, mapping.Source),
					)
				}
				targetText, _ := os.Readlink(targetAbs)
				return action, fmt.Errorf(
					"target %s is a symlink to another source %s (use --force to replace it)",
					targetAbs,
					targetText,
				)
			}
			action.state = linkTargetWrongSymlink
		}
	} else {
		if !force {
			return action, fmt.Errorf(
				"target %s already exists (use --force --dry-run to preview replacing it)",
				HomeRelative(targetAbs, s.Env),
			)
		}
		action.state = linkTargetNonSymlink
	}
	return action, nil
}

func (s Service) blockingPackageForTarget(
	manifest *Manifest,
	packageName string,
	targetAbs string,
) (string, bool, error) {
	targetKey := filepath.Clean(targetAbs)
	for otherPackageName, pkg := range manifest.Packages {
		if otherPackageName == packageName {
			continue
		}
		for _, link := range pkg.Links {
			otherTargetAbs, err := ExpandTargetPath(link.Target, s.Env)
			if err != nil {
				return "", false, err
			}
			if filepath.Clean(otherTargetAbs) != targetKey {
				continue
			}
			otherSourceAbs, err := PackageSourcePath(s.Repo, otherPackageName, link.Source)
			if err != nil {
				return "", false, err
			}
			if symlinkPointsTo(targetAbs, otherSourceAbs) {
				return otherPackageName, true, nil
			}
		}
	}
	return "", false, nil
}

func (s Service) executeLinkAction(tx *Tx, action *linkAction) error {
	switch action.state {
	case linkTargetAbsent:
		return CreateSymlinkTx(tx, action.sourceAbs, action.targetAbs)
	case linkTargetAlreadyCorrect:
		return nil
	case linkTargetRelativeCorrect:
		if err := RemoveSymlinkTx(tx, action.targetAbs); err != nil {
			return err
		}
		return CreateSymlinkTx(tx, action.sourceAbs, action.targetAbs)
	case linkTargetWrongSymlink, linkTargetNonSymlink:
		if err := MoveAsideTx(tx, action.targetAbs); err != nil {
			return err
		}
		return CreateSymlinkTx(tx, action.sourceAbs, action.targetAbs)
	default:
		return fmt.Errorf("unexpected link target state %d", action.state)
	}
}

func (s Service) linkResults(plan *linkPlan, dryRun bool) []LinkResult {
	results := make([]LinkResult, len(plan.actions))
	for i, a := range plan.actions {
		results[i] = LinkResult{
			Package:    a.packageName,
			Source:     a.mapping.Source,
			Target:     a.mapping.Target,
			SourcePath: HomeRelative(a.sourceAbs, s.Env),
			Action:     linkResultAction(a.state),
			Tracked:    a.tracked,
			DryRun:     dryRun,
		}
	}
	return results
}

func linkResultAction(state linkTargetState) string {
	switch state {
	case linkTargetAbsent:
		return LinkResultActionCreate
	case linkTargetAlreadyCorrect:
		return LinkResultActionNoop
	case linkTargetRelativeCorrect:
		return LinkResultActionNormalize
	case linkTargetWrongSymlink, linkTargetNonSymlink:
		return LinkResultActionReplaceConflict
	default:
		return ""
	}
}
