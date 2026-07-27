package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/douglasmiranda/godownload/internal/config"
	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: go run %s <download.yaml>\n", os.Args[0])
		os.Exit(2)
	}

	path := os.Args[1]
	if err := validateKnownFields(path); err != nil {
		fmt.Fprintf(os.Stderr, "strict YAML validation failed: %v\n", err)
		os.Exit(1)
	}

	_, diagnostics, err := config.LoadFile(path)
	warnings := 0
	for _, diagnostic := range diagnostics {
		fmt.Fprintln(os.Stderr, diagnostic.String())
		if diagnostic.Severity == config.SeverityWarning {
			warnings++
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration invalid: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("configuration valid (%d warning(s))\n", warnings)
}

func validateKnownFields(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return err
	}
	if len(document.Content) != 1 {
		return fmt.Errorf("expected one YAML document")
	}
	if err := validateNodeFields(document.Content[0], reflect.TypeOf(config.Config{}), ""); err != nil {
		return err
	}

	var extra any
	err = decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("multiple YAML documents are not supported")
}

var retryType = reflect.TypeOf(config.Retry{})

func validateNodeFields(node *yaml.Node, target reflect.Type, path string) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.AliasNode {
		return validateNodeFields(node.Alias, target, path)
	}

	for target.Kind() == reflect.Pointer {
		target = target.Elem()
	}
	if target == retryType && node.Kind == yaml.MappingNode {
		target = reflect.TypeOf(config.RetryConfig{})
	}

	switch node.Kind {
	case yaml.MappingNode:
		switch target.Kind() {
		case reflect.Map:
			for index := 0; index+1 < len(node.Content); index += 2 {
				key := node.Content[index].Value
				if err := validateNodeFields(node.Content[index+1], target.Elem(), joinPath(path, key)); err != nil {
					return err
				}
			}
		case reflect.Struct:
			fields := yamlFields(target)
			for index := 0; index+1 < len(node.Content); index += 2 {
				key := node.Content[index].Value
				fieldType, ok := fields[key]
				if !ok {
					return fmt.Errorf("%s: unknown field", joinPath(path, key))
				}
				if err := validateNodeFields(node.Content[index+1], fieldType, joinPath(path, key)); err != nil {
					return err
				}
			}
		}
	case yaml.SequenceNode:
		if target.Kind() == reflect.Slice || target.Kind() == reflect.Array {
			for index, child := range node.Content {
				if err := validateNodeFields(child, target.Elem(), fmt.Sprintf("%s[%d]", path, index)); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func yamlFields(target reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type)
	for index := 0; index < target.NumField(); index++ {
		field := target.Field(index)
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

func joinPath(parent string, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}
