package dotty

import (
	"fmt"
	"os"
)

// InitRepo initializes a Dotfiles Repository at repoPath, creates the Manifest
// if missing, validates an existing Manifest, records the Default Repository in
// user config, and returns a ready-to-use Service.
func InitRepo(repoPath string, env Env) (Service, error) {
	if err := validateTOMLString("repo", repoPath); err != nil {
		return Service{}, err
	}
	repo, err := ExpandPath(repoPath, env)
	if err != nil {
		return Service{}, err
	}
	cfg := &Config{Repo: HomeRelative(repo, env)}
	if err := validateTOMLString("config repo", cfg.Repo); err != nil {
		return Service{}, err
	}
	if err := withRepositoryInitLock(repo, func() error {
		// Init writes config even with a positional repository path. Refuse an
		// unsupported existing document before creating any repository content.
		if _, err := LoadConfig(env); err != nil {
			return err
		}
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

			return SaveConfig(tx, env, cfg)
		})
	}); err != nil {
		return Service{}, err
	}
	return NewService(repo, env), nil
}
