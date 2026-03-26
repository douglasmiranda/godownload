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

	cfg, _, err := LoadBytes(b)
	if err != nil {
		return cfg, nil, err
	}

	configDir := filepath.Dir(path)
	diags, err := resolveEnvAndExpand(cfg, configDir)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve environment variables: %w", err)
	}

	postDiags := Validate(cfg)
	diags = append(diags, postDiags...)
	for _, d := range diags {
		if d.Severity == SeverityError {
			return cfg, diags, ValidationError{Diagnostics: diags}
		}
	}

	return cfg, diags, nil
}

func LoadBytes(data []byte) (*Config, []Diagnostic, error) {
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, nil, fmt.Errorf("parse yaml: %w", err)
	}

	if cfg.Downloads == nil {
		cfg.Downloads = map[string]*DownloadTask{}
	}

	applyDefaults(cfg)
	diags := Validate(cfg)
	for _, d := range diags {
		if d.Severity == SeverityError {
			return cfg, diags, ValidationError{Diagnostics: diags}
		}
	}
	return cfg, diags, nil
}
