package config

import (
	"path/filepath"
	"strings"

	gdEnv "github.com/douglasmiranda/godownload/internal/env"
)

func resolveEnvAndExpand(cfg *Config, configDir string) ([]Diagnostic, error) {
	diags := []Diagnostic{}

	envFileVars, err := gdEnv.LoadEnvFiles(cfg.EnvFile, configDir)
	if err != nil {
		return nil, err
	}
	globalVars := gdEnv.MergeWithPrecedence(gdEnv.SystemEnvMap(), envFileVars, cfg.Env)

	for k, v := range cfg.Env {
		expanded, missing := gdEnv.ExpandString(v, globalVars)
		cfg.Env[k] = expanded
		for _, m := range missing {
			diags = append(diags, Diagnostic{Severity: SeverityError, Field: "env." + k, Message: "missing environment variable: " + m})
		}
	}

	expandField("config.save.base_download_dir", &cfg.Config.Save.BaseDownloadDir, globalVars, "", &diags)
	expandField("config.save.filename_template", &cfg.Config.Save.FilenameTemplate, globalVars, "", &diags)

	for name, task := range cfg.Downloads {
		if task == nil {
			continue
		}

		taskVars := gdEnv.MergeWithPrecedence(globalVars, task.Env)
		expandField("url", &task.URL, taskVars, name, &diags)
		expandField("selector", &task.Selector, taskVars, name, &diags)
		expandField("attr", &task.Attr, taskVars, name, &diags)
		expandField("regex_filter", &task.RegexFilter, taskVars, name, &diags)
		expandField("attr_val_regex_filter", &task.AttrValRegexFilter, taskVars, name, &diags)
		expandField("base_download_dir", &task.BaseDownloadDir, taskVars, name, &diags)
		expandField("save.base_download_dir", &task.Save.BaseDownloadDir, taskVars, name, &diags)
		expandField("save.filename_template", &task.Save.FilenameTemplate, taskVars, name, &diags)
		expandField("save.filename", &task.Save.Filename, taskVars, name, &diags)
		expandField("save.extension", &task.Save.Extension, taskVars, name, &diags)

		if task.URLGenFile != nil {
			expandField("url_gen_file.file", &task.URLGenFile.File, taskVars, name, &diags)
		}
		if task.URLGenDates != nil {
			expandField("url_gen_dates.date_range_start", &task.URLGenDates.DateRangeStart, taskVars, name, &diags)
			expandField("url_gen_dates.date_range_end", &task.URLGenDates.DateRangeEnd, taskVars, name, &diags)
			expandField("url_gen_dates.date_format", &task.URLGenDates.DateFormat, taskVars, name, &diags)
		}
		if task.Auth != nil {
			expandField("auth.username", &task.Auth.Username, taskVars, name, &diags)
			expandField("auth.password", &task.Auth.Password, taskVars, name, &diags)
			expandField("auth.token", &task.Auth.Token, taskVars, name, &diags)
			expandField("auth.key_name", &task.Auth.KeyName, taskVars, name, &diags)
			expandField("auth.key_value", &task.Auth.KeyValue, taskVars, name, &diags)
		}

		if task.URLGenFile != nil && task.URLGenFile.File != "" {
			task.URLGenFile.File = toAbsoluteOrURL(task.URLGenFile.File, configDir)
		}
	}

	cfg.Config.Save.BaseDownloadDir = toAbsoluteOrURL(cfg.Config.Save.BaseDownloadDir, configDir)
	for _, task := range cfg.Downloads {
		if task == nil {
			continue
		}
		task.Save.BaseDownloadDir = toAbsoluteOrURL(task.Save.BaseDownloadDir, configDir)
		if task.BaseDownloadDir != "" {
			task.BaseDownloadDir = toAbsoluteOrURL(task.BaseDownloadDir, configDir)
		}
	}

	return diags, nil
}

func toAbsoluteOrURL(v string, baseDir string) string {
	if v == "" {
		return v
	}
	if hasHTTPScheme(v) {
		return v
	}
	if filepath.IsAbs(v) {
		return v
	}
	return filepath.Clean(filepath.Join(baseDir, v))
}

func expandField(field string, target *string, vars map[string]string, taskName string, diags *[]Diagnostic) {
	if target == nil || *target == "" {
		return
	}
	expanded, missing := gdEnv.ExpandString(*target, vars)
	*target = expanded
	for _, m := range missing {
		*diags = append(*diags, Diagnostic{Severity: SeverityError, TaskName: taskName, Field: field, Message: "missing environment variable: " + m})
	}
}

func hasHTTPScheme(v string) bool {
	return strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://")
}
