package dotty

import (
	"fmt"
	"os"
)

// InitRepo initializes a Dotfiles Repository at repoPath, creates the Manifest
// if missing, validates an existing Manifest, records the Default Repository in
// user config, and returns a ready-to-use Service.
func InitRepo(repoPath string, env Env) (Service, error) {
	repo, err := ExpandPath(repoPath, env)
	if err != nil {
		return Service{}, err
	}
	if err := withRepositoryInitLock(repo, func() error {
		return RunAtomic(func(tx *Tx) error {
			if err := EnsureDirTx(tx, repo, 0o755); err != nil {
				return err
			}

			manifestPath := ManifestPath(repo)
			if _, err := os.Lstat(manifestPath); err == nil {
				if _, err := LoadManifest(repo, env); err != nil {
					return err
				}
			} else if os.IsNotExist(err) {
				if err := SaveManifest(tx, repo, NewManifest(), env); err != nil {
					return err
				}
			} else {
				return fmt.Errorf("inspect manifest %s: %w", manifestPath, err)
			}

			return SaveConfig(tx, env, &Config{Repo: HomeRelative(repo, env)})
		})
	}); err != nil {
		return Service{}, err
	}
	return NewService(repo, env), nil
}
