package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBytes_ValidMinimalConfig(t *testing.T) {
	data := []byte("downloads:\n  one:\n    url: https://example.com/file.zip\n")

	cfg, diags, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config")
	}
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %d", len(diags))
	}
}

func TestLoadBytes_DuplicateTaskNameShowsFriendlyError(t *testing.T) {
	data := []byte(`downloads:
  pdfs:
    url: https://example.com/first.pdf
  pdfs:
    url: https://example.com/second.pdf
`)

	_, _, err := LoadBytes(data)
	if err == nil {
		t.Fatal("expected duplicate task name error")
	}

	message := err.Error()
	if !strings.Contains(message, `task "pdfs" already defined at line 2`) {
		t.Fatalf("expected friendly duplicate task error, got %q", message)
	}
	if strings.Contains(message, "mapping key") {
		t.Fatalf("expected yaml mapping terminology to be hidden, got %q", message)
	}
}

func TestLoadBytes_MutuallyExclusiveURLGenerators(t *testing.T) {
	data := []byte(`
downloads:
  bad:
    url: https://example.com/file_{{.N}}.zip
    url_gen_range:
      var_name: N
      range_start: 1
      range_end: 2
    url_gen_list:
      var_name: N
      values: ["1", "2"]
`)

	_, diags, err := LoadBytes(data)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !hasError(diags, "bad", "url_gen_*") {
		t.Fatalf("expected url_gen_* validation error, got %#v", diags)
	}
}

func TestLoadBytes_URLGenVarNameMustMatchURLTemplate(t *testing.T) {
	data := []byte(`
downloads:
  bad:
    url: https://httpbin.org/stream/{{.N}}
    url_gen_range:
      var_name: X
      range_start: 1
      range_end: 1
      step: 1
`)

	_, diags, err := LoadBytes(data)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !hasError(diags, "bad", "url_gen_range.var_name") {
		t.Fatalf("expected url_gen_range.var_name validation error, got %#v", diags)
	}
	if !errorContains(diags, "bad", "url_gen_range.var_name", "{{.X}}") {
		t.Fatalf("expected clear template mismatch message, got %#v", diags)
	}
}

func TestLoadBytes_FilenameWithSelectorFails(t *testing.T) {
	data := []byte(`
downloads:
  bad:
    url: https://example.com/
    selector: a
    save:
      filename: fixed.zip
`)

	_, diags, err := LoadBytes(data)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !hasError(diags, "bad", "save.filename") {
		t.Fatalf("expected save.filename validation error, got %#v", diags)
	}
}

func TestLoadBytes_InvalidRegexFails(t *testing.T) {
	data := []byte(`
downloads:
  bad:
    url: https://example.com/
    selector: a
    regex_filter: "["
`)

	_, diags, err := LoadBytes(data)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !hasError(diags, "bad", "regex_filter") {
		t.Fatalf("expected regex_filter validation error, got %#v", diags)
	}
}

func TestLoadBytes_DateFormatValidation(t *testing.T) {
	data := []byte(`
downloads:
  bad:
    url: https://example.com/file_{{.DATE}}.zip
    url_gen_dates:
      var_name: DATE
      date_range_start: "2022-01-01"
      date_range_end: "2022-01-31"
      date_format: "BAD"
`)

	_, diags, err := LoadBytes(data)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !hasError(diags, "bad", "url_gen_dates.date_format") {
		t.Fatalf("expected url_gen_dates.date_format validation error, got %#v", diags)
	}
}

func TestLoadBytes_DefaultMaxDepthApplied(t *testing.T) {
	data := []byte(`
downloads:
  one:
    url: https://example.com/
    selector: a
`)

	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got := cfg.Downloads["one"].MaxDepth; got != DefaultMaxDepth {
		t.Fatalf("expected max_depth=%d, got %d", DefaultMaxDepth, got)
	}
}

func TestLoadBytes_AuthValidation(t *testing.T) {
	data := []byte(`
downloads:
  bad:
    url: https://example.com/file.zip
    auth:
      type: api_key
      key_name: X-API-Key
      key_in: header
`)

	_, diags, err := LoadBytes(data)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !hasError(diags, "bad", "auth.key_value") {
		t.Fatalf("expected auth.key_value validation error, got %#v", diags)
	}
}

func TestLoadBytes_ValidationDiagnostics(t *testing.T) {
	cases := []struct {
		name     string
		yaml     string
		taskName string
		field    string
		severity Severity
	}{
		{
			name: "config_task_concurrency",
			yaml: `
config:
  task_concurrency: 0
downloads:
  one:
    url: https://example.com/file.zip
`,
			field:    "config.task_concurrency",
			severity: SeverityError,
		},
		{
			name: "task_timeout",
			yaml: `
downloads:
  one:
    url: https://example.com/file.zip
    timeout: -1
`,
			taskName: "one",
			field:    "timeout",
			severity: SeverityError,
		},
		{
			name: "download_limit",
			yaml: `
downloads:
  one:
    url: https://example.com/file.zip
    download_limit: -1
`,
			taskName: "one",
			field:    "download_limit",
			severity: SeverityError,
		},
		{
			name: "selector_matches_limit",
			yaml: `
downloads:
  one:
    url: https://example.com/
    selector: a
    selector_matches_limit: -1
`,
			taskName: "one",
			field:    "selector_matches_limit",
			severity: SeverityError,
		},
		{
			name: "max_depth",
			yaml: `
downloads:
  one:
    url: https://example.com/
    selector: a
    max_depth: 0
`,
			taskName: "one",
			field:    "max_depth",
			severity: SeverityError,
		},
		{
			name: "max_depth_without_selector",
			yaml: `
downloads:
  one:
    url: https://example.com/file.zip
    max_depth: 2
`,
			taskName: "one",
			field:    "max_depth",
			severity: SeverityWarning,
		},
		{
			name: "empty_allowed_domain",
			yaml: `
downloads:
  one:
    url: https://example.com/file.zip
    security:
      allowed_domains: [""]
`,
			taskName: "one",
			field:    "security.allowed_domains",
			severity: SeverityError,
		},
		{
			name: "empty_allowed_file_type",
			yaml: `
downloads:
  one:
    url: https://example.com/file.zip
    security:
      allowed_file_types: [""]
`,
			taskName: "one",
			field:    "security.allowed_file_types",
			severity: SeverityError,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, diags, _ := LoadBytes([]byte(c.yaml))
			if !hasDiagnostic(diags, c.severity, c.taskName, c.field) {
				t.Fatalf("expected %s diagnostic for %q, got %#v", c.severity, c.field, diags)
			}
		})
	}
}

func TestLoadBytes_GlobalSecurityInheritedByTask(t *testing.T) {
	data := []byte(`
config:
  security:
    allow_insecure_http: true
downloads:
  one:
    url: http://example.com/file.zip
`)

	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !cfg.Downloads["one"].Security.AllowInsecureHTTP {
		t.Fatal("expected task.security.allow_insecure_http to inherit true from config.security")
	}
}

func TestLoadBytes_GlobalTaskFieldsInherited(t *testing.T) {
	data := []byte(`
config:
  selector: a.download
  attr: href
  force_download: true
  timeout: 25
downloads:
  one:
    url: https://example.com/
`)

	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	task := cfg.Downloads["one"]
	if task.Selector != "a.download" {
		t.Fatalf("expected selector inheritance, got %q", task.Selector)
	}
	if task.Attr != "href" {
		t.Fatalf("expected attr inheritance, got %q", task.Attr)
	}
	if !task.ForceDownload {
		t.Fatal("expected force_download inheritance")
	}
	if task.Timeout != 25 {
		t.Fatalf("expected timeout inheritance 25, got %d", task.Timeout)
	}
}

func TestLoadBytes_GlobalTreatWarningsAsErrorsInherited(t *testing.T) {
	data := []byte(`
config:
  treat_warnings_as_errors: true
downloads:
  one:
    url: https://example.com/file.zip
`)

	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !cfg.Downloads["one"].TreatWarningsAsErrors {
		t.Fatal("expected task treat_warnings_as_errors to inherit true from config")
	}
}

func TestLoadBytes_TaskOverridesGlobal(t *testing.T) {
	cases := []struct {
		name  string
		yaml  string
		check func(t *testing.T, task *DownloadTask)
	}{
		{
			name: "force_download_false",
			yaml: `
config:
  force_download: true
downloads:
  one:
    url: https://example.com/file.zip
    force_download: false
`,
			check: func(t *testing.T, task *DownloadTask) {
				if task.ForceDownload {
					t.Fatal("expected task force_download=false to override global true")
				}
			},
		},
		{
			name: "treat_warnings_as_errors_false",
			yaml: `
config:
  treat_warnings_as_errors: true
downloads:
  one:
    url: https://example.com/file.zip
    treat_warnings_as_errors: false
`,
			check: func(t *testing.T, task *DownloadTask) {
				if task.TreatWarningsAsErrors {
					t.Fatal("expected task treat_warnings_as_errors=false to override global true")
				}
			},
		},
		{
			name: "allow_insecure_http_false",
			yaml: `
config:
  security:
    allow_insecure_http: true
downloads:
  one:
    url: http://example.com/file.zip
    security:
      allow_insecure_http: false
`,
			check: func(t *testing.T, task *DownloadTask) {
				if task.Security.AllowInsecureHTTP {
					t.Fatal("expected task security.allow_insecure_http=false to override global true")
				}
			},
		},
		{
			name: "stop_all_on_http_code_empty",
			yaml: `
config:
  stop_all_tasks:
    on_http_code: [404]
downloads:
  one:
    url: https://example.com/file.zip
    stop_all_tasks:
      on_http_code: []
`,
			check: func(t *testing.T, task *DownloadTask) {
				if len(task.StopAllTasks.OnHTTPCode) != 0 {
					t.Fatalf("expected stop_all_tasks.on_http_code override to empty, got %#v", task.StopAllTasks.OnHTTPCode)
				}
			},
		},
		{
			name: "retry_false",
			yaml: `
config:
  retry: true
downloads:
  one:
    url: https://example.com/file.zip
    retry: false
`,
			check: func(t *testing.T, task *DownloadTask) {
				if task.Retry.Enabled {
					t.Fatal("expected task retry=false to override global true")
				}
			},
		},
		{
			name: "override_if_exists_false",
			yaml: `
config:
  save:
    override_if_exists: true
downloads:
  one:
    url: https://example.com/file.zip
    save:
      override_if_exists: false
`,
			check: func(t *testing.T, task *DownloadTask) {
				if task.Save.OverrideIfExists {
					t.Fatal("expected task save.override_if_exists=false to override global true")
				}
			},
		},
		{
			name: "stop_all_on_runtime_error_false",
			yaml: `
config:
  stop_all_tasks:
    on_runtime_error: true
downloads:
  one:
    url: https://example.com/file.zip
    stop_all_tasks:
      on_runtime_error: false
`,
			check: func(t *testing.T, task *DownloadTask) {
				if task.StopAllTasks.OnRuntimeError {
					t.Fatal("expected task stop_all_tasks.on_runtime_error=false to override global true")
				}
			},
		},
		{
			name: "download_limit_zero",
			yaml: `
config:
  download_limit: 5
downloads:
  one:
    url: https://example.com/file.zip
    download_limit: 0
`,
			check: func(t *testing.T, task *DownloadTask) {
				if task.DownloadLimit != 0 {
					t.Fatalf("expected download_limit=0 override, got %d", task.DownloadLimit)
				}
			},
		},
		{
			name: "follow_redirects_false",
			yaml: `
config:
  security:
    follow_redirects: true
downloads:
  one:
    url: https://example.com/file.zip
    security:
      follow_redirects: false
`,
			check: func(t *testing.T, task *DownloadTask) {
				if task.Security.FollowRedirects == nil {
					t.Fatal("expected task security.follow_redirects to be set")
				}
				if *task.Security.FollowRedirects {
					t.Fatal("expected task security.follow_redirects=false to override global true")
				}
			},
		},
		{
			name: "save_metadata_false",
			yaml: `
config:
  save:
    save_metadata: true
downloads:
  one:
    url: https://example.com/file.zip
    save:
      save_metadata: false
`,
			check: func(t *testing.T, task *DownloadTask) {
				if task.Save.SaveMetadata {
					t.Fatal("expected task save.save_metadata=false to override global true")
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, _, err := LoadBytes([]byte(c.yaml))
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			c.check(t, cfg.Downloads["one"])
		})
	}
}

func TestLoadBytes_NoWarningWhenFilenameTemplateOnlyFromDefault(t *testing.T) {
	data := []byte(`
downloads:
  one:
    url: https://example.com/file.zip
    save:
      filename: renamed.zip
`)

	_, diags, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if hasWarning(diags, "one", "save.filename_template") {
		t.Fatal("did not expect warning when filename_template was not explicitly set in YAML")
	}
}

func TestLoadBytes_WarningWhenGlobalFilenameTemplateExplicitlySet(t *testing.T) {
	data := []byte(`
config:
  save:
    filename_template: "{{.ORIGINAL_FILENAME}}"
downloads:
  one:
    url: https://example.com/file.zip
    save:
      filename: renamed.zip
`)

	_, diags, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !hasWarning(diags, "one", "save.filename_template") {
		t.Fatal("expected warning when global save.filename_template is explicitly set")
	}
}

func TestLoadBytes_WarningWhenTaskFilenameTemplateExplicitlySet(t *testing.T) {
	data := []byte(`
downloads:
  one:
    url: https://example.com/file.zip
    save:
      filename: renamed.zip
      filename_template: "{{.ORIGINAL_FILENAME}}"
`)

	_, diags, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !hasWarning(diags, "one", "save.filename_template") {
		t.Fatal("expected warning when task save.filename_template is explicitly set")
	}
}

func TestLoadBytes_HooksGlobalDefaultsAndTaskOverrides(t *testing.T) {
	data := []byte(`
config:
  hooks:
    fail_task_if_error: true
    timeout: 9
    on_success:
      command: echo global-success
    on_failure:
      command: echo global-failure
    on_complete:
      command: echo global-complete
downloads:
  one:
    url: https://example.com/file.zip
    hooks:
      timeout: 2
      on_success:
        command: echo task-success
      on_failure: null
`)

	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	hooks := cfg.Downloads["one"].Hooks
	if !hooks.FailTaskIfError {
		t.Fatal("expected fail_task_if_error to inherit from global hooks")
	}
	if hooks.Timeout != 2 {
		t.Fatalf("expected task timeout override, got %d", hooks.Timeout)
	}
	if hooks.OnSuccess == nil || hooks.OnSuccess.Command != "echo task-success" {
		t.Fatalf("expected task on_success override, got %#v", hooks.OnSuccess)
	}
	if hooks.OnFailure != nil {
		t.Fatalf("expected explicit null to disable inherited on_failure, got %#v", hooks.OnFailure)
	}
	if hooks.OnComplete == nil || hooks.OnComplete.Command != "echo global-complete" {
		t.Fatalf("expected inherited on_complete, got %#v", hooks.OnComplete)
	}
}

func TestLoadBytes_HooksValidationDiagnostics(t *testing.T) {
	data := []byte(`
downloads:
  one:
    url: https://example.com/file.zip
    hooks:
      timeout: -1
      on_success:
        command: ""
`)

	_, diags, _ := LoadBytes(data)
	if !hasError(diags, "one", "hooks.timeout") {
		t.Fatalf("expected hooks.timeout validation error, got %#v", diags)
	}
	if !hasError(diags, "one", "hooks.on_success.command") {
		t.Fatalf("expected hooks.on_success.command validation error, got %#v", diags)
	}
}

func TestLoadFile_HooksExpandEnvAndSetRuntimeContext(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "download.yaml")
	data := `
env:
  HOOK_MSG: from-root
config:
  env:
    SHELL: /bin/sh
downloads:
  one:
    url: https://example.com/file.zip
    env:
      HOOK_MSG: from-task
    hooks:
      on_complete:
        command: echo $HOOK_MSG
`
	if err := os.WriteFile(cfgPath, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := LoadFile(cfgPath)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	task := cfg.Downloads["one"]
	if task.Hooks.OnComplete == nil || task.Hooks.OnComplete.Command != "echo from-task" {
		t.Fatalf("expected expanded task hook command, got %#v", task.Hooks.OnComplete)
	}
	if task.HookConfigDir != dir {
		t.Fatalf("expected hook config dir %q, got %q", dir, task.HookConfigDir)
	}
	if task.HookEnv["HOOK_MSG"] != "from-task" {
		t.Fatalf("expected task env to win, got %#v", task.HookEnv)
	}
	if task.HookEnv["SHELL"] != "/bin/sh" {
		t.Fatalf("expected config env in hook env, got %#v", task.HookEnv)
	}
}

func TestLoadBytes_HooksExpandEnvAndSetRuntimeContext(t *testing.T) {
	data := []byte(`
env:
  HOOK_MSG: from-root
config:
  env:
    SHELL: /bin/sh
downloads:
  one:
    url: https://example.com/file.zip
    env:
      HOOK_MSG: from-task
    hooks:
      on_complete:
        command: echo $HOOK_MSG
`)

	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	task := cfg.Downloads["one"]
	if task.Hooks.OnComplete == nil || task.Hooks.OnComplete.Command != "echo from-task" {
		t.Fatalf("expected expanded task hook command, got %#v", task.Hooks.OnComplete)
	}
	if task.HookConfigDir != "." {
		t.Fatalf("expected hook config dir %q, got %q", ".", task.HookConfigDir)
	}
	if task.HookEnv["HOOK_MSG"] != "from-task" {
		t.Fatalf("expected task env to win, got %#v", task.HookEnv)
	}
	if task.HookEnv["SHELL"] != "/bin/sh" {
		t.Fatalf("expected config env in hook env, got %#v", task.HookEnv)
	}
}

func hasWarning(diags []Diagnostic, taskName string, field string) bool {
	for _, d := range diags {
		if d.Severity == SeverityWarning && d.TaskName == taskName && d.Field == field {
			return true
		}
	}
	return false
}

func hasError(diags []Diagnostic, taskName string, field string) bool {
	return hasDiagnostic(diags, SeverityError, taskName, field)
}

func hasDiagnostic(diags []Diagnostic, severity Severity, taskName string, field string) bool {
	for _, d := range diags {
		if d.Severity == severity && d.TaskName == taskName && d.Field == field {
			return true
		}
	}
	return false
}

func errorContains(diags []Diagnostic, taskName string, field string, substr string) bool {
	for _, d := range diags {
		if d.Severity == SeverityError && d.TaskName == taskName && d.Field == field {
			return strings.Contains(d.Message, substr)
		}
	}
	return false
}
