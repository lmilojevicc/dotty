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

func (s Service) Unlink(options UnlinkOptions) ([]UnlinkResult, error) {
	var unlinked []UnlinkResult
	if options.DryRun {
		manifest, err := LoadManifest(s.Repo)
		if err != nil {
			return nil, err
		}
		selected, err := ResolvePackageSelection(manifest, options.Packages, options.Collections, options.All)
		if err != nil {
			return nil, err
		}
		for _, packageName := range selected {
			pkg := manifest.Packages[packageName]
			for _, mapping := range pkg.Links {
				result, err := s.unlinkMapping(nil, packageName, mapping, options.Hard, true)
				if err != nil {
					return nil, err
				}
				unlinked = append(unlinked, result)
			}
		}
		return unlinked, nil
	}

	if err := RunAtomic(func(tx *Tx) error {
		manifest, err := LoadManifest(s.Repo)
		if err != nil {
			return err
		}
		selected, err := ResolvePackageSelection(manifest, options.Packages, options.Collections, options.All)
		if err != nil {
			return err
		}
		for _, packageName := range selected {
			pkg := manifest.Packages[packageName]
			for _, mapping := range pkg.Links {
				result, err := s.unlinkMapping(tx, packageName, mapping, options.Hard, false)
				if err != nil {
					return err
				}
				unlinked = append(unlinked, result)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return unlinked, nil
}

func (s Service) unlinkMapping(tx *Tx, packageName string, mapping LinkMapping, hard bool, dryRun bool) (UnlinkResult, error) {
	result := UnlinkResult{Package: packageName, Target: mapping.Target, Hard: hard, DryRun: dryRun}
	sourceAbs, err := PackageSourcePath(s.Repo, packageName, mapping.Source)
	if err != nil {
		return result, err
	}
	targetAbs, err := ExpandTargetPath(mapping.Target)
	if err != nil {
		return result, err
	}

	info, err := os.Lstat(targetAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return result, fmt.Errorf("inspect target %s: %w", targetAbs, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return result, fmt.Errorf("target %s is not an expected dotty link", targetAbs)
	}
	if !symlinkPointsTo(targetAbs, sourceAbs) {
		return result, fmt.Errorf("target %s is a symlink to another source", targetAbs)
	}

	if hard {
		if dryRun {
			return result, nil
		}
		return result, RemoveSymlinkTx(tx, targetAbs)
	}
	if exists, err := pathExists(sourceAbs); err != nil {
		return result, err
	} else if !exists {
		return result, fmt.Errorf("package %q source %q is missing", packageName, mapping.Source)
	}
	if dryRun {
		if err := validateCopyablePath(sourceAbs); err != nil {
			return result, err
		}
		return result, nil
	}
	if err := RemoveSymlinkTx(tx, targetAbs); err != nil {
		return result, err
	}
	return result, CopyPathTx(tx, sourceAbs, targetAbs)
}
