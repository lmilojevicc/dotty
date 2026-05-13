package dotty

import (
	"fmt"
	"path/filepath"
)

type MapOptions struct {
	Package string
	Source  string
	Target  string
	DryRun  bool
}

type MapResult struct {
	Package    string
	Source     string
	SourcePath string
	Target     string
	DryRun     bool
}

func (s Service) Map(options MapOptions) (*MapResult, error) {
	if options.DryRun {
		manifest, err := LoadManifest(s.Repo, s.Env)
		if err != nil {
			return nil, err
		}
		return s.planMap(manifest, options)
	}

	var result *MapResult
	if err := withRepositoryLock(s.Repo, func() error {
		return RunAtomic(func(tx *Tx) error {
			manifest, err := LoadManifest(s.Repo, s.Env)
			if err != nil {
				return err
			}
			planned, err := s.planMap(manifest, options)
			if err != nil {
				return err
			}
			if err := SaveManifest(tx, s.Repo, manifest, s.Env); err != nil {
				return err
			}
			result = planned
			return nil
		})
	}); err != nil {
		return nil, err
	}
	return result, nil
}

func (s Service) planMap(manifest *Manifest, options MapOptions) (*MapResult, error) {
	manifest.normalize()
	if err := validateName("package", options.Package); err != nil {
		return nil, err
	}
	if _, ok := manifest.Packages[options.Package]; !ok {
		return nil, fmt.Errorf(
			"unknown package %q (run `dotty list` to see packages)",
			options.Package,
		)
	}
	if err := validateSourcePath(options.Source); err != nil {
		return nil, err
	}
	if err := validateTargetPath(options.Target); err != nil {
		return nil, err
	}

	sourceAbs, err := PackageSourcePath(s.Repo, options.Package, options.Source)
	if err != nil {
		return nil, err
	}
	if exists, err := pathExists(sourceAbs); err != nil {
		return nil, err
	} else if !exists {
		return nil, fmt.Errorf(
			"package %q source %q is missing (choose an existing Package Source)",
			options.Package,
			options.Source,
		)
	}

	storedTarget, err := StoreTargetPath(options.Target, s.Env)
	if err != nil {
		return nil, err
	}
	if err := rejectMappedTarget(manifest, options.Package, storedTarget, s.Env); err != nil {
		return nil, err
	}
	link := LinkMapping{Source: options.Source, Target: storedTarget}
	if err := AddManifestLink(manifest, options.Package, link, s.Env); err != nil {
		return nil, err
	}
	if err := validateLinkMappingTopology(s.Repo, options.Package, link, s.Env); err != nil {
		return nil, err
	}

	return &MapResult{
		Package:    options.Package,
		Source:     options.Source,
		SourcePath: HomeRelative(sourceAbs, s.Env),
		Target:     storedTarget,
		DryRun:     options.DryRun,
	}, nil
}

func rejectMappedTarget(manifest *Manifest, packageName, target string, env Env) error {
	targetAbs, err := ExpandTargetPath(target, env)
	if err != nil {
		return err
	}
	targetKey := filepath.Clean(targetAbs)
	for existingPackageName, pkg := range manifest.Packages {
		for _, link := range pkg.Links {
			existingAbs, err := ExpandTargetPath(link.Target, env)
			if err != nil {
				return err
			}
			if filepath.Clean(existingAbs) == targetKey {
				if existingPackageName == packageName {
					return fmt.Errorf(
						"target %q is mapped more than once in package %s (edit dotty.toml so each Target Path appears once)",
						target,
						existingPackageName,
					)
				}
				return fmt.Errorf(
					"target %q is mapped more than once (%s and %s) (edit dotty.toml so each Target Path appears once)",
					target,
					existingPackageName,
					packageName,
				)
			}
		}
	}
	return nil
}
