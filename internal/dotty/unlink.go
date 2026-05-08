package dotty

import (
	"fmt"
	"os"
)

type UnlinkOptions struct {
	Packages    []string
	Collections []string
	Hard        bool
}

func (s Service) Unlink(options UnlinkOptions) ([]string, error) {
	var unlinked []string
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
				if err := s.unlinkMapping(tx, packageName, mapping, options.Hard); err != nil {
					return err
				}
			}
		}
		unlinked = selected
		return nil
	}); err != nil {
		return nil, err
	}
	return unlinked, nil
}

func (s Service) unlinkMapping(tx *Tx, packageName string, mapping LinkMapping, hard bool) error {
	sourceAbs, err := PackageSourcePath(s.Repo, packageName, mapping.Source)
	if err != nil {
		return err
	}
	targetAbs, err := ExpandTargetPath(mapping.Target)
	if err != nil {
		return err
	}

	info, err := os.Lstat(targetAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect target %s: %w", targetAbs, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("target %s is not an expected dotty link", targetAbs)
	}
	if !symlinkPointsTo(targetAbs, sourceAbs) {
		return fmt.Errorf("target %s is a symlink to another source", targetAbs)
	}

	if hard {
		return RemoveSymlinkTx(tx, targetAbs)
	}
	if exists, err := pathExists(sourceAbs); err != nil {
		return err
	} else if !exists {
		return fmt.Errorf("package %q source %q is missing", packageName, mapping.Source)
	}
	if err := RemoveSymlinkTx(tx, targetAbs); err != nil {
		return err
	}
	return CopyPathTx(tx, sourceAbs, targetAbs)
}
