package dotty

import "fmt"

type Inventory struct {
	Packages    []InventoryPackage
	Collections []InventoryCollection
	Detail      *InventoryPackage
}

type InventoryPackage struct {
	Name      string
	LinkCount int
	Links     []LinkMapping
}

type InventoryCollection struct {
	Name     string
	Packages []string
}

func (s Service) List(packageFilter ...string) (*Inventory, error) {
	manifest, err := LoadManifest(s.Repo, s.Env)
	if err != nil {
		return nil, err
	}
	if len(packageFilter) > 1 {
		return nil, fmt.Errorf("list accepts at most one package")
	}
	inv := &Inventory{}
	if len(packageFilter) == 1 {
		selector, err := ParseSelector(packageFilter[0])
		if err != nil {
			return nil, err
		}
		if selector.IsPackageSource() {
			return nil, fmt.Errorf("list accepts packages only, not package/source selectors")
		}
		pkg, ok := manifest.Packages[selector.Package]
		if !ok {
			return nil, fmt.Errorf(
				"unknown package %q (run `dotty list` to see packages)",
				selector.Package,
			)
		}
		inv.Detail = &InventoryPackage{
			Name:      selector.Package,
			LinkCount: len(pkg.Links),
			Links:     append([]LinkMapping(nil), pkg.Links...),
		}
		return inv, nil
	}
	for _, name := range sortedKeys(manifest.Packages) {
		inv.Packages = append(
			inv.Packages,
			InventoryPackage{Name: name, LinkCount: len(manifest.Packages[name].Links)},
		)
	}
	for _, name := range sortedKeys(manifest.Collections) {
		collection := manifest.Collections[name]
		inv.Collections = append(
			inv.Collections,
			InventoryCollection{Name: name, Packages: collection.Packages},
		)
	}
	return inv, nil
}
