package config

import "fmt"

type Severity string

const (
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type Diagnostic struct {
	Severity Severity
	TaskName string
	Field    string
	Message  string
}

func (d Diagnostic) String() string {
	if d.TaskName == "" {
		return fmt.Sprintf("[%s] %s: %s", d.Severity, d.Field, d.Message)
	}
	return fmt.Sprintf("[%s] %s.%s: %s", d.Severity, d.TaskName, d.Field, d.Message)
}

type ValidationError struct {
	Diagnostics []Diagnostic
}

func (e ValidationError) Error() string {
	count := 0
	for _, d := range e.Diagnostics {
		if d.Severity == SeverityError {
			count++
		}
	}
	return fmt.Sprintf("configuration validation failed with %d error(s)", count)
}
