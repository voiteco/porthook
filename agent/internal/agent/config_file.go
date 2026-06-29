// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type ConfigFile struct {
	ServerURL string `json:"server_url"`
	Token     string `json:"token"`
}

func ConfigFilePath() (string, error) {
	if path := os.Getenv("PORTHOOK_CONFIG_PATH"); path != "" {
		return path, nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(configDir, "porthook", "config.json"), nil
}

func LoadConfigFile() (ConfigFile, bool, error) {
	path, err := ConfigFilePath()
	if err != nil {
		return ConfigFile{}, false, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ConfigFile{}, false, nil
	}
	if err != nil {
		return ConfigFile{}, false, fmt.Errorf("read config file: %w", err)
	}

	var cfg ConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ConfigFile{}, false, fmt.Errorf("decode config file: %w", err)
	}
	return cfg, true, nil
}

func SaveConfigFile(cfg ConfigFile) error {
	path, err := ConfigFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config file: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	return nil
}

func RemoveConfigFile() error {
	path, err := ConfigFilePath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove config file: %w", err)
	}
	return nil
}
