package config

import "testing"

func TestLoadBytes_ValidMinimalConfig(t *testing.T) {
	data := []byte(`
config:
  concurrency: 5
downloads:
  one:
    url: https://example.com/file.zip
`)

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

	_, _, err := LoadBytes(data)
	if err == nil {
		t.Fatal("expected validation error")
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

	_, _, err := LoadBytes(data)
	if err == nil {
		t.Fatal("expected validation error")
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

	_, _, err := LoadBytes(data)
	if err == nil {
		t.Fatal("expected validation error")
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

	_, _, err := LoadBytes(data)
	if err == nil {
		t.Fatal("expected validation error")
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

	_, _, err := LoadBytes(data)
	if err == nil {
		t.Fatal("expected validation error")
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

func TestLoadBytes_TaskForceDownloadFalseOverridesGlobalTrue(t *testing.T) {
	data := []byte(`
config:
  force_download: true
downloads:
  one:
    url: https://example.com/file.zip
    force_download: false
`)

	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Downloads["one"].ForceDownload {
		t.Fatal("expected task force_download=false to override global config force_download=true")
	}
}

func TestLoadBytes_TaskSecurityFalseOverridesGlobalTrue(t *testing.T) {
	data := []byte(`
config:
  security:
    allow_insecure_http: true
downloads:
  one:
    url: http://example.com/file.zip
    security:
      allow_insecure_http: false
`)

	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Downloads["one"].Security.AllowInsecureHTTP {
		t.Fatal("expected task security.allow_insecure_http=false to override global true")
	}
}

func TestLoadBytes_TaskStopOnCodeZeroOverridesGlobal(t *testing.T) {
	data := []byte(`
config:
  stop_on_code: 404
downloads:
  one:
    url: https://example.com/file.zip
    stop_on_code: 0
`)

	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Downloads["one"].StopOnCode != 0 {
		t.Fatalf("expected stop_on_code=0 override, got %d", cfg.Downloads["one"].StopOnCode)
	}
}

func TestLoadBytes_TaskRetryFalseOverridesGlobalTrue(t *testing.T) {
	data := []byte(`
config:
  retry: true
downloads:
  one:
    url: https://example.com/file.zip
    retry: false
`)

	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Downloads["one"].Retry.Enabled {
		t.Fatal("expected task retry=false to override global retry=true")
	}
}

func TestLoadBytes_TaskOverrideIfExistsFalseOverridesGlobalTrue(t *testing.T) {
	data := []byte(`
config:
  save:
    override_if_exists: true
downloads:
  one:
    url: https://example.com/file.zip
    save:
      override_if_exists: false
`)

	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Downloads["one"].Save.OverrideIfExists {
		t.Fatal("expected task save.override_if_exists=false to override global true")
	}
}

func TestLoadBytes_TaskStopOnErrorFalseOverridesGlobalTrue(t *testing.T) {
	data := []byte(`
config:
  stop_on_error: true
downloads:
  one:
    url: https://example.com/file.zip
    stop_on_error: false
`)

	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Downloads["one"].StopOnError {
		t.Fatal("expected task stop_on_error=false to override global true")
	}
}

func TestLoadBytes_TaskDownloadLimitZeroOverridesGlobal(t *testing.T) {
	data := []byte(`
config:
  download_limit: 5
downloads:
  one:
    url: https://example.com/file.zip
    download_limit: 0
`)

	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Downloads["one"].DownloadLimit != 0 {
		t.Fatalf("expected download_limit=0 override, got %d", cfg.Downloads["one"].DownloadLimit)
	}
}

func TestLoadBytes_TaskFollowRedirectsFalseOverridesGlobalTrue(t *testing.T) {
	data := []byte(`
config:
  security:
    follow_redirects: true
downloads:
  one:
    url: https://example.com/file.zip
    security:
      follow_redirects: false
`)

	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Downloads["one"].Security.FollowRedirects == nil {
		t.Fatal("expected task security.follow_redirects to be set")
	}
	if *cfg.Downloads["one"].Security.FollowRedirects {
		t.Fatal("expected task security.follow_redirects=false to override global true")
	}
}

func TestLoadBytes_TaskSaveMetadataFalseOverridesGlobalTrue(t *testing.T) {
	data := []byte(`
config:
  save:
    save_metadata: true
downloads:
  one:
    url: https://example.com/file.zip
    save:
      save_metadata: false
`)

	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Downloads["one"].Save.SaveMetadata {
		t.Fatal("expected task save.save_metadata=false to override global true")
	}
}
