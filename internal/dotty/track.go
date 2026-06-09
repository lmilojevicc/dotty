package dotty

import "fmt"

type TrackOptions struct {
	Selector Selector
	Targets  []string
	DryRun   bool
}

type TrackResult struct {
	Package string
	Source  string
	Target  string
	Added   bool
	DryRun  bool
}

func (s Service) Track(options TrackOptions) ([]TrackResult, error) {
	if options.DryRun {
		manifest, err := LoadManifest(s.Repo, s.Env)
		if err != nil {
			return nil, err
		}
		return s.planTrack(manifest, options)
	}

	var results []TrackResult
	if err := withRepositoryLock(s.Repo, func() error {
		return RunAtomic(func(tx *Tx) error {
			manifest, err := LoadManifest(s.Repo, s.Env)
			if err != nil {
				return err
			}
			planned, err := s.planTrack(manifest, options)
			if err != nil {
				return err
			}
			if err := SaveManifest(tx, s.Repo, manifest, s.Env); err != nil {
				return err
			}
			results = planned
			return nil
		})
	}); err != nil {
		return nil, err
	}
	return results, nil
}

func (s Service) planTrack(manifest *Manifest, options TrackOptions) ([]TrackResult, error) {
	manifest.normalize()
	if len(options.Targets) == 0 {
		return nil, fmt.Errorf("provide at least one target to track")
	}
	if err := validateName("package", options.Selector.Package); err != nil {
		return nil, err
	}
	source := options.Selector.Source
	if source == "" {
		source = "."
	} else if err := validateSourcePath(source); err != nil {
		return nil, err
	}

	packageRoot := PackageRoot(s.Repo, options.Selector.Package)
	if exists, err := pathExists(packageRoot); err != nil {
		return nil, err
	} else if !exists {
		return nil, fmt.Errorf("package root %s is missing", packageRoot)
	}

	sourceAbs, err := PackageSourcePath(s.Repo, options.Selector.Package, source)
	if err != nil {
		return nil, err
	}
	if exists, err := pathExists(sourceAbs); err != nil {
		return nil, err
	} else if !exists {
		return nil, fmt.Errorf(
			"%s is missing from the repository (create it first, or use `dotty add <target> %s` to adopt target-side content)",
			selectorLabel(options.Selector.Package, source),
			options.Selector.Package,
		)
	}
	if err := validateSupportedSourcePath(sourceAbs); err != nil {
		return nil, err
	}

	results := make([]TrackResult, 0, len(options.Targets))
	for _, target := range options.Targets {
		if err := validateTargetPath(target); err != nil {
			return nil, err
		}
		storedTarget, err := StoreTargetPath(target, s.Env)
		if err != nil {
			return nil, err
		}
		link := LinkMapping{Source: source, Target: storedTarget}
		added := !manifestHasLink(manifest, options.Selector.Package, link)
		if err := AddManifestLink(manifest, options.Selector.Package, link, s.Env); err != nil {
			return nil, err
		}
		results = append(results, TrackResult{
			Package: options.Selector.Package,
			Source:  source,
			Target:  storedTarget,
			Added:   added,
			DryRun:  options.DryRun,
		})
	}
	return results, nil
}

func manifestHasLink(manifest *Manifest, packageName string, link LinkMapping) bool {
	pkg := manifest.Packages[packageName]
	for _, existing := range pkg.Links {
		if existing.Source == link.Source && existing.Target == link.Target {
			return true
		}
	}
	return false
}
