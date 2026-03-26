package env

import (
	"fmt"
	"regexp"
	"strings"
)

var envVarPattern = regexp.MustCompile(`\$[A-Za-z_][A-Za-z0-9_]*`)

func ExpandString(value string, vars map[string]string) (string, []string) {
	missing := []string{}
	replaced := envVarPattern.ReplaceAllStringFunc(value, func(token string) string {
		k := strings.TrimPrefix(token, "$")
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
