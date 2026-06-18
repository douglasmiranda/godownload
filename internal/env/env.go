package env

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func MergeWithPrecedence(base map[string]string, layers ...map[string]string) map[string]string {
	out := make(map[string]string, len(base))
	for k, v := range base {
		out[k] = v
	}
	for _, layer := range layers {
		for k, v := range layer {
			out[k] = v
		}
	}
	return out
}

func SystemEnvMap() map[string]string {
	out := map[string]string{}
	for _, kv := range os.Environ() {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}
		out[parts[0]] = parts[1]
	}
	return out
}

func LoadEnvFiles(paths []string, baseDir string) (map[string]string, error) {
	// First file in the list has highest precedence by project contract.
	out := map[string]string{}
	for i := len(paths) - 1; i >= 0; i-- {
		p := strings.TrimSpace(paths[i])
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(baseDir, p)
		}
		vars, err := ParseDotEnvFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("parse env_file %s: %w", p, err)
		}
		for k, v := range vars {
			out[k] = v
		}
	}
	return out, nil
}

func ParseDotEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := map[string]string{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = stripMatchingQuotes(val)
		if key == "" {
			continue
		}
		out[key] = val
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// stripMatchingQuotes removes a single pair of surrounding quotes (either
// double or single) without trimming unmatched or interior quote characters.
func stripMatchingQuotes(v string) string {
	if len(v) < 2 {
		return v
	}
	first := v[0]
	last := v[len(v)-1]
	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return v[1 : len(v)-1]
	}
	return v
}
