package config

import "testing"

func TestParseByteSize(t *testing.T) {
	cases := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"", 0, false},
		{"1024", 1024, false},
		{"500kb", 500 * 1024, false},
		{"10mb", 10 * 1024 * 1024, false},
		{"1gb", 1 * 1024 * 1024 * 1024, false},
		{"2 MB", 2 * 1024 * 1024, false},
		{"5b", 5, false},
		{"10xb", 0, true},
		{"abc", 0, true},
		{"-5", 0, true},
	}
	for _, c := range cases {
		got, err := parseByteSize(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseByteSize(%q): expected error, got %d", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseByteSize(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseByteSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestLoadBytes_SizeSuffixesParsed(t *testing.T) {
	data := []byte(`
downloads:
  d1:
    url: https://example.com/file.zip
    security:
      max_download_size: 10mb
      max_in_memory_size: 1gb
`)
	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sec := cfg.Downloads["d1"].Security
	if sec.MaxDownloadSize != 10*1024*1024 {
		t.Fatalf("expected max_download_size=10MiB, got %d", sec.MaxDownloadSize)
	}
	if sec.MaxInMemorySize != 1*1024*1024*1024 {
		t.Fatalf("expected max_in_memory_size=1GiB, got %d", sec.MaxInMemorySize)
	}
}

func TestLoadBytes_DefaultMaxInMemorySizeApplied(t *testing.T) {
	data := []byte(`
downloads:
  d1:
    url: https://example.com/file.zip
`)
	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.Downloads["d1"].Security.MaxInMemorySize; got != DefaultMaxInMemorySize {
		t.Fatalf("expected default max_in_memory_size=%d, got %d", DefaultMaxInMemorySize, got)
	}
}

func TestLoadBytes_MaxInMemorySizeZeroDisablesCap(t *testing.T) {
	data := []byte(`
downloads:
  d1:
    url: https://example.com/file.zip
    security:
      max_in_memory_size: 0
`)
	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.Downloads["d1"].Security.MaxInMemorySize; got != 0 {
		t.Fatalf("expected max_in_memory_size=0 (disabled), got %d", got)
	}
}

func TestLoadBytes_TaskMaxDownloadSizeOverridesGlobal(t *testing.T) {
	data := []byte(`
config:
  security:
    max_download_size: 29b
downloads:
  d1:
    url: https://example.com/file.zip
    security:
      max_download_size: 30b
`)
	cfg, _, err := LoadBytes(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := cfg.Downloads["d1"].Security.MaxDownloadSize; got != 30 {
		t.Fatalf("expected task max_download_size=30, got %d", got)
	}
}
