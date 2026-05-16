package dotty

import (
	"fmt"
	"os"
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
	if err := validateTargetTopology(targetAbs, repo, env); err != nil {
		return err
	}
	return validateTargetParentsAreLexicalDirectories(targetAbs, env)
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

func validateTargetParentsAreLexicalDirectories(targetAbs string, env Env) error {
	target := filepath.Clean(targetAbs)
	if env.Home != "" {
		home := filepath.Clean(env.Home)
		if isWithin(home, target) {
			for parent := filepath.Dir(target); parent != home; parent = filepath.Dir(parent) {
				if err := validateTargetParentIsLexicalDirectory(targetAbs, parent); err != nil {
					return err
				}
			}
			return nil
		}
	}

	return validateAbsoluteTargetParentsAreLexicalDirectories(targetAbs, filepath.Dir(target))
}

func validateAbsoluteTargetParentsAreLexicalDirectories(targetAbs, firstParent string) error {
	for parent := firstParent; parent != "." && parent != string(filepath.Separator) && parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
		if parent != firstParent && filepath.Dir(parent) == string(filepath.Separator) {
			return nil
		}
		if err := validateTargetParentIsLexicalDirectory(targetAbs, parent); err != nil {
			return err
		}
	}
	return nil
}

func validateTargetParentIsLexicalDirectory(targetAbs, parent string) error {
	if parent == "." || parent == string(filepath.Separator) || parent == filepath.Dir(parent) {
		return nil
	}
	info, err := os.Lstat(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("inspect Target Path parent %s: %w", parent, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf(
			"target path %s has symlinked parent %s (operate on the lexical Target Path parent directly before continuing)",
			targetAbs,
			parent,
		)
	}
	return nil
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
