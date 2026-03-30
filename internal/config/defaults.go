package config

import "strings"

func defaultRetryConfig() RetryConfig {
	backoff := true
	return RetryConfig{
		OnRuntimeError:     true,
		Retries:            3,
		Delay:              1,
		ExponentialBackoff: &backoff,
	}
}

func applyDefaults(cfg *Config) {
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}

	if cfg.Config.Concurrency <= 0 {
		cfg.Config.Concurrency = DefaultConcurrency
	}
	if strings.TrimSpace(cfg.Config.LogLevel) == "" {
		cfg.Config.LogLevel = DefaultLogLevel
	}
	if cfg.Config.DefaultAttrMapping == nil {
		cfg.Config.DefaultAttrMapping = map[string]string{}
	}
	if len(cfg.Config.DefaultAttrMapping) == 0 {
		cfg.Config.DefaultAttrMapping["a"] = "href"
		cfg.Config.DefaultAttrMapping["img"] = "src"
		cfg.Config.DefaultAttrMapping["link"] = "href"
		cfg.Config.DefaultAttrMapping["script"] = "src"
		cfg.Config.DefaultAttrMapping["iframe"] = "src"
	}
	if strings.TrimSpace(cfg.Config.Save.FilenameTemplate) == "" {
		cfg.Config.Save.FilenameTemplate = DefaultFilenameTemplate
	}
	if cfg.Config.Env == nil {
		cfg.Config.Env = map[string]string{}
	}

	for _, task := range cfg.Downloads {
		if task == nil {
			continue
		}
		if task.Env == nil {
			task.Env = map[string]string{}
		}
		for k, v := range cfg.Config.Env {
			if _, exists := task.Env[k]; !exists {
				task.Env[k] = v
			}
		}

		if strings.TrimSpace(task.Selector) == "" {
			task.Selector = cfg.Config.Selector
		}
		if strings.TrimSpace(task.Attr) == "" {
			task.Attr = cfg.Config.Attr
		}
		if strings.TrimSpace(task.RegexFilter) == "" {
			task.RegexFilter = cfg.Config.RegexFilter
		}
		if strings.TrimSpace(task.AttrValRegexFilter) == "" {
			task.AttrValRegexFilter = cfg.Config.AttrValRegexFilter
		}
		if task.URLGenRange == nil {
			task.URLGenRange = cloneURLGenRange(cfg.Config.URLGenRange)
		}
		if task.URLGenList == nil {
			task.URLGenList = cloneURLGenList(cfg.Config.URLGenList)
		}
		if task.URLGenFile == nil {
			task.URLGenFile = cloneURLGenFile(cfg.Config.URLGenFile)
		}
		if task.URLGenDates == nil {
			task.URLGenDates = cloneURLGenDates(cfg.Config.URLGenDates)
		}
		if !task.hasField("force_download") {
			task.ForceDownload = cfg.Config.ForceDownload
		}
		if strings.TrimSpace(task.BaseDownloadDir) == "" {
			task.BaseDownloadDir = cfg.Config.BaseDownloadDir
		}
		if !task.hasField("stop_all_tasks.on_runtime_error") {
			task.StopAllTasks.OnRuntimeError = cfg.Config.StopAllTasks.OnRuntimeError
		}
		if !task.hasField("stop_all_tasks.on_http_code") && len(task.StopAllTasks.OnHTTPCode) == 0 && len(cfg.Config.StopAllTasks.OnHTTPCode) > 0 {
			task.StopAllTasks.OnHTTPCode = append([]int{}, cfg.Config.StopAllTasks.OnHTTPCode...)
		}
		if !task.hasField("timeout") {
			task.Timeout = cfg.Config.Timeout
		}
		if !task.hasField("download_limit") {
			task.DownloadLimit = cfg.Config.DownloadLimit
		}
		if !task.hasField("selector_matches_limit") {
			task.SelectorMatchesLimit = cfg.Config.SelectorMatchesLimit
		}
		if !task.hasField("retry") {
			task.Retry = cloneRetry(cfg.Config.Retry)
		} else {
			task.Retry = mergeRetryDefaults(task, cfg.Config.Retry)
		}
		if !task.hasField("auth") {
			task.Auth = cloneAuth(cfg.Config.Auth)
		} else {
			task.Auth = mergeAuthDefaults(task, cfg.Config.Auth)
		}
		task.Security = mergeSecurityDefaults(task, cfg.Config.Security)

		if strings.TrimSpace(task.Save.BaseDownloadDir) == "" {
			task.Save.BaseDownloadDir = cfg.Config.Save.BaseDownloadDir
		}
		if strings.TrimSpace(task.Save.FilenameTemplate) == "" {
			task.Save.FilenameTemplate = cfg.Config.Save.FilenameTemplate
		}
		if strings.TrimSpace(task.Save.Filename) == "" {
			task.Save.Filename = cfg.Config.Save.Filename
		}
		if strings.TrimSpace(task.Save.Extension) == "" {
			task.Save.Extension = cfg.Config.Save.Extension
		}
		if !task.hasField("save.override_if_exists") {
			task.Save.OverrideIfExists = cfg.Config.Save.OverrideIfExists
		}
		if !task.hasField("save.save_metadata") {
			task.Save.SaveMetadata = cfg.Config.Save.SaveMetadata
		}

		if strings.TrimSpace(task.BaseDownloadDir) != "" && strings.TrimSpace(task.Save.BaseDownloadDir) == "" {
			task.Save.BaseDownloadDir = task.BaseDownloadDir
		}

		if !task.hasField("max_depth") && task.MaxDepth <= 0 {
			if cfg.Config.MaxDepth > 0 {
				task.MaxDepth = cfg.Config.MaxDepth
			} else {
				task.MaxDepth = DefaultMaxDepth
			}
		}

		if task.Retry.Enabled {
			if task.Retry.Config.Retries == 0 {
				task.Retry.Config.Retries = 3
			}
			if task.Retry.Config.Delay == 0 {
				task.Retry.Config.Delay = 1
			}
			if task.Retry.Config.ExponentialBackoff == nil {
				backoff := true
				task.Retry.Config.ExponentialBackoff = &backoff
			}
		}

		if task.Auth != nil && strings.TrimSpace(task.Auth.KeyIn) == "" {
			task.Auth.KeyIn = "header"
		}
	}
}

func cloneURLGenRange(in *URLGenRange) *URLGenRange {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneURLGenList(in *URLGenList) *URLGenList {
	if in == nil {
		return nil
	}
	out := *in
	if in.Values != nil {
		out.Values = append([]string{}, in.Values...)
	}
	return &out
}

func cloneURLGenFile(in *URLGenFile) *URLGenFile {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneURLGenDates(in *URLGenDates) *URLGenDates {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneRetry(in Retry) Retry {
	out := in
	out.Config.OnHTTPCode = append([]int{}, in.Config.OnHTTPCode...)
	if in.Config.ExponentialBackoff != nil {
		v := *in.Config.ExponentialBackoff
		out.Config.ExponentialBackoff = &v
	}
	return out
}

func cloneAuth(in *AuthConfig) *AuthConfig {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func mergeSecurityDefaults(task *DownloadTask, defaults SecurityConfig) SecurityConfig {
	out := task.Security
	if !task.hasField("security.follow_redirects") && out.FollowRedirects == nil && defaults.FollowRedirects != nil {
		v := *defaults.FollowRedirects
		out.FollowRedirects = &v
	}
	if !task.hasField("security.allow_insecure_http") {
		out.AllowInsecureHTTP = defaults.AllowInsecureHTTP
	}
	if !task.hasField("security.allow_self_signed_cert") {
		out.AllowSelfSigned = defaults.AllowSelfSigned
	}
	if !task.hasField("security.allowed_domains") && len(defaults.AllowedDomains) > 0 {
		out.AllowedDomains = append([]string{}, defaults.AllowedDomains...)
	}
	if !task.hasField("security.allowed_file_types") && len(defaults.AllowedFileTypes) > 0 {
		out.AllowedFileTypes = append([]string{}, defaults.AllowedFileTypes...)
	}
	return out
}

func mergeRetryDefaults(task *DownloadTask, defaults Retry) Retry {
	out := task.Retry
	if !out.Enabled || !defaults.Enabled {
		return out
	}

	if !task.hasField("retry.on_runtime_error") {
		out.Config.OnRuntimeError = defaults.Config.OnRuntimeError
	}
	if !task.hasField("retry.on_http_code") && len(out.Config.OnHTTPCode) == 0 && len(defaults.Config.OnHTTPCode) > 0 {
		out.Config.OnHTTPCode = append([]int{}, defaults.Config.OnHTTPCode...)
	}
	if !task.hasField("retry.retries") {
		out.Config.Retries = defaults.Config.Retries
	}
	if !task.hasField("retry.delay") {
		out.Config.Delay = defaults.Config.Delay
	}
	if !task.hasField("retry.exponential_backoff") && out.Config.ExponentialBackoff == nil && defaults.Config.ExponentialBackoff != nil {
		v := *defaults.Config.ExponentialBackoff
		out.Config.ExponentialBackoff = &v
	}

	return out
}

func mergeAuthDefaults(task *DownloadTask, defaults *AuthConfig) *AuthConfig {
	if task.Auth == nil || defaults == nil {
		return task.Auth
	}

	out := *task.Auth
	if !task.hasField("auth.type") && strings.TrimSpace(out.Type) == "" {
		out.Type = defaults.Type
	}
	if !task.hasField("auth.username") && strings.TrimSpace(out.Username) == "" {
		out.Username = defaults.Username
	}
	if !task.hasField("auth.password") && strings.TrimSpace(out.Password) == "" {
		out.Password = defaults.Password
	}
	if !task.hasField("auth.token") && strings.TrimSpace(out.Token) == "" {
		out.Token = defaults.Token
	}
	if !task.hasField("auth.key_name") && strings.TrimSpace(out.KeyName) == "" {
		out.KeyName = defaults.KeyName
	}
	if !task.hasField("auth.key_value") && strings.TrimSpace(out.KeyValue) == "" {
		out.KeyValue = defaults.KeyValue
	}
	if !task.hasField("auth.key_in") && strings.TrimSpace(out.KeyIn) == "" {
		out.KeyIn = defaults.KeyIn
	}

	return &out
}
