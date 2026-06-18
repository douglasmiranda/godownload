package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ByteSize is a byte count that can be expressed in YAML either as a plain
// integer number of bytes (e.g. 1048576) or with a human-friendly unit suffix
// (e.g. "500kb", "10mb", "1gb"). Units are binary (1kb = 1024 bytes).
type ByteSize int64

func (b ByteSize) Int64() int64 { return int64(b) }

var byteSizePattern = regexp.MustCompile(`^(\d+)\s*([a-z]*)$`)

func parseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, nil
	}
	m := byteSizePattern.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid size %q (use bytes or a suffix like 500kb, 10mb, 1gb)", s)
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	var mult int64
	switch m[2] {
	case "", "b":
		mult = 1
	case "k", "kb", "kib":
		mult = 1 << 10
	case "m", "mb", "mib":
		mult = 1 << 20
	case "g", "gb", "gib":
		mult = 1 << 30
	case "t", "tb", "tib":
		mult = 1 << 40
	default:
		return 0, fmt.Errorf("invalid size unit in %q (supported: b, kb, mb, gb, tb)", s)
	}
	if mult > 1 && n > (1<<62)/mult {
		return 0, fmt.Errorf("size %q is too large", s)
	}
	return n * mult, nil
}

func (b *ByteSize) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Kind != yaml.ScalarNode {
		return fmt.Errorf("size must be a scalar (bytes or a value like 500kb, 10mb, 1gb)")
	}
	n, err := parseByteSize(value.Value)
	if err != nil {
		return err
	}
	*b = ByteSize(n)
	return nil
}
