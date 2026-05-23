package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type TableMode string

const (
	TableModeStructureAndData TableMode = "structure_and_data"
	TableModeStructureOnly    TableMode = "structure_only"
	TableModeIgnore           TableMode = "ignore"
	TableModeCustomRule       TableMode = "custom_rule"
)

type Config struct {
	LivePath        string               `json:"live_path"`
	StagePath       string               `json:"stage_path"`
	OrderCount      int                  `json:"order_count"`
	OrderPreference string               `json:"order_preference"` // "last" or "first"
	TableModes      map[string]TableMode `json:"table_modes"`
}

func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".wp-sync.json"), nil
}

func Load() (*Config, error) {
	p, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

func Save(cfg *Config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0644)
}
