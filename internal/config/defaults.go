package config

import "strings"

func defaultRetryConfig() RetryConfig {
	backoff := true
	return RetryConfig{
		OnError:            true,
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

	for _, task := range cfg.Downloads {
		if task == nil {
			continue
		}
		if task.Env == nil {
			task.Env = map[string]string{}
		}

		if strings.TrimSpace(task.Save.BaseDownloadDir) == "" {
			task.Save.BaseDownloadDir = cfg.Config.Save.BaseDownloadDir
		}
		if strings.TrimSpace(task.Save.FilenameTemplate) == "" {
			task.Save.FilenameTemplate = cfg.Config.Save.FilenameTemplate
		}
		if !task.Save.OverrideIfExists {
			task.Save.OverrideIfExists = cfg.Config.Save.OverrideIfExists
		}

		if strings.TrimSpace(task.BaseDownloadDir) != "" && strings.TrimSpace(task.Save.BaseDownloadDir) == "" {
			task.Save.BaseDownloadDir = task.BaseDownloadDir
		}

		if task.MaxDepth <= 0 {
			task.MaxDepth = DefaultMaxDepth
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
