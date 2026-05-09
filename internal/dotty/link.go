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
	DryRun     bool
}

func (s Service) Link(options LinkOptions) ([]LinkResult, error) {
	var linked []LinkResult
	if options.DryRun {
		manifest, err := LoadManifest(s.Repo)
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
		for _, packageName := range selected {
			pkg := manifest.Packages[packageName]
			for _, mapping := range pkg.Links {
				result, err := s.linkMapping(nil, packageName, mapping, options.Force, true)
				if err != nil {
					return nil, err
				}
				linked = append(linked, result)
			}
		}
		return linked, nil
	}

	if err := RunAtomic(func(tx *Tx) error {
		manifest, err := LoadManifest(s.Repo)
		if err != nil {
			return err
		}
		selected, err := ResolvePackageSelection(
			manifest,
			options.Packages,
			options.Collections,
			options.All,
		)
		if err != nil {
			return err
		}
		for _, packageName := range selected {
			pkg := manifest.Packages[packageName]
			for _, mapping := range pkg.Links {
				result, err := s.linkMapping(tx, packageName, mapping, options.Force, false)
				if err != nil {
					return err
				}
				linked = append(linked, result)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return linked, nil
}

func (s Service) linkMapping(
	tx *Tx,
	packageName string,
	mapping LinkMapping,
	force bool,
	dryRun bool,
) (LinkResult, error) {
	result := LinkResult{Package: packageName, Target: mapping.Target, DryRun: dryRun}
	sourceAbs, err := PackageSourcePath(s.Repo, packageName, mapping.Source)
	if err != nil {
		return result, err
	}
	result.SourcePath = HomeRelative(sourceAbs)
	if exists, err := pathExists(sourceAbs); err != nil {
		return result, err
	} else if !exists {
		return result, fmt.Errorf("package %q source %q is missing", packageName, mapping.Source)
	}
	targetAbs, err := ExpandTargetPath(mapping.Target)
	if err != nil {
		return result, err
	}

	info, err := os.Lstat(targetAbs)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if symlinkPointsTo(targetAbs, sourceAbs) {
				targetText, _ := os.Readlink(targetAbs)
				if targetText == sourceAbs || dryRun {
					return result, nil
				}
				if err := RemoveSymlinkTx(tx, targetAbs); err != nil {
					return result, err
				}
			} else if force {
				if dryRun {
					return result, nil
				}
				if err := MoveAsideTx(tx, targetAbs); err != nil {
					return result, err
				}
			} else {
				return result, fmt.Errorf("target %s is a symlink to another source", targetAbs)
			}
		} else if force {
			if dryRun {
				return result, nil
			}
			if err := MoveAsideTx(tx, targetAbs); err != nil {
				return result, err
			}
		} else {
			return result, fmt.Errorf("target %s already exists", targetAbs)
		}
	} else if !os.IsNotExist(err) {
		return result, fmt.Errorf("inspect target %s: %w", targetAbs, err)
	}

	if dryRun {
		return result, nil
	}
	return result, CreateSymlinkTx(tx, sourceAbs, targetAbs)
}
