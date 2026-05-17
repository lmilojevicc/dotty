package dotty

import (
	"fmt"
	"path/filepath"
)

type SelectedLinkMapping struct {
	Package string
	Link    LinkMapping
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

func ResolveSelectedLinkMappings(
	manifest *Manifest,
	packages []string,
	collections []string,
	all bool,
	targets []string,
	env Env,
) ([]SelectedLinkMapping, error) {
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
