package dotty

import (
	"fmt"
	"os"
)

type LinkOptions struct {
	Packages    []string
	Collections []string
	All         bool
	Force       bool
	DryRun      bool
}

type LinkResult struct {
	Package    string
	Target     string
	SourcePath string
	Action     string
	DryRun     bool
}

type linkAction struct {
	packageName string
	mapping     LinkMapping
	sourceAbs   string
	targetAbs   string
	state       linkTargetState
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
		var err error
		plan, err = s.planLink(options)
		if err != nil {
			return err
		}
		return RunAtomic(func(tx *Tx) error {
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
	selected, err := ResolvePackageSelection(
		manifest,
		options.Packages,
		options.Collections,
		options.All,
	)
	if err != nil {
		return nil, err
	}

	plan := &linkPlan{}
	for _, packageName := range selected {
		pkg := manifest.Packages[packageName]
		for _, mapping := range pkg.Links {
			action, err := s.classifyLinkAction(packageName, mapping, options.Force)
			if err != nil {
				return nil, err
			}
			plan.actions = append(plan.actions, action)
		}
	}
	return plan, nil
}

func (s Service) classifyLinkAction(
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

	if exists, err := pathExists(sourceAbs); err != nil {
		return action, err
	} else if !exists {
		return action, fmt.Errorf(
			"package %q source %q is missing (restore the Package Source or remove the Link Mapping from dotty.toml)",
			packageName,
			mapping.Source,
		)
	}

	targetAbs, err := ExpandTargetPath(mapping.Target, s.Env)
	if err != nil {
		return action, err
	}
	action.targetAbs = targetAbs

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
			if !force {
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
				"target %s already exists (use --force to move it aside and create the Link)",
				targetAbs,
			)
		}
		action.state = linkTargetNonSymlink
	}
	return action, nil
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
			Target:     a.mapping.Target,
			SourcePath: HomeRelative(a.sourceAbs, s.Env),
			Action:     linkResultAction(a.state),
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
