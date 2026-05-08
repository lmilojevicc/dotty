package dotty

import (
	"fmt"
	"os"
)

type LinkOptions struct {
	Packages    []string
	Collections []string
	Force       bool
}

func (s Service) Link(options LinkOptions) ([]string, error) {
	var linked []string
	if err := RunAtomic(func(tx *Tx) error {
		manifest, err := LoadManifest(s.Repo)
		if err != nil {
			return err
		}
		selected, err := ResolvePackageSelection(manifest, options.Packages, options.Collections)
		if err != nil {
			return err
		}
		for _, packageName := range selected {
			pkg := manifest.Packages[packageName]
			for _, mapping := range pkg.Links {
				if err := s.linkMapping(tx, packageName, mapping, options.Force); err != nil {
					return err
				}
			}
		}
		linked = selected
		return nil
	}); err != nil {
		return nil, err
	}
	return linked, nil
}

func (s Service) linkMapping(tx *Tx, packageName string, mapping LinkMapping, force bool) error {
	sourceAbs, err := PackageSourcePath(s.Repo, packageName, mapping.Source)
	if err != nil {
		return err
	}
	if exists, err := pathExists(sourceAbs); err != nil {
		return err
	} else if !exists {
		return fmt.Errorf("package %q source %q is missing", packageName, mapping.Source)
	}
	targetAbs, err := ExpandTargetPath(mapping.Target)
	if err != nil {
		return err
	}

	info, err := os.Lstat(targetAbs)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if symlinkPointsTo(targetAbs, sourceAbs) {
				targetText, _ := os.Readlink(targetAbs)
				if targetText == sourceAbs {
					return nil
				}
				if err := RemoveSymlinkTx(tx, targetAbs); err != nil {
					return err
				}
			} else if force {
				if err := MoveAsideTx(tx, targetAbs); err != nil {
					return err
				}
			} else {
				return fmt.Errorf("target %s is a symlink to another source", targetAbs)
			}
		} else if force {
			if err := MoveAsideTx(tx, targetAbs); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("target %s already exists", targetAbs)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect target %s: %w", targetAbs, err)
	}

	return CreateSymlinkTx(tx, sourceAbs, targetAbs)
}
