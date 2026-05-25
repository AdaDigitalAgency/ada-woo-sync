package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	Anonymize       bool                 `json:"anonymize"`
	TableModes      map[string]TableMode `json:"table_modes"`
	RsyncExcludes   []string             `json:"rsync_excludes,omitempty"`
}

func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "wp-stage-sync"), nil
}

func sitesDir() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sites"), nil
}

// DomainFromPath extracts the domain from a webroot path.
func DomainFromPath(webroot string) string {
	return filepath.Base(strings.TrimRight(webroot, "/"))
}

func siteConfigPath(domain string) (string, error) {
	dir, err := sitesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, domain+".json"), nil
}

// Load reads the config for a specific domain.
func Load(domain string) (*Config, error) {
	p, err := siteConfigPath(domain)
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

// Save writes the config, deriving the domain from LivePath.
func Save(cfg *Config) error {
	domain := DomainFromPath(cfg.LivePath)
	if domain == "" || domain == "." {
		return fmt.Errorf("cannot derive domain from live path: %s", cfg.LivePath)
	}
	dir, err := sitesDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	p, err := siteConfigPath(domain)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0644)
}

// SavedSite represents a saved per-site configuration.
type SavedSite struct {
	Domain string
	Config *Config
}

// ListSites returns all saved site configurations.
func ListSites() []SavedSite {
	dir, err := sitesDir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var sites []SavedSite
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		domain := strings.TrimSuffix(e.Name(), ".json")
		cfg, err := Load(domain)
		if err != nil {
			continue
		}
		sites = append(sites, SavedSite{Domain: domain, Config: cfg})
	}
	return sites
}
