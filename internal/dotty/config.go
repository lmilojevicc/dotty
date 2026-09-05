package dotty

import (
	"fmt"
	"os"
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
	if err := decodeTOML(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &cfg, nil
}

func SaveConfig(tx *Tx, env Env, cfg *Config) error {
	path := env.ConfigFilePath()
	if err := validateTOMLString("config repo", cfg.Repo); err != nil {
		return fmt.Errorf("save config %s: %w", path, err)
	}
	if _, err := LoadConfig(env); err != nil {
		return err
	}
	data := []byte("repo = " + tomlBasicString(cfg.Repo) + "\n")
	return WriteFileTx(tx, path, data, 0o644)
}
