package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func Validate(cfg *Config) []Diagnostic {
	diags := make([]Diagnostic, 0)

	if cfg.Config.Concurrency <= 0 {
		diags = append(diags, Diagnostic{Severity: SeverityError, Field: "config.concurrency", Message: "must be greater than 0"})
	}

	for name, task := range cfg.Downloads {
		if task == nil {
			diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: name, Field: "task", Message: "task cannot be null"})
			continue
		}
		diags = append(diags, validateTask(name, task)...)
	}

	return diags
}

func validateTask(name string, task *DownloadTask) []Diagnostic {
	diags := make([]Diagnostic, 0)

	if strings.TrimSpace(task.URL) == "" {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: name, Field: "url", Message: "is required"})
	} else if !looksLikeTemplate(task.URL) && !looksLikeEnvRef(task.URL) {
		if err := validateURL(task.URL); err != nil {
			diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: name, Field: "url", Message: err.Error()})
		}
	}

	urlGenCount := 0
	if task.URLGenRange != nil {
		urlGenCount++
		diags = append(diags, validateURLGenRange(name, task.URLGenRange)...)
	}
	if task.URLGenList != nil {
		urlGenCount++
		diags = append(diags, validateURLGenList(name, task.URLGenList)...)
	}
	if task.URLGenFile != nil {
		urlGenCount++
		diags = append(diags, validateURLGenFile(name, task.URLGenFile)...)
	}
	if task.URLGenDates != nil {
		urlGenCount++
		diags = append(diags, validateURLGenDates(name, task.URLGenDates)...)
	}
	if urlGenCount > 1 {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: name, Field: "url_gen_*", Message: "only one url_gen_* block can be configured per task"})
	}

	if strings.TrimSpace(task.RegexFilter) != "" {
		if _, err := regexp.Compile(task.RegexFilter); err != nil {
			diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: name, Field: "regex_filter", Message: fmt.Sprintf("invalid regex: %v", err)})
		}
	}
	if strings.TrimSpace(task.AttrValRegexFilter) != "" {
		if _, err := regexp.Compile(task.AttrValRegexFilter); err != nil {
			diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: name, Field: "attr_val_regex_filter", Message: fmt.Sprintf("invalid regex: %v", err)})
		}
	}

	if task.Timeout < 0 {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: name, Field: "timeout", Message: "must be greater than or equal to 0"})
	}
	if task.StopOnCode != 0 && (task.StopOnCode < 100 || task.StopOnCode > 599) {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: name, Field: "stop_on_code", Message: "must be a valid HTTP status code"})
	}
	if task.DownloadLimit < 0 {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: name, Field: "download_limit", Message: "must be greater than or equal to 0"})
	}
	if task.SelectorMatchesLimit < 0 {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: name, Field: "selector_matches_limit", Message: "must be greater than or equal to 0"})
	}

	if strings.TrimSpace(task.Save.Filename) != "" && taskCanResolveMultipleURLs(task) {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: name, Field: "save.filename", Message: "cannot be used when selector or url_gen_* can resolve multiple URLs"})
	}
	if strings.TrimSpace(task.Save.Filename) != "" && strings.TrimSpace(task.Save.FilenameTemplate) != "" {
		diags = append(diags, Diagnostic{Severity: SeverityWarning, TaskName: name, Field: "save.filename_template", Message: "is ignored when save.filename is set"})
	}
	if strings.TrimSpace(task.BaseDownloadDir) != "" {
		diags = append(diags, Diagnostic{Severity: SeverityWarning, TaskName: name, Field: "base_download_dir", Message: "top-level base_download_dir is deprecated; use save.base_download_dir"})
	}

	if task.MaxDepth < 1 {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: name, Field: "max_depth", Message: "must be greater than or equal to 1"})
	}
	if strings.TrimSpace(task.Selector) == "" && task.MaxDepth > DefaultMaxDepth {
		diags = append(diags, Diagnostic{Severity: SeverityWarning, TaskName: name, Field: "max_depth", Message: "has no effect when selector is not configured"})
	}

	diags = append(diags, validateRetry(name, task.Retry)...)
	diags = append(diags, validateAuth(name, task.Auth)...)
	diags = append(diags, validateSecurity(name, task.Security)...)

	return diags
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL scheme must be http or https")
	}
	if strings.TrimSpace(u.Host) == "" {
		return fmt.Errorf("URL must include host")
	}
	return nil
}

func validateURLGenRange(taskName string, cfg *URLGenRange) []Diagnostic {
	diags := make([]Diagnostic, 0)
	if strings.TrimSpace(cfg.VarName) == "" {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "url_gen_range.var_name", Message: "is required"})
	}
	if cfg.Step == 0 {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "url_gen_range.step", Message: "must not be zero"})
	}
	if cfg.RangeEnd < cfg.RangeStart {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "url_gen_range", Message: "range_end must be greater than or equal to range_start"})
	}
	return diags
}

func validateURLGenList(taskName string, cfg *URLGenList) []Diagnostic {
	diags := make([]Diagnostic, 0)
	if strings.TrimSpace(cfg.VarName) == "" {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "url_gen_list.var_name", Message: "is required"})
	}
	if len(cfg.Values) == 0 {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "url_gen_list.values", Message: "must have at least one value"})
	}
	return diags
}

func validateURLGenFile(taskName string, cfg *URLGenFile) []Diagnostic {
	diags := make([]Diagnostic, 0)
	if strings.TrimSpace(cfg.VarName) == "" {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "url_gen_file.var_name", Message: "is required"})
	}
	if strings.TrimSpace(cfg.File) == "" {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "url_gen_file.file", Message: "is required"})
	}
	return diags
}

func validateURLGenDates(taskName string, cfg *URLGenDates) []Diagnostic {
	diags := make([]Diagnostic, 0)
	if strings.TrimSpace(cfg.VarName) == "" {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "url_gen_dates.var_name", Message: "is required"})
	}
	if strings.TrimSpace(cfg.DateRangeStart) == "" {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "url_gen_dates.date_range_start", Message: "is required"})
	}
	if strings.TrimSpace(cfg.DateRangeEnd) == "" {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "url_gen_dates.date_range_end", Message: "is required"})
	}
	if strings.TrimSpace(cfg.DateFormat) == "" {
		cfg.DateFormat = "YYYY-MM-DD"
	}
	layout, err := dateFormatToGoLayout(cfg.DateFormat)
	if err != nil {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "url_gen_dates.date_format", Message: err.Error()})
		return diags
	}
	start, err := time.Parse(layout, cfg.DateRangeStart)
	if err != nil {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "url_gen_dates.date_range_start", Message: fmt.Sprintf("invalid date for format %s", cfg.DateFormat)})
		return diags
	}
	end, err := time.Parse(layout, cfg.DateRangeEnd)
	if err != nil {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "url_gen_dates.date_range_end", Message: fmt.Sprintf("invalid date for format %s", cfg.DateFormat)})
		return diags
	}
	if end.Before(start) {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "url_gen_dates", Message: "date_range_end must be greater than or equal to date_range_start"})
	}
	return diags
}

func dateFormatToGoLayout(format string) (string, error) {
	switch strings.TrimSpace(format) {
	case "YYYY-MM-DD":
		return "2006-01-02", nil
	case "MM/DD/YYYY":
		return "01/02/2006", nil
	case "DD-MM-YYYY":
		return "02-01-2006", nil
	default:
		return "", fmt.Errorf("must be one of YYYY-MM-DD, MM/DD/YYYY, DD-MM-YYYY")
	}
}

func validateRetry(taskName string, retry Retry) []Diagnostic {
	if !retry.Enabled {
		return nil
	}
	diags := make([]Diagnostic, 0)
	if retry.Config.Retries < 0 {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "retry.retries", Message: "must be greater than or equal to 0"})
	}
	if retry.Config.Delay < 0 {
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "retry.delay", Message: "must be greater than or equal to 0"})
	}
	for _, code := range retry.Config.OnCode {
		if code < 100 || code > 599 {
			diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "retry.on_code", Message: fmt.Sprintf("invalid HTTP code: %d", code)})
		}
	}
	return diags
}

func validateAuth(taskName string, auth *AuthConfig) []Diagnostic {
	if auth == nil {
		return nil
	}
	diags := make([]Diagnostic, 0)
	authType := strings.ToLower(strings.TrimSpace(auth.Type))
	switch authType {
	case "basic":
		if strings.TrimSpace(auth.Username) == "" {
			diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "auth.username", Message: "is required for basic auth"})
		}
		if strings.TrimSpace(auth.Password) == "" {
			diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "auth.password", Message: "is required for basic auth"})
		}
	case "bearer":
		if strings.TrimSpace(auth.Token) == "" {
			diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "auth.token", Message: "is required for bearer auth"})
		}
	case "api_key":
		if strings.TrimSpace(auth.KeyName) == "" {
			diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "auth.key_name", Message: "is required for api_key auth"})
		}
		if strings.TrimSpace(auth.KeyValue) == "" {
			diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "auth.key_value", Message: "is required for api_key auth"})
		}
		keyIn := strings.ToLower(strings.TrimSpace(auth.KeyIn))
		if keyIn != "header" && keyIn != "query" && keyIn != "cookie" {
			diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "auth.key_in", Message: "must be one of: header, query, cookie"})
		}
	default:
		diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "auth.type", Message: "must be one of: basic, bearer, api_key"})
	}
	return diags
}

func validateSecurity(taskName string, sec SecurityConfig) []Diagnostic {
	diags := make([]Diagnostic, 0)
	for _, domain := range sec.AllowedDomains {
		if strings.TrimSpace(domain) == "" {
			diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "security.allowed_domains", Message: "must not contain empty values"})
		}
	}
	for i, ext := range sec.AllowedFileTypes {
		clean := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
		if clean == "" {
			diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "security.allowed_file_types", Message: "must not contain empty values"})
			continue
		}
		if _, err := strconv.Unquote("\"" + clean + "\""); err != nil {
			diags = append(diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: "security.allowed_file_types", Message: fmt.Sprintf("invalid value at index %d", i)})
		}
	}
	return diags
}

func taskCanResolveMultipleURLs(task *DownloadTask) bool {
	if strings.TrimSpace(task.Selector) != "" {
		return true
	}
	if task.URLGenRange != nil || task.URLGenList != nil || task.URLGenFile != nil || task.URLGenDates != nil {
		return true
	}
	return false
}

func looksLikeTemplate(v string) bool {
	return strings.Contains(v, "{{") && strings.Contains(v, "}}")
}

func looksLikeEnvRef(v string) bool {
	trimmed := strings.TrimSpace(v)
	return strings.Contains(trimmed, "$")
}
