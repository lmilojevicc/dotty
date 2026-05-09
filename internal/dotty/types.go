package dotty

const (
	ManifestFileName = "dotty.toml"
	ManifestVersion  = 1
)

type Manifest struct {
	Version     int                   `toml:"version"`
	Packages    map[string]Package    `toml:"packages"`
	Collections map[string]Collection `toml:"collections"`
}

type Package struct {
	Links []LinkMapping `toml:"links"`
}

type LinkMapping struct {
	Source string `toml:"source"`
	Target string `toml:"target"`
}

type Collection struct {
	Packages []string `toml:"packages"`
}

type Config struct {
	Repo string `toml:"repo"`
}

type Service struct {
	Repo string
	Env  Env
}

func NewService(repo string, env Env) Service {
	return Service{Repo: repo, Env: env}
}

func NewManifest() *Manifest {
	return &Manifest{
		Version:     ManifestVersion,
		Packages:    map[string]Package{},
		Collections: map[string]Collection{},
	}
}

func (m *Manifest) normalize() {
	if m.Packages == nil {
		m.Packages = map[string]Package{}
	}
	if m.Collections == nil {
		m.Collections = map[string]Collection{}
	}
}
