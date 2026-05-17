package dotty

import "fmt"

type UnmapOptions struct {
	Package string
	Targets []string
	DryRun  bool
}

type UnmapResult struct {
	Package string
	Source  string
	Target  string
	DryRun  bool
}

func (s Service) Unmap(options UnmapOptions) ([]UnmapResult, error) {
	if options.DryRun {
		manifest, err := LoadManifest(s.Repo, s.Env)
		if err != nil {
			return nil, err
		}
		results, err := s.planUnmap(manifest, options)
		if err != nil {
			return nil, err
		}
		return results, nil
	}

	var results []UnmapResult
	if err := withRepositoryLock(s.Repo, func() error {
		return RunAtomic(func(tx *Tx) error {
			manifest, err := LoadManifest(s.Repo, s.Env)
			if err != nil {
				return err
			}
			planned, err := s.planUnmap(manifest, options)
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

func (s Service) planUnmap(manifest *Manifest, options UnmapOptions) ([]UnmapResult, error) {
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
	if len(options.Targets) == 0 {
		return nil, fmt.Errorf("select at least one target with --target")
	}

	selected, err := ResolveSelectedLinkMappings(
		manifest,
		[]string{options.Package},
		nil,
		false,
		options.Targets,
		s.Env,
	)
	if err != nil {
		return nil, err
	}

	remove := map[string]bool{}
	results := make([]UnmapResult, 0, len(selected))
	for _, item := range selected {
		key, err := targetKey(item.Link.Target, s.Env)
		if err != nil {
			return nil, err
		}
		remove[key] = true
		results = append(results, UnmapResult{
			Package: item.Package,
			Source:  item.Link.Source,
			Target:  item.Link.Target,
			DryRun:  options.DryRun,
		})
	}

	pkg := manifest.Packages[options.Package]
	kept := make([]LinkMapping, 0, len(pkg.Links)-len(remove))
	for _, link := range pkg.Links {
		key, err := targetKey(link.Target, s.Env)
		if err != nil {
			return nil, err
		}
		if remove[key] {
			continue
		}
		kept = append(kept, link)
	}
	pkg.Links = kept
	manifest.Packages[options.Package] = pkg

	return results, nil
}
