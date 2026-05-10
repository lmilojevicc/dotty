package dotty

import (
	"fmt"
	"os"
	"path/filepath"
)

type AddResult struct {
	Package    string
	Source     string
	SourcePath string
	Target     string
	DryRun     bool
}

type AddOptions struct {
	Target  string
	Package string
	DryRun  bool
}

type addPlan struct {
	result          AddResult
	targetAbs       string
	adoptPath       string
	dest            string
	symlinkAdoption bool
	destExists      bool
}

func (s Service) Add(targetInput, packageName string) (*AddResult, error) {
	return s.AddWithOptions(AddOptions{Target: targetInput, Package: packageName})
}

func (s Service) AddWithOptions(options AddOptions) (*AddResult, error) {
	if options.DryRun {
		manifest, err := LoadManifest(s.Repo, s.Env)
		if err != nil {
			return nil, err
		}
		plan, err := s.planAdd(options.Target, options.Package, manifest, true)
		if err != nil {
			return nil, err
		}
		return &plan.result, nil
	}

	var result AddResult
	if err := withRepositoryLock(s.Repo, func() error {
		return RunAtomic(func(tx *Tx) error {
			manifest, err := LoadManifest(s.Repo, s.Env)
			if err != nil {
				return err
			}
			plan, err := s.planAdd(options.Target, options.Package, manifest, false)
			if err != nil {
				return err
			}

			if !plan.destExists {
				if plan.symlinkAdoption {
					if err := CopyPathTx(tx, plan.adoptPath, plan.dest); err != nil {
						return err
					}
				} else {
					if err := MovePathTx(tx, plan.targetAbs, plan.dest); err != nil {
						return err
					}
				}
			}

			if exists, err := pathExists(plan.targetAbs); err != nil {
				return err
			} else if exists {
				info, err := os.Lstat(plan.targetAbs)
				if err != nil {
					return err
				}
				if info.Mode()&os.ModeSymlink == 0 {
					return fmt.Errorf("target %s still exists and is not a symlink", plan.targetAbs)
				}
				if err := RemoveSymlinkTx(tx, plan.targetAbs); err != nil {
					return err
				}
			}

			if err := CreateSymlinkTx(tx, plan.dest, plan.targetAbs); err != nil {
				return err
			}
			if err := SaveManifest(tx, s.Repo, manifest, s.Env); err != nil {
				return err
			}

			result = plan.result
			return nil
		})
	}); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s Service) planAdd(
	targetInput, packageName string,
	manifest *Manifest,
	dryRun bool,
) (*addPlan, error) {
	if err := validateName("package", packageName); err != nil {
		return nil, err
	}
	targetAbs, err := ExpandTargetPath(targetInput, s.Env)
	if err != nil {
		return nil, err
	}
	storedTarget := HomeRelative(targetAbs, s.Env)

	targetInfo, err := os.Lstat(targetAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("target %s does not exist", targetAbs)
		}
		return nil, fmt.Errorf("inspect target %s: %w", targetAbs, err)
	}

	adoptPath := targetAbs
	symlinkAdoption := targetInfo.Mode()&os.ModeSymlink != 0
	if symlinkAdoption {
		resolved, err := filepath.EvalSymlinks(targetAbs)
		if err != nil {
			return nil, fmt.Errorf("resolve symlink %s: %w", targetAbs, err)
		}
		adoptPath = filepath.Clean(resolved)
	}

	adoptInfo, err := os.Stat(adoptPath)
	if err != nil {
		return nil, fmt.Errorf("inspect adopted content %s: %w", adoptPath, err)
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

	destExists, err := pathExists(dest)
	if err != nil {
		return nil, err
	}
	if destExists {
		if !sameExistingPath(dest, adoptPath) {
			return nil, fmt.Errorf("repo-side package source %s already exists", dest)
		}
		if !symlinkAdoption {
			return nil, fmt.Errorf("target %s still exists and is not a symlink", targetAbs)
		}
	} else if symlinkAdoption {
		repoResolved, err := filepath.EvalSymlinks(s.Repo)
		if err != nil {
			return nil, fmt.Errorf("resolve dotfiles repository %s: %w", s.Repo, err)
		}
		if isWithin(repoResolved, adoptPath) {
			return nil, fmt.Errorf(
				"symlink target %s is inside the dotfiles repository but is not the intended package source %s",
				adoptPath,
				dest,
			)
		}
		if dryRun {
			if err := validateCopyablePath(adoptPath); err != nil {
				return nil, err
			}
		}
	}

	link := LinkMapping{Source: filepath.ToSlash(sourceRel), Target: storedTarget}
	if err := AddManifestLink(manifest, packageName, link, s.Env); err != nil {
		return nil, err
	}

	return &addPlan{
		result: AddResult{
			Package:    packageName,
			Source:     filepath.ToSlash(sourceRel),
			SourcePath: HomeRelative(dest, s.Env),
			Target:     storedTarget,
			DryRun:     dryRun,
		},
		targetAbs:       targetAbs,
		adoptPath:       adoptPath,
		dest:            dest,
		symlinkAdoption: symlinkAdoption,
		destExists:      destExists,
	}, nil
}
