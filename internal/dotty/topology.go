package dotty

import (
	"fmt"
	"path/filepath"
)

func ValidateManifestForRepo(manifest *Manifest, repo string, env Env) error {
	if err := ValidateManifest(manifest, env); err != nil {
		return err
	}
	manifest.normalize()
	for packageName, pkg := range manifest.Packages {
		for i, link := range pkg.Links {
			if err := validateLinkMappingTopology(repo, packageName, link, env); err != nil {
				return fmt.Errorf("package %q link %d: %w", packageName, i+1, err)
			}
		}
	}
	return nil
}

func validateLinkMappingTopology(repo, packageName string, link LinkMapping, env Env) error {
	if _, err := PackageSourcePath(repo, packageName, link.Source); err != nil {
		return err
	}
	targetAbs, err := ExpandTargetPath(link.Target, env)
	if err != nil {
		return err
	}
	return validateTargetTopology(targetAbs, repo, env)
}

func validateTargetTopology(targetAbs, repo string, env Env) error {
	target := topologyPath(targetAbs)
	repoPath := topologyPath(repo)
	if pathsOverlap(target.clean, repoPath.clean) ||
		pathsOverlap(target.parentResolved, repoPath.resolved) {
		return dangerousTargetPathError(targetAbs, repo, "Dotfiles Repository")
	}

	if env.Home != "" {
		homePath := topologyPath(env.Home)
		if containsPath(target.clean, homePath.clean) ||
			containsPath(target.parentResolved, homePath.resolved) {
			return dangerousTargetPathError(targetAbs, env.Home, "home directory")
		}
	}

	return nil
}

type topologyPathState struct {
	clean          string
	resolved       string
	parentResolved string
}

func topologyPath(path string) topologyPathState {
	clean := filepath.Clean(path)
	return topologyPathState{
		clean:          clean,
		resolved:       resolveExistingPath(clean),
		parentResolved: resolveThroughParent(clean),
	}
}

func resolveThroughParent(path string) string {
	parent := filepath.Dir(path)
	resolvedParent := resolveExistingPath(parent)
	return filepath.Clean(filepath.Join(resolvedParent, filepath.Base(path)))
}

func resolveExistingPath(path string) string {
	clean := filepath.Clean(path)
	existing, ok := nearestExistingPath(clean)
	if !ok {
		return clean
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return clean
	}
	rel, err := filepath.Rel(existing, clean)
	if err != nil || rel == "." {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(filepath.Join(resolved, rel))
}

func pathsOverlap(a, b string) bool {
	return containsPath(a, b) || containsPath(b, a)
}

func containsPath(parent, child string) bool {
	return isWithin(filepath.Clean(parent), filepath.Clean(child))
}

func dangerousTargetPathError(targetAbs, protectedPath, protectedName string) error {
	return fmt.Errorf(
		"dangerous Target Path %s overlaps the %s %s (choose a Target Path outside protected Dotty state)",
		targetAbs,
		protectedName,
		protectedPath,
	)
}
