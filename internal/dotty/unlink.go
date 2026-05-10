package dotty

import (
	"fmt"
	"os"
)

type UnlinkOptions struct {
	Packages    []string
	Collections []string
	All         bool
	Hard        bool
	DryRun      bool
}

type UnlinkResult struct {
	Package string
	Target  string
	Hard    bool
	DryRun  bool
}

type unlinkAction struct {
	packageName string
	mapping     LinkMapping
	sourceAbs   string
	targetAbs   string
	hard        bool
	state       unlinkTargetState
}

type unlinkTargetState int

const (
	unlinkTargetAbsent  unlinkTargetState = iota // nothing at target, no-op
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
		var err error
		plan, err = s.planUnlink(options)
		if err != nil {
			return err
		}
		return RunAtomic(func(tx *Tx) error {
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
			Package: a.packageName,
			Target:  a.mapping.Target,
			Hard:    a.hard,
			DryRun:  dryRun,
		}
	}
	return results
}

func (s Service) planUnlink(options UnlinkOptions) (*unlinkPlan, error) {
	manifest, err := LoadManifest(s.Repo, s.Env)
	if err != nil {
		return nil, err
	}
	selected, err := ResolvePackageSelection(
		manifest,
		options.Packages,
		options.Collections,
		options.All,
	)
	if err != nil {
		return nil, err
	}

	plan := &unlinkPlan{}
	for _, packageName := range selected {
		pkg := manifest.Packages[packageName]
		for _, mapping := range pkg.Links {
			action, err := s.classifyUnlinkAction(packageName, mapping, options.Hard)
			if err != nil {
				return nil, err
			}
			plan.actions = append(plan.actions, action)
		}
	}
	return plan, nil
}

func (s Service) classifyUnlinkAction(
	packageName string,
	mapping LinkMapping,
	hard bool,
) (unlinkAction, error) {
	action := unlinkAction{packageName: packageName, mapping: mapping, hard: hard}

	sourceAbs, err := PackageSourcePath(s.Repo, packageName, mapping.Source)
	if err != nil {
		return action, err
	}
	action.sourceAbs = sourceAbs

	targetAbs, err := ExpandTargetPath(mapping.Target, s.Env)
	if err != nil {
		return action, err
	}
	action.targetAbs = targetAbs

	info, err := os.Lstat(targetAbs)
	if err != nil {
		if os.IsNotExist(err) {
			action.state = unlinkTargetAbsent
			return action, nil
		}
		return action, fmt.Errorf("inspect target %s: %w", targetAbs, err)
	}

	if info.Mode()&os.ModeSymlink == 0 {
		return action, fmt.Errorf("target %s is not an expected dotty link", targetAbs)
	}
	if !symlinkPointsTo(targetAbs, sourceAbs) {
		return action, fmt.Errorf("target %s is a symlink to another source", targetAbs)
	}
	action.state = unlinkTargetCorrect

	// For soft unlink, validate that source exists and is copyable during planning
	if !hard {
		if exists, err := pathExists(sourceAbs); err != nil {
			return action, err
		} else if !exists {
			return action, fmt.Errorf(
				"package %q source %q is missing",
				packageName,
				mapping.Source,
			)
		}
		if err := validateCopyablePath(sourceAbs); err != nil {
			return action, err
		}
	}

	return action, nil
}

func (s Service) executeUnlinkAction(tx *Tx, action *unlinkAction) error {
	if action.state == unlinkTargetAbsent {
		return nil
	}

	if action.hard {
		return RemoveSymlinkTx(tx, action.targetAbs)
	}

	if err := RemoveSymlinkTx(tx, action.targetAbs); err != nil {
		return err
	}
	return CopyPathTx(tx, action.sourceAbs, action.targetAbs)
}
