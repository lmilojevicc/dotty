package dotty

import (
	"fmt"
	"os"
	"path/filepath"
)

type AddResult struct {
	Package string
	Source  string
	Target  string
}

func (s Service) Add(targetInput, packageName string) (*AddResult, error) {
	if err := validateName("package", packageName); err != nil {
		return nil, err
	}
	targetAbs, err := ExpandTargetPath(targetInput)
	if err != nil {
		return nil, err
	}
	storedTarget := HomeRelative(targetAbs)

	var result AddResult
	if err := RunAtomic(func(tx *Tx) error {
		manifest, err := LoadManifest(s.Repo)
		if err != nil {
			return err
		}

		targetInfo, err := os.Lstat(targetAbs)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("target %s does not exist", targetAbs)
			}
			return fmt.Errorf("inspect target %s: %w", targetAbs, err)
		}

		adoptPath := targetAbs
		symlinkAdoption := targetInfo.Mode()&os.ModeSymlink != 0
		if symlinkAdoption {
			resolved, err := filepath.EvalSymlinks(targetAbs)
			if err != nil {
				return fmt.Errorf("resolve symlink %s: %w", targetAbs, err)
			}
			adoptPath = filepath.Clean(resolved)
		}

		adoptInfo, err := os.Stat(adoptPath)
		if err != nil {
			return fmt.Errorf("inspect adopted content %s: %w", adoptPath, err)
		}

		_, packageExists := manifest.Packages[packageName]
		packageRoot := PackageRoot(s.Repo, packageName)
		sourceRel := filepath.Base(targetAbs)
		dest := filepath.Join(packageRoot, sourceRel)
		if !packageExists && adoptInfo.IsDir() {
			sourceRel = "."
			dest = packageRoot
		}
		dest = filepath.Clean(dest)

		if exists, err := pathExists(dest); err != nil {
			return err
		} else if exists {
			if !sameExistingPath(dest, adoptPath) {
				return fmt.Errorf("repo-side package source %s already exists", dest)
			}
		} else {
			if symlinkAdoption {
				if isWithin(s.Repo, adoptPath) {
					return fmt.Errorf("symlink target %s is inside the dotfiles repository but is not the intended package source %s", adoptPath, dest)
				}
				if err := CopyPathTx(tx, adoptPath, dest); err != nil {
					return err
				}
			} else {
				if err := MovePathTx(tx, targetAbs, dest); err != nil {
					return err
				}
			}
		}

		if exists, err := pathExists(targetAbs); err != nil {
			return err
		} else if exists {
			info, err := os.Lstat(targetAbs)
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink == 0 {
				return fmt.Errorf("target %s still exists and is not a symlink", targetAbs)
			}
			if err := RemoveSymlinkTx(tx, targetAbs); err != nil {
				return err
			}
		}

		if err := CreateSymlinkTx(tx, dest, targetAbs); err != nil {
			return err
		}

		link := LinkMapping{Source: filepath.ToSlash(sourceRel), Target: storedTarget}
		if err := AddManifestLink(manifest, packageName, link); err != nil {
			return err
		}
		if err := SaveManifest(tx, s.Repo, manifest); err != nil {
			return err
		}

		result = AddResult{Package: packageName, Source: filepath.ToSlash(sourceRel), Target: storedTarget}
		return nil
	}); err != nil {
		return nil, err
	}
	return &result, nil
}
