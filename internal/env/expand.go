package env

import (
	"fmt"
	"regexp"
	"strings"
)

// envVarPattern matches, in order: a literal "$$" escape, the braced form
// "${VAR}", and the bare form "$VAR".
var envVarPattern = regexp.MustCompile(`\$\$|\$\{[A-Za-z_][A-Za-z0-9_]*\}|\$[A-Za-z_][A-Za-z0-9_]*`)

func ExpandString(value string, vars map[string]string) (string, []string) {
	missing := []string{}
	replaced := envVarPattern.ReplaceAllStringFunc(value, func(token string) string {
		if token == "$$" {
			return "$"
		}
		k := token[1:]
		if strings.HasPrefix(k, "{") {
			k = strings.TrimSuffix(strings.TrimPrefix(k, "{"), "}")
		}
		v, ok := vars[k]
		if !ok {
			missing = append(missing, k)
			return token
		}
		return v
	})
	return replaced, missing
}

func MissingVarsError(vars []string) error {
	if len(vars) == 0 {
		return nil
	}
	return fmt.Errorf("missing environment variable(s): %s", strings.Join(vars, ", "))
}
