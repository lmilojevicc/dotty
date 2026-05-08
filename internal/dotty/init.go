package dotty

import (
	"fmt"
	"os"
)

func Init(repoPath string) (string, error) {
	repo, err := ExpandPath(repoPath)
	if err != nil {
		return "", err
	}
	if err := RunAtomic(func(tx *Tx) error {
		if err := EnsureDirTx(tx, repo, 0o755); err != nil {
			return err
		}

		manifestPath := ManifestPath(repo)
		if _, err := os.Lstat(manifestPath); err == nil {
			if _, err := LoadManifest(repo); err != nil {
				return err
			}
		} else if os.IsNotExist(err) {
			if err := SaveManifest(tx, repo, NewManifest()); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("inspect manifest %s: %w", manifestPath, err)
		}

		return SaveConfig(tx, &Config{Repo: HomeRelative(repo)})
	}); err != nil {
		return "", err
	}
	return repo, nil
}
