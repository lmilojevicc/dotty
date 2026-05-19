package dotty

import (
	"fmt"
	"os"
	"path/filepath"
)

type UnlinkOptions struct {
	Packages    []string
	Selectors   []Selector
	Collections []string
	Targets     []string
	All         bool
	LeaveCopy   bool
	Untrack     bool
	DryRun      bool
}

type UnlinkResult struct {
	Package   string
	Source    string
	Target    string
	Action    string
	LeaveCopy bool
	Untracked bool
	DryRun    bool
}

type unlinkAction struct {
	packageName   string
	mapping       LinkMapping
	sourceAbs     string
	copySourceAbs string
	targetAbs     string
	leaveCopy     bool
	untrack       bool
	state         unlinkTargetState
}

type unlinkTargetState int

const (
	unlinkTargetAbsent  unlinkTargetState = iota // nothing at target, no-op unless leaving a copy
	unlinkTargetSkipped                          // unexpected target intentionally left untouched
	unlinkTargetCorrect                          // target is expected dotty link
)

func (s Service) Unlink(options UnlinkOptions) ([]UnlinkResult, error) {
	var plan *unlinkPlan
	if options.DryRun {
		var err error
		plan, err = s.planUnlink(options)
		if err != nil {
			return nil, err
		}
		return s.unlinkResults(plan, true), nil
	}
	if err := withRepositoryLock(s.Repo, func() error {
		return RunAtomic(func(tx *Tx) error {
			manifest, err := LoadManifest(s.Repo, s.Env)
			if err != nil {
				return err
			}
			plan, err = s.planUnlinkWithManifest(manifest, options)
			if err != nil {
				return err
			}
			if options.Untrack {
				if err := SaveManifest(tx, s.Repo, manifest, s.Env); err != nil {
					return err
				}
			}
			for i := range plan.actions {
				if err := s.executeUnlinkAction(tx, &plan.actions[i]); err != nil {
					return err
				}
			}
			return nil
		})
	}); err != nil {
		return nil, err
	}
	return s.unlinkResults(plan, false), nil
}

type unlinkPlan struct {
	actions []unlinkAction
}

func (s Service) unlinkResults(plan *unlinkPlan, dryRun bool) []UnlinkResult {
	results := make([]UnlinkResult, len(plan.actions))
	for i, a := range plan.actions {
		results[i] = UnlinkResult{
			Package:   a.packageName,
			Source:    a.mapping.Source,
			Target:    a.mapping.Target,
			Action:    unlinkResultAction(a.state, a.leaveCopy),
			LeaveCopy: a.leaveCopy,
			Untracked: a.untrack,
			DryRun:    dryRun,
		}
	}
	return results
}

func unlinkResultAction(state unlinkTargetState, leaveCopy bool) string {
	if state == unlinkTargetSkipped {
		return UnlinkResultActionNoop
	}
	if state == unlinkTargetAbsent {
		if leaveCopy {
			return UnlinkResultActionCopySource
		}
		return UnlinkResultActionNoop
	}
	if leaveCopy {
		return UnlinkResultActionCopySource
	}
	return UnlinkResultActionRemoveLink
}

func (s Service) planUnlink(options UnlinkOptions) (*unlinkPlan, error) {
	manifest, err := LoadManifest(s.Repo, s.Env)
	if err != nil {
		return nil, err
	}
	return s.planUnlinkWithManifest(manifest, options)
}

func (s Service) planUnlinkWithManifest(
	manifest *Manifest,
	options UnlinkOptions,
) (*unlinkPlan, error) {
	selected, err := s.resolveUnlinkSelections(manifest, options)
	if err != nil {
		return nil, err
	}

	plan := &unlinkPlan{}
	for _, mapping := range selected {
		action, err := s.classifyUnlinkAction(
			mapping.Package,
			mapping.Link,
			options.LeaveCopy,
			options.Untrack,
		)
		if err != nil {
			return nil, err
		}
		plan.actions = append(plan.actions, action)
	}
	return plan, nil
}

func (s Service) resolveUnlinkSelections(
	manifest *Manifest,
	options UnlinkOptions,
) ([]SelectedLinkMapping, error) {
	if options.Untrack {
		return s.resolveUntrackedUnlinkSelections(manifest, options)
	}
	if len(options.Selectors) > 0 {
		return ResolveSelectors(manifest, ResolveOptions{
			Selectors: options.Selectors,
			Targets:   options.Targets,
		}, s.Env)
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

func (s Service) resolveUntrackedUnlinkSelections(
	manifest *Manifest,
	options UnlinkOptions,
) ([]SelectedLinkMapping, error) {
	if options.All {
		return nil, fmt.Errorf("--untrack cannot be combined with --all")
	}
	if len(options.Collections) > 0 {
		return nil, fmt.Errorf("--untrack cannot be combined with --collection")
	}
	selectors := append([]Selector{}, options.Selectors...)
	for _, packageName := range options.Packages {
		selectors = append(selectors, Selector{Package: packageName})
	}
	if len(selectors) != 1 {
		return nil, fmt.Errorf("--untrack accepts exactly one selector")
	}
	untracked, err := s.planUntrack(manifest, UntrackOptions{
		Selector: selectors[0],
		Targets:  options.Targets,
		DryRun:   options.DryRun,
	})
	if err != nil {
		return nil, err
	}
	selected := make([]SelectedLinkMapping, 0, len(untracked))
	for _, item := range untracked {
		selected = append(selected, SelectedLinkMapping{
			Package: item.Package,
			Link: LinkMapping{
				Source: item.Source,
				Target: item.Target,
			},
		})
	}
	return selected, nil
}

func (s Service) classifyUnlinkAction(
	packageName string,
	mapping LinkMapping,
	leaveCopy bool,
	allowUnexpectedTarget bool,
) (unlinkAction, error) {
	action := unlinkAction{
		packageName: packageName,
		mapping:     mapping,
		leaveCopy:   leaveCopy,
		untrack:     allowUnexpectedTarget,
	}

	sourceAbs, err := packageSourcePathLexical(s.Repo, packageName, mapping.Source)
	if leaveCopy {
		sourceAbs, err = PackageSourcePath(s.Repo, packageName, mapping.Source)
	}
	if err != nil {
		return action, err
	}
	action.sourceAbs = sourceAbs

	targetAbs, err := ExpandTargetPath(mapping.Target, s.Env)
	if err != nil {
		return action, err
	}
	action.targetAbs = targetAbs
	if leaveCopy {
		if err := validateLinkMappingTopology(s.Repo, packageName, mapping, s.Env); err != nil {
			return action, err
		}
	} else if err := validateTargetTopology(targetAbs, s.Repo, s.Env); err != nil {
		return action, err
	}

	if err := validateTargetParentsAreLexicalDirectories(targetAbs, s.Env); err != nil {
		return action, err
	}

	// For --leave-copy unlink, validate that the source copy can be materialized during planning.
	if leaveCopy {
		if exists, err := pathExists(sourceAbs); err != nil {
			return action, err
		} else if !exists {
			return action, fmt.Errorf(
				"%s is missing from the repository (restore it, or omit --leave-copy to remove only the link)",
				selectorLabel(packageName, mapping.Source),
			)
		}
		copySourceAbs, err := unlinkCopySourcePath(sourceAbs)
		if err != nil {
			return action, err
		}
		action.copySourceAbs = copySourceAbs
		if exists, err := pathExists(copySourceAbs); err != nil {
			return action, err
		} else if !exists {
			return action, fmt.Errorf(
				"%s is missing from the repository (restore it, or omit --leave-copy to remove only the link)",
				selectorLabel(packageName, mapping.Source),
			)
		}
		if err := validateSupportedSourcePath(copySourceAbs); err != nil {
			return action, err
		}
		if externalHardlinks, err := hasPreservedSymlinkReferentHardlinksOutsideRoot(
			copySourceAbs,
			copySourceAbs,
			s.Repo,
		); err != nil {
			return action, err
		} else if externalHardlinks {
			return action, fmt.Errorf(
				"%s has symlink referents with external hardlink aliases (copy them into the repository before unlinking)",
				selectorLabel(packageName, mapping.Source),
			)
		}
		if err := validateCopyablePath(copySourceAbs); err != nil {
			return action, err
		}
	}

	info, err := os.Lstat(targetAbs)
	if err != nil {
		if os.IsNotExist(err) {
			action.state = unlinkTargetAbsent
			return action, nil
		}
		return action, fmt.Errorf("inspect target %s: %w", targetAbs, err)
	}

	if info.Mode()&os.ModeSymlink == 0 {
		if allowUnexpectedTarget {
			action.state = unlinkTargetSkipped
			return action, nil
		}
		return action, fmt.Errorf(
			"target %s is not an expected dotty link (inspect with `dotty status` or remove it manually)",
			targetAbs,
		)
	}
	if !symlinkPointsTo(targetAbs, sourceAbs) {
		if allowUnexpectedTarget {
			action.state = unlinkTargetSkipped
			return action, nil
		}
		targetText, _ := os.Readlink(targetAbs)
		return action, fmt.Errorf(
			"target %s is a symlink to another source %s (restore the expected Link or remove it manually)",
			targetAbs,
			targetText,
		)
	}
	action.state = unlinkTargetCorrect

	return action, nil
}

func unlinkCopySourcePath(sourceAbs string) (string, error) {
	info, err := os.Lstat(sourceAbs)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return sourceAbs, nil
	}
	resolved, err := filepath.EvalSymlinks(sourceAbs)
	if err != nil {
		return "", fmt.Errorf("resolve source symlink %s: %w", sourceAbs, err)
	}
	return filepath.Clean(resolved), nil
}

func (s Service) executeUnlinkAction(tx *Tx, action *unlinkAction) error {
	if action.state == unlinkTargetSkipped {
		return nil
	}
	if action.state == unlinkTargetAbsent {
		if action.leaveCopy {
			return CopyPathTx(tx, action.copySourceAbs, action.targetAbs)
		}
		return nil
	}

	if !action.leaveCopy {
		return RemoveSymlinkTx(tx, action.targetAbs)
	}

	if err := RemoveSymlinkTx(tx, action.targetAbs); err != nil {
		return err
	}
	return CopyPathTx(tx, action.copySourceAbs, action.targetAbs)
}
