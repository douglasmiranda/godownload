package config

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	configType      = reflect.TypeOf(Config{})
	retryType       = reflect.TypeOf(Retry{})
	retryConfigType = reflect.TypeOf(RetryConfig{})
)

// unknownFieldDiagnostics reports YAML keys that do not correspond to any
// configuration field. yaml.v3's KnownFields option does not reach the custom
// UnmarshalYAML methods, so the document is checked against the types here.
func unknownFieldDiagnostics(data []byte) []Diagnostic {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil || len(document.Content) == 0 {
		return nil
	}

	diags := []Diagnostic{}
	walkKnownFields(document.Content[0], configType, nil, &diags)

	var extra yaml.Node
	if err := decoder.Decode(&extra); err == nil {
		diags = append(diags, Diagnostic{Severity: SeverityError, Field: "yaml", Message: "multiple YAML documents are not supported"})
	}
	return diags
}

func walkKnownFields(node *yaml.Node, target reflect.Type, path []string, diags *[]Diagnostic) {
	if node == nil {
		return
	}
	if node.Kind == yaml.AliasNode {
		walkKnownFields(node.Alias, target, path, diags)
		return
	}
	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	if target == retryType {
		if node.Kind != yaml.MappingNode {
			return
		}
		target = retryConfigType
	}

	switch node.Kind {
	case yaml.MappingNode:
		var fields map[string]reflect.Type
		if target.Kind() == reflect.Struct {
			fields = yamlFieldTypes(target)
		} else if target.Kind() != reflect.Map {
			return
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if key.Tag == "!!merge" {
				walkMergeValue(value, target, path, diags)
				continue
			}
			childPath := appendPath(path, key.Value)
			if target.Kind() == reflect.Map {
				walkKnownFields(value, target.Elem(), childPath, diags)
				continue
			}
			fieldType, ok := fields[key.Value]
			if !ok {
				*diags = append(*diags, unknownFieldDiagnostic(childPath, target))
				continue
			}
			walkKnownFields(value, fieldType, childPath, diags)
		}
	case yaml.SequenceNode:
		if target.Kind() != reflect.Slice && target.Kind() != reflect.Array {
			return
		}
		for _, child := range node.Content {
			walkKnownFields(child, target.Elem(), path, diags)
		}
	}
}

func walkMergeValue(value *yaml.Node, target reflect.Type, path []string, diags *[]Diagnostic) {
	if value.Kind == yaml.SequenceNode {
		for _, child := range value.Content {
			walkKnownFields(child, target, path, diags)
		}
		return
	}
	walkKnownFields(value, target, path, diags)
}

func unknownFieldDiagnostic(path []string, parent reflect.Type) Diagnostic {
	key := path[len(path)-1]
	msg := "unknown field"
	if suggestion := suggestField(key, parent); suggestion != "" {
		msg = fmt.Sprintf("unknown field (did you mean %q?)", suggestion)
	}
	d := Diagnostic{Severity: SeverityError, Field: strings.Join(path, "."), Message: msg}
	if len(path) > 2 && path[0] == "downloads" {
		d.TaskName = path[1]
		d.Field = strings.Join(path[2:], ".")
	}
	return d
}

// suggestField looks for a likely intended key: a close spelling of a sibling
// field, or the same key one level down (e.g. "allowed_domains" placed outside
// "security").
func suggestField(key string, parent reflect.Type) string {
	fields := yamlFieldTypes(parent)
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)

	best, bestDistance := "", -1
	for _, name := range names {
		d := editDistance(key, name)
		if bestDistance == -1 || d < bestDistance {
			best, bestDistance = name, d
		}
	}
	if bestDistance >= 0 && bestDistance <= maxSuggestionDistance(key) {
		return best
	}

	for _, name := range names {
		child := fields[name]
		for child.Kind() == reflect.Pointer {
			child = child.Elem()
		}
		if child == retryType {
			child = retryConfigType
		}
		if child.Kind() != reflect.Struct {
			continue
		}
		if _, ok := yamlFieldTypes(child)[key]; ok {
			return name + "." + key
		}
	}
	return ""
}

func maxSuggestionDistance(key string) int {
	if len(key) > 10 {
		return 3
	}
	return 2
}

func yamlFieldTypes(target reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type, target.NumField())
	for i := 0; i < target.NumField(); i++ {
		field := target.Field(i)
		if !field.IsExported() {
			continue
		}
		name := strings.Split(field.Tag.Get("yaml"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = strings.ToLower(field.Name)
		}
		fields[name] = field.Type
	}
	return fields
}

func appendPath(path []string, key string) []string {
	out := make([]string, len(path), len(path)+1)
	copy(out, path)
	return append(out, key)
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}
