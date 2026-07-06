package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

func LoadFile(path string) (*Config, []Diagnostic, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read config file: %w", err)
	}

	return loadConfig(b, filepath.Dir(path))
}

func LoadBytes(data []byte) (*Config, []Diagnostic, error) {
	return loadConfig(data, ".")
}

func loadConfig(data []byte, configDir string) (*Config, []Diagnostic, error) {
	// Parse and apply defaults, but defer validation until after environment
	// resolution so values like "$VAR" are validated in their resolved form
	// (and validation runs exactly once).
	cfg, err := parseConfig(data)
	if err != nil {
		return nil, nil, err
	}

	diags, err := resolveEnvAndExpand(cfg, configDir)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve environment variables: %w", err)
	}

	diags = append(diags, Validate(cfg)...)
	for _, d := range diags {
		if d.Severity == SeverityError {
			return cfg, diags, ValidationError{Diagnostics: diags}
		}
	}

	return cfg, diags, nil
}

func parseConfig(data []byte) (*Config, error) {
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	if cfg.Downloads == nil {
		cfg.Downloads = map[string]*DownloadTask{}
	}

	applyDefaults(cfg)
	return cfg, nil
}
