// Package role provides role-based skill loading for Reef Clients.
package role

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config defines a role's skill manifest and system prompt override.
type Config struct {
	Name         string   `yaml:"name"`
	Skills       []string `yaml:"skills"`
	SystemPrompt string   `yaml:"system_prompt,omitempty"`
}

// Load reads and parses a role configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read role config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse role config %s: %w", path, err)
	}

	if cfg.Name == "" {
		return nil, fmt.Errorf("role config %s: name is required", path)
	}
	if len(cfg.Skills) == 0 {
		return nil, fmt.Errorf("role config %s: at least one skill is required", path)
	}

	return &cfg, nil
}

// Validate checks that all listed skills exist in the available set.
func (c *Config) Validate(available map[string]struct{}) error {
	var missing []string
	for _, s := range c.Skills {
		if _, ok := available[s]; !ok {
			missing = append(missing, s)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("role %s: unknown skills: %v", c.Name, missing)
	}
	return nil
}
