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
