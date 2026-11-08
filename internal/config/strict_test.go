package config

import (
	"errors"
	"strings"
	"testing"
)

func TestLoadBytes_UnknownFields(t *testing.T) {
	cases := []struct {
		name      string
		yaml      string
		taskName  string
		field     string
		wantInMsg string
	}{
		{
			name: "typo in task key",
			yaml: `
downloads:
  d1:
    url: https://example.com/a.zip
    selecter: a
`,
			taskName:  "d1",
			field:     "selecter",
			wantInMsg: `did you mean "selector"?`,
		},
		{
			name: "typo in nested task key",
			yaml: `
downloads:
  d1:
    url: https://example.com/a.zip
    save:
      filname: a.zip
`,
			taskName:  "d1",
			field:     "save.filname",
			wantInMsg: `did you mean "filename"?`,
		},
		{
			name: "typo in global config key",
			yaml: `
config:
  task_concurency: 2
downloads:
  d1:
    url: https://example.com/a.zip
`,
			field:     "config.task_concurency",
			wantInMsg: `did you mean "task_concurrency"?`,
		},
		{
			name: "key placed one level too high",
			yaml: `
downloads:
  d1:
    url: https://example.com/a.zip
    allowed_domains: [example.com]
`,
			taskName:  "d1",
			field:     "allowed_domains",
			wantInMsg: `did you mean "security.allowed_domains"?`,
		},
		{
			name: "base_download_dir outside save",
			yaml: `
downloads:
  d1:
    url: https://example.com/a.zip
    base_download_dir: ./out
`,
			taskName:  "d1",
			field:     "base_download_dir",
			wantInMsg: `did you mean "save.base_download_dir"?`,
		},
		{
			name: "typo inside retry mapping",
			yaml: `
downloads:
  d1:
    url: https://example.com/a.zip
    retry:
      retires: 2
`,
			taskName:  "d1",
			field:     "retry.retires",
			wantInMsg: `did you mean "retries"?`,
		},
		{
			name: "typo inside hook command",
			yaml: `
downloads:
  d1:
    url: https://example.com/a.zip
    hooks:
      on_success:
        comand: echo ok
`,
			taskName:  "d1",
			field:     "hooks.on_success.comand",
			wantInMsg: `did you mean "command"?`,
		},
		{
			name: "unknown top-level key",
			yaml: `
download:
  d1:
    url: https://example.com/a.zip
`,
			field:     "download",
			wantInMsg: `did you mean "downloads"?`,
		},
		{
			name: "no suggestion for unrelated key",
			yaml: `
downloads:
  d1:
    url: https://example.com/a.zip
    zzzzzzzz: 1
`,
			taskName:  "d1",
			field:     "zzzzzzzz",
			wantInMsg: "unknown field",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, diags, err := LoadBytes([]byte(tc.yaml))
			var verr ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("expected validation error, got %v", err)
			}
			for _, d := range diags {
				if d.Severity == SeverityError && d.TaskName == tc.taskName && d.Field == tc.field {
					if !strings.Contains(d.Message, tc.wantInMsg) {
						t.Fatalf("message %q does not contain %q", d.Message, tc.wantInMsg)
					}
					return
				}
			}
			t.Fatalf("no error diagnostic for task=%q field=%q in %+v", tc.taskName, tc.field, diags)
		})
	}
}

func TestLoadBytes_KnownFieldsAccepted(t *testing.T) {
	data := []byte(`
config:
  log_level: debug
  retry:
    retries: 2
  security:
    allowed_domains: [example.com]
  save:
    base_download_dir: ./out
downloads:
  d1:
    url: https://example.com/a.zip
    env:
      ANY_NAME: value
    retry: true
    hooks:
      on_success:
        command: echo ok
`)
	_, diags, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("unexpected error: %v (%+v)", err, diags)
	}
	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %+v", diags)
	}
}

func TestLoadBytes_MergeKeysAreChecked(t *testing.T) {
	data := []byte(`
downloads:
  base: &base
    url: https://example.com/a.zip
  d1:
    <<: *base
    regex_filtr: a
`)
	_, diags, err := LoadBytes(data)
	if err == nil {
		t.Fatal("expected unknown field error")
	}
	if !hasError(diags, "d1", "regex_filtr") {
		t.Fatalf("expected regex_filtr diagnostic, got %+v", diags)
	}
}

func TestLoadBytes_MultipleDocumentsRejected(t *testing.T) {
	data := []byte(`
downloads:
  d1:
    url: https://example.com/a.zip
---
downloads:
  d2:
    url: https://example.com/b.zip
`)
	if _, _, err := LoadBytes(data); err == nil {
		t.Fatal("expected error for multiple YAML documents")
	}
}

func TestLoadBytes_RetryMappingStartsFromDefaults(t *testing.T) {
	data := []byte(`
downloads:
  partial:
    url: https://example.com/a.zip
    retry:
      on_http_code: [503]
  zero:
    url: https://example.com/b.zip
    retry:
      retries: 0
      delay: 0
`)
	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	partial := cfg.Downloads["partial"].Retry
	if !partial.Enabled || partial.Config.Retries != 3 || partial.Config.Delay != 1 {
		t.Fatalf("expected default retries/delay for partial mapping, got %+v", partial.Config)
	}
	if partial.Config.ExponentialBackoff == nil || !*partial.Config.ExponentialBackoff {
		t.Fatalf("expected exponential backoff default true, got %v", partial.Config.ExponentialBackoff)
	}

	zero := cfg.Downloads["zero"].Retry
	if !zero.Enabled || zero.Config.Retries != 0 || zero.Config.Delay != 0 {
		t.Fatalf("expected explicit zeros to be kept, got %+v", zero.Config)
	}
}

func TestLoadBytes_RetryRejectsSequence(t *testing.T) {
	data := []byte(`
downloads:
  d1:
    url: https://example.com/a.zip
    retry: [1, 2]
`)
	if _, _, err := LoadBytes(data); err == nil {
		t.Fatal("expected error for retry as a list")
	}
}

func TestLoadBytes_LogLevel(t *testing.T) {
	for _, level := range []string{"", "info", "DEBUG", " Debug "} {
		data := []byte("config:\n  log_level: \"" + level + "\"\ndownloads:\n  d1:\n    url: https://example.com/a.zip\n")
		cfg, _, err := LoadBytes(data)
		if err != nil {
			t.Fatalf("log_level %q: unexpected error: %v", level, err)
		}
		if cfg.Config.LogLevel != "INFO" && cfg.Config.LogLevel != "DEBUG" {
			t.Fatalf("log_level %q normalized to %q", level, cfg.Config.LogLevel)
		}
	}

	data := []byte("config:\n  log_level: verbose\ndownloads:\n  d1:\n    url: https://example.com/a.zip\n")
	_, diags, err := LoadBytes(data)
	if err == nil {
		t.Fatal("expected invalid log_level error")
	}
	if !hasError(diags, "", "config.log_level") {
		t.Fatalf("expected config.log_level diagnostic, got %+v", diags)
	}
}
