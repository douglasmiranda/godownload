package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		if duplicateErrors := duplicateDownloadTaskErrors(data); len(duplicateErrors) > 0 {
			return nil, fmt.Errorf("parse yaml: %s", strings.Join(duplicateErrors, "; "))
		}
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	if cfg.Downloads == nil {
		cfg.Downloads = map[string]*DownloadTask{}
	}

	applyDefaults(cfg)
	return cfg, nil
}

func duplicateDownloadTaskErrors(data []byte) []string {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil || len(document.Content) == 0 {
		return nil
	}

	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil
	}

	var duplicateErrors []string
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		if key.Value != "downloads" || value.Kind != yaml.MappingNode {
			continue
		}

		firstDefinitionLines := make(map[string]int)
		for j := 0; j+1 < len(value.Content); j += 2 {
			taskName := value.Content[j]
			if firstLine, exists := firstDefinitionLines[taskName.Value]; exists {
				duplicateErrors = append(duplicateErrors,
					fmt.Sprintf("task %q already defined at line %d", taskName.Value, firstLine))
				continue
			}
			firstDefinitionLines[taskName.Value] = taskName.Line
		}
	}

	return duplicateErrors
}
