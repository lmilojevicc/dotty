package dotty

type Inventory struct {
	Packages    []InventoryPackage
	Collections []InventoryCollection
}

type InventoryPackage struct {
	Name      string
	LinkCount int
}

type InventoryCollection struct {
	Name     string
	Packages []string
}

func (s Service) List() (*Inventory, error) {
	manifest, err := LoadManifest(s.Repo, s.Env)
	if err != nil {
		return nil, err
	}
	inv := &Inventory{}
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
