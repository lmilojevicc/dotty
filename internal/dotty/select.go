package dotty

import (
	"fmt"
	"path/filepath"
)

type SelectedLinkMapping struct {
	Package string
	Link    LinkMapping
	Added   bool
}

func ResolvePackageSelection(
	manifest *Manifest,
	packages []string,
	collections []string,
	all bool,
) ([]string, error) {
	manifest.normalize()
	if all {
		if len(packages) > 0 || len(collections) > 0 {
			return nil, fmt.Errorf("--all cannot be combined with packages or collections")
		}
		return sortedKeys(manifest.Packages), nil
	}
	if len(packages) == 0 && len(collections) == 0 {
		return nil, fmt.Errorf("select at least one package or collection (or use --all)")
	}
	seen := map[string]bool{}
	selected := []string{}
	addPackage := func(name string) error {
		if err := validateName("package", name); err != nil {
			return err
		}
		if _, ok := manifest.Packages[name]; !ok {
			return fmt.Errorf("unknown package %q (run `dotty list` to see packages)", name)
		}
		if !seen[name] {
			seen[name] = true
			selected = append(selected, name)
		}
		return nil
	}
	for _, name := range packages {
		if err := addPackage(name); err != nil {
			return nil, err
		}
	}
	for _, collectionName := range collections {
		if err := validateName("collection", collectionName); err != nil {
			return nil, err
		}
		collection, ok := manifest.Collections[collectionName]
		if !ok {
			return nil, fmt.Errorf(
				"unknown collection %q (run `dotty list` to see collections)",
				collectionName,
			)
		}
		for _, packageName := range collection.Packages {
			if err := addPackage(packageName); err != nil {
				return nil, err
			}
		}
	}
	return selected, nil
}

func ResolveSelectors(
	manifest *Manifest,
	options ResolveOptions,
	env Env,
) ([]SelectedLinkMapping, error) {
	if err := rejectInvalidTargetScope(
		len(options.Targets),
		len(options.Selectors),
		len(options.Collections),
		options.All,
	); err != nil {
		return nil, err
	}
	if options.All && (len(options.Selectors) > 0 || len(options.Collections) > 0) {
		return nil, fmt.Errorf("--all cannot be combined with selectors or collections")
	}

	selectors := append([]Selector{}, options.Selectors...)
	if options.All || len(options.Collections) > 0 {
		selectedPackages, err := ResolvePackageSelection(
			manifest,
			nil,
			options.Collections,
			options.All,
		)
		if err != nil {
			return nil, err
		}
		for _, packageName := range selectedPackages {
			selectors = append(selectors, Selector{Package: packageName})
		}
	}
	if len(selectors) == 0 {
		return nil, fmt.Errorf("select at least one selector, collection, or use --all")
	}

	requestedTargets, err := normalizeTargetSelectors(options.Targets, env)
	if err != nil {
		return nil, err
	}
	filterTargets := len(requestedTargets) > 0

	seenSelectedMappings := map[string]bool{}
	matchedTargets := map[string]bool{}
	selected := []SelectedLinkMapping{}
	for _, selector := range selectors {
		if err := validateName("package", selector.Package); err != nil {
			return nil, err
		}
		if selector.IsPackageSource() {
			if err := validateSourcePath(selector.Source); err != nil {
				return nil, err
			}
		}

		pkg, ok := manifest.Packages[selector.Package]
		if !ok {
			return nil, fmt.Errorf(
				"unknown package %q (run `dotty list` to see packages)",
				selector.Package,
			)
		}

		matchedSource := false
		for _, link := range pkg.Links {
			if selector.IsPackageSource() {
				if link.Source != selector.Source {
					continue
				}
				matchedSource = true
			}

			if filterTargets {
				targetKey, err := targetKey(link.Target, env)
				if err != nil {
					return nil, err
				}
				if _, ok := requestedTargets[targetKey]; !ok {
					continue
				}
				matchedTargets[targetKey] = true
			}

			mappingKey := selectedMappingKey(selector.Package, link)
			if seenSelectedMappings[mappingKey] {
				continue
			}
			seenSelectedMappings[mappingKey] = true
			selected = append(selected, SelectedLinkMapping{Package: selector.Package, Link: link})
		}
		if selector.IsPackageSource() && !matchedSource {
			return nil, fmt.Errorf(
				"unknown source %q in package %q",
				selector.Source,
				selector.Package,
			)
		}
	}

	if filterTargets {
		for _, target := range options.Targets {
			key, ok := requestedTargetsByOriginal(requestedTargets, target, env)
			if !ok || matchedTargets[key] {
				continue
			}
			return nil, fmt.Errorf(
				"target %q is not mapped in the selected package scope",
				target,
			)
		}
	}

	return selected, nil
}

func selectedMappingKey(packageName string, link LinkMapping) string {
	return packageName + "\x00" + link.Source + "\x00" + link.Target
}

func ResolveSelectedLinkMappings(
	manifest *Manifest,
	packages []string,
	collections []string,
	all bool,
	targets []string,
	env Env,
) ([]SelectedLinkMapping, error) {
	if err := rejectInvalidTargetScope(
		len(targets),
		len(packages),
		len(collections),
		all,
	); err != nil {
		return nil, err
	}
	selectedPackages, err := ResolvePackageSelection(manifest, packages, collections, all)
	if err != nil {
		return nil, err
	}

	requestedTargets, err := normalizeTargetSelectors(targets, env)
	if err != nil {
		return nil, err
	}
	filterTargets := len(requestedTargets) > 0

	seenSelectedTargets := map[string]bool{}
	matchedTargets := map[string]bool{}
	selected := []SelectedLinkMapping{}
	for _, packageName := range selectedPackages {
		pkg := manifest.Packages[packageName]
		for _, link := range pkg.Links {
			targetKey, err := targetKey(link.Target, env)
			if err != nil {
				return nil, err
			}
			if filterTargets {
				if _, ok := requestedTargets[targetKey]; !ok {
					continue
				}
				matchedTargets[targetKey] = true
				if seenSelectedTargets[targetKey] {
					continue
				}
			}
			seenSelectedTargets[targetKey] = true
			selected = append(selected, SelectedLinkMapping{Package: packageName, Link: link})
		}
	}

	if filterTargets {
		for _, target := range targets {
			key, ok := requestedTargetsByOriginal(requestedTargets, target, env)
			if !ok || matchedTargets[key] {
				continue
			}
			return nil, fmt.Errorf(
				"target %q is not mapped in the selected package scope",
				target,
			)
		}
	}

	return selected, nil
}

func rejectInvalidTargetScope(targetCount, selectorCount, collectionCount int, all bool) error {
	if targetCount == 0 {
		return nil
	}
	if all || collectionCount > 0 || selectorCount != 1 {
		return fmt.Errorf(
			"--target can only be used with one selector (run separate commands when narrowing by target)",
		)
	}
	return nil
}

func normalizeTargetSelectors(targets []string, env Env) (map[string]string, error) {
	requested := map[string]string{}
	for _, target := range targets {
		if err := validateTargetPath(target); err != nil {
			return nil, err
		}
		key, err := targetKey(target, env)
		if err != nil {
			return nil, err
		}
		if _, seen := requested[key]; !seen {
			requested[key] = target
		}
	}
	return requested, nil
}

func requestedTargetsByOriginal(
	requested map[string]string,
	target string,
	env Env,
) (string, bool) {
	key, err := targetKey(target, env)
	if err != nil {
		return "", false
	}
	_, ok := requested[key]
	return key, ok
}

func targetKey(target string, env Env) (string, error) {
	targetAbs, err := ExpandTargetPath(target, env)
	if err != nil {
		return "", err
	}
	return filepath.Clean(targetAbs), nil
}
