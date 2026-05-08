package dotty

import "fmt"

func ResolvePackageSelection(manifest *Manifest, packages []string, collections []string, all bool) ([]string, error) {
	manifest.normalize()
	if all {
		if len(packages) > 0 || len(collections) > 0 {
			return nil, fmt.Errorf("--all cannot be combined with packages or collections")
		}
		return sortedKeys(manifest.Packages), nil
	}
	if len(packages) == 0 && len(collections) == 0 {
		return nil, fmt.Errorf("select at least one package or collection")
	}
	seen := map[string]bool{}
	selected := []string{}
	addPackage := func(name string) error {
		if err := validateName("package", name); err != nil {
			return err
		}
		if _, ok := manifest.Packages[name]; !ok {
			return fmt.Errorf("unknown package %q", name)
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
			return nil, fmt.Errorf("unknown collection %q", collectionName)
		}
		for _, packageName := range collection.Packages {
			if err := addPackage(packageName); err != nil {
				return nil, err
			}
		}
	}
	return selected, nil
}
