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
	expandField("config.save.filename", &cfg.Config.Save.Filename, globalVars, "", &diags)
	expandField("config.save.extension", &cfg.Config.Save.Extension, globalVars, "", &diags)
	expandField("config.selector", &cfg.Config.Selector, globalVars, "", &diags)
	expandField("config.attr", &cfg.Config.Attr, globalVars, "", &diags)
	expandField("config.regex_filter", &cfg.Config.RegexFilter, globalVars, "", &diags)
	expandField("config.attr_val_regex_filter", &cfg.Config.AttrValRegexFilter, globalVars, "", &diags)
	expandField("config.base_download_dir", &cfg.Config.BaseDownloadDir, globalVars, "", &diags)
	if cfg.Config.URLGenFile != nil {
		expandField("config.url_gen_file.file", &cfg.Config.URLGenFile.File, globalVars, "", &diags)
	}
	if cfg.Config.URLGenDates != nil {
		expandField("config.url_gen_dates.date_range_start", &cfg.Config.URLGenDates.DateRangeStart, globalVars, "", &diags)
		expandField("config.url_gen_dates.date_range_end", &cfg.Config.URLGenDates.DateRangeEnd, globalVars, "", &diags)
		expandField("config.url_gen_dates.date_format", &cfg.Config.URLGenDates.DateFormat, globalVars, "", &diags)
	}
	if cfg.Config.Auth != nil {
		expandField("config.auth.username", &cfg.Config.Auth.Username, globalVars, "", &diags)
		expandField("config.auth.password", &cfg.Config.Auth.Password, globalVars, "", &diags)
		expandField("config.auth.token", &cfg.Config.Auth.Token, globalVars, "", &diags)
		expandField("config.auth.key_name", &cfg.Config.Auth.KeyName, globalVars, "", &diags)
		expandField("config.auth.key_value", &cfg.Config.Auth.KeyValue, globalVars, "", &diags)
	}
	expandHookCommands("config.hooks", &cfg.Config.Hooks, globalVars, "", &diags)

	for name, task := range cfg.Downloads {
		if task == nil {
			continue
		}

		taskVars := gdEnv.MergeWithPrecedence(globalVars, cfg.Config.Env, task.Env)
		task.HookConfigDir = configDir
		task.HookEnv = copyStringMap(taskVars)
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
		expandHookCommands("hooks", &task.Hooks, taskVars, name, &diags)

		if task.URLGenFile != nil && task.URLGenFile.File != "" {
			task.URLGenFile.File = toAbsoluteOrURL(task.URLGenFile.File, configDir)
		}
	}

	cfg.Config.Save.BaseDownloadDir = toAbsoluteOrURL(cfg.Config.Save.BaseDownloadDir, configDir)
	if cfg.Config.BaseDownloadDir != "" {
		cfg.Config.BaseDownloadDir = toAbsoluteOrURL(cfg.Config.BaseDownloadDir, configDir)
	}
	if cfg.Config.URLGenFile != nil && cfg.Config.URLGenFile.File != "" {
		cfg.Config.URLGenFile.File = toAbsoluteOrURL(cfg.Config.URLGenFile.File, configDir)
	}
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

func expandHookCommands(prefix string, hooks *HooksConfig, vars map[string]string, taskName string, diags *[]Diagnostic) {
	if hooks.OnComplete != nil {
		expandField(prefix+".on_complete.command", &hooks.OnComplete.Command, vars, taskName, diags)
	}
	if hooks.OnSuccess != nil {
		expandField(prefix+".on_success.command", &hooks.OnSuccess.Command, vars, taskName, diags)
	}
	if hooks.OnFailure != nil {
		expandField(prefix+".on_failure.command", &hooks.OnFailure.Command, vars, taskName, diags)
	}
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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
