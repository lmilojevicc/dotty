package dotty

import (
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

func LoadConfig() (*Config, error) {
	path, err := ConfigFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &cfg, nil
}

func SaveConfig(tx *Tx, cfg *Config) error {
	path, err := ConfigFilePath()
	if err != nil {
		return err
	}
	if err := EnsureDirTx(tx, dirOf(path), 0o755); err != nil {
		return err
	}
	data := []byte(fmt.Sprintf("repo = %q\n", cfg.Repo))
	return WriteFileTx(tx, path, data, 0o644)
}
