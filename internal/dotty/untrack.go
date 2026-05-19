package dotty

import "os"

type UntrackOptions struct {
	Selector Selector
	Targets  []string
	DryRun   bool
}

type UntrackResult struct {
	Package    string
	Source     string
	Target     string
	LinkExists bool
	DryRun     bool
}

func (s Service) Untrack(options UntrackOptions) ([]UntrackResult, error) {
	if options.DryRun {
		manifest, err := LoadManifest(s.Repo, s.Env)
		if err != nil {
			return nil, err
		}
		return s.planUntrack(manifest, options)
	}

	var results []UntrackResult
	if err := withRepositoryLock(s.Repo, func() error {
		return RunAtomic(func(tx *Tx) error {
			manifest, err := LoadManifest(s.Repo, s.Env)
			if err != nil {
				return err
			}
			planned, err := s.planUntrack(manifest, options)
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

func (s Service) planUntrack(manifest *Manifest, options UntrackOptions) ([]UntrackResult, error) {
	selected, err := ResolveSelectors(manifest, ResolveOptions{
		Selectors: []Selector{options.Selector},
		Targets:   options.Targets,
	}, s.Env)
	if err != nil {
		return nil, err
	}

	remove := map[string]bool{}
	results := make([]UntrackResult, 0, len(selected))
	for _, item := range selected {
		remove[selectedMappingKey(item.Package, item.Link)] = true
		linkExists, err := s.untrackLinkExists(item.Package, item.Link)
		if err != nil {
			return nil, err
		}
		results = append(results, UntrackResult{
			Package:    item.Package,
			Source:     item.Link.Source,
			Target:     item.Link.Target,
			LinkExists: linkExists,
			DryRun:     options.DryRun,
		})
	}

	for packageName, pkg := range manifest.Packages {
		kept := make([]LinkMapping, 0, len(pkg.Links))
		for _, link := range pkg.Links {
			if remove[selectedMappingKey(packageName, link)] {
				continue
			}
			kept = append(kept, link)
		}
		pkg.Links = kept
		manifest.Packages[packageName] = pkg
	}
	return results, nil
}

func (s Service) untrackLinkExists(packageName string, link LinkMapping) (bool, error) {
	sourceAbs, err := PackageSourcePath(s.Repo, packageName, link.Source)
	if err != nil {
		return false, err
	}
	targetAbs, err := ExpandTargetPath(link.Target, s.Env)
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(targetAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.Mode()&os.ModeSymlink != 0 && symlinkPointsTo(targetAbs, sourceAbs), nil
}
