package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFile_EnvPrecedenceAndExpansion(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, ".env.local"), []byte("BASE_DOWNLOAD_DIR=./from-local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("BASE_DOWNLOAD_DIR=./from-base\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	yml := `
env_file:
  - .env.local
  - .env
env:
  BASE_DOWNLOAD_DIR: ./from-global
config:
  save:
    base_download_dir: $BASE_DOWNLOAD_DIR
downloads:
  d1:
    url: https://example.com/file.zip
    env:
      BASE_DOWNLOAD_DIR: ./from-task
    save:
      base_download_dir: $BASE_DOWNLOAD_DIR
`
	cfgPath := filepath.Join(dir, "download.yaml")
	if err := os.WriteFile(cfgPath, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, diags, err := LoadFile(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v (diags=%v)", err, diags)
	}

	want := filepath.Join(dir, "from-task")
	if got := cfg.Downloads["d1"].Save.BaseDownloadDir; got != want {
		t.Fatalf("expected task save.base_download_dir=%s, got %s", want, got)
	}
}

func TestLoadFile_MissingEnvVariableFails(t *testing.T) {
	dir := t.TempDir()
	yml := `
downloads:
  d1:
    url: $MISSING_URL
`
	cfgPath := filepath.Join(dir, "download.yaml")
	if err := os.WriteFile(cfgPath, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := LoadFile(cfgPath)
	if err == nil {
		t.Fatal("expected validation error")
	}
}
