package dotty

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func LoadManifest(repo string, env Env) (*Manifest, error) {
	path := ManifestPath(repo)
	data, err := readRegularFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("manifest not found at %s; run `dotty init %s`", path, repo)
		}
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var manifest Manifest
	if err := toml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	manifest.normalize()
	if err := ValidateManifest(&manifest, env); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func SaveManifest(tx *Tx, repo string, manifest *Manifest, env Env) error {
	manifest.normalize()
	if err := ValidateManifest(manifest, env); err != nil {
		return err
	}
	data := []byte(FormatManifest(manifest))
	return WriteFileTx(tx, ManifestPath(repo), data, 0o644)
}

func ValidateManifest(manifest *Manifest, env Env) error {
	manifest.normalize()
	if manifest.Version != ManifestVersion {
		return fmt.Errorf("unsupported manifest version %d", manifest.Version)
	}

	for name, pkg := range manifest.Packages {
		if err := validateName("package", name); err != nil {
			return err
		}
		targets := map[string]string{}
		for i, link := range pkg.Links {
			if err := validateSourcePath(link.Source); err != nil {
				return fmt.Errorf("package %q link %d: %w", name, i+1, err)
			}
			if err := validateTargetPath(link.Target); err != nil {
				return fmt.Errorf("package %q link %d: %w", name, i+1, err)
			}
			targetAbs, err := ExpandTargetPath(link.Target, env)
			if err != nil {
				return fmt.Errorf("package %q link %d: %w", name, i+1, err)
			}
			key := filepath.Clean(targetAbs)
			if prev, ok := targets[key]; ok {
				return fmt.Errorf(
					"package %q target %q is mapped more than once (%s and %s) (edit dotty.toml so each Target Path appears once)",
					name,
					link.Target,
					prev,
					link.Source,
				)
			}
			targets[key] = link.Source
		}
	}

	for name, collection := range manifest.Collections {
		if err := validateName("collection", name); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, packageName := range collection.Packages {
			if err := validateName("package", packageName); err != nil {
				return fmt.Errorf("collection %q: %w", name, err)
			}
			if _, ok := manifest.Packages[packageName]; !ok {
				return fmt.Errorf("collection %q references unknown package %q", name, packageName)
			}
			if seen[packageName] {
				return fmt.Errorf(
					"collection %q references package %q more than once",
					name,
					packageName,
				)
			}
			seen[packageName] = true
		}
	}
	return nil
}

func FormatManifest(manifest *Manifest) string {
	manifest.normalize()
	var b bytes.Buffer
	fmt.Fprintf(&b, "version = %d\n", manifest.Version)

	packageNames := sortedKeys(manifest.Packages)
	for _, name := range packageNames {
		pkg := manifest.Packages[name]
		fmt.Fprintf(&b, "\n[packages.%s]\n", tomlKey(name))
		if len(pkg.Links) == 0 {
			b.WriteString("links = []\n")
			continue
		}
		b.WriteString("links = [\n")
		for _, link := range pkg.Links {
			fmt.Fprintf(
				&b,
				"  { source = %s, target = %s },\n",
				strconv.Quote(link.Source),
				strconv.Quote(link.Target),
			)
		}
		b.WriteString("]\n")
	}

	collectionNames := sortedKeys(manifest.Collections)
	for _, name := range collectionNames {
		collection := manifest.Collections[name]
		fmt.Fprintf(&b, "\n[collections.%s]\n", tomlKey(name))
		b.WriteString("packages = [")
		for i, packageName := range collection.Packages {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.Quote(packageName))
		}
		b.WriteString("]\n")
	}
	return b.String()
}

func AddManifestLink(manifest *Manifest, packageName string, link LinkMapping, env Env) error {
	manifest.normalize()
	pkg := manifest.Packages[packageName]
	for _, existing := range pkg.Links {
		if existing.Source == link.Source && existing.Target == link.Target {
			manifest.Packages[packageName] = pkg
			return nil
		}
		if existing.Target == link.Target {
			return fmt.Errorf(
				"package %q already maps target %q (edit dotty.toml to change the existing Link Mapping)",
				packageName,
				link.Target,
			)
		}
	}
	pkg.Links = append(pkg.Links, link)
	manifest.Packages[packageName] = pkg
	return ValidateManifest(manifest, env)
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func validateName(kind, name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf(
			"%s name %q must start with a letter or digit and contain only letters, digits, '_' or '-'",
			kind,
			name,
		)
	}
	return nil
}

func validateSourcePath(source string) error {
	if source == "" {
		return fmt.Errorf("source is empty")
	}
	if filepath.IsAbs(source) || strings.HasPrefix(source, "~") {
		return fmt.Errorf("source %q must be relative to the package root", source)
	}
	clean := filepath.Clean(filepath.FromSlash(source))
	if clean == "." {
		return nil
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("source %q escapes the package root", source)
	}
	return nil
}

func validateTargetPath(target string) error {
	if target == "" {
		return fmt.Errorf("target is empty")
	}
	if strings.HasPrefix(target, "~") {
		if target != "~" && !strings.HasPrefix(target, "~/") {
			return fmt.Errorf("target %q has unsupported home syntax", target)
		}
		return nil
	}
	if !filepath.IsAbs(target) {
		return fmt.Errorf("target %q must be absolute or home-relative", target)
	}
	return nil
}

func tomlKey(name string) string {
	if namePattern.MatchString(name) {
		return name
	}
	return strconv.Quote(name)
}
