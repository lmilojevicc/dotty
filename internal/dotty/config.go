package dotty

import (
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

func LoadConfig(env Env) (*Config, error) {
	path := env.ConfigFilePath()
	data, err := readRegularFile(path)
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

func SaveConfig(tx *Tx, env Env, cfg *Config) error {
	path := env.ConfigFilePath()
	if err := EnsureDirTx(tx, dirOf(path), 0o755); err != nil {
		return err
	}
	data := []byte(fmt.Sprintf("repo = %q\n", cfg.Repo))
	return WriteFileTx(tx, path, data, 0o644)
}
