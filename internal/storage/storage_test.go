package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/douglasmiranda/godownload/internal/config"
)

func TestSave_WritesFileAndMetadata(t *testing.T) {
	dir := t.TempDir()
	task := &config.DownloadTask{
		URL: "https://example.com/a.pdf",
		Save: config.SaveConfig{
			BaseDownloadDir: dir,
			SaveMetadata:    true,
		},
	}
	result, err := Save("task1", task, "https://example.com/a.pdf", []byte("hello"), "application/pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Skipped {
		t.Fatal("expected file to be saved")
	}
	if _, err := os.Stat(result.FilePath); err != nil {
		t.Fatalf("expected file to exist: %v", err)
	}
	if _, err := os.Stat(result.MetadataPath); err != nil {
		t.Fatalf("expected metadata to exist: %v", err)
	}
}

func TestSave_SkipWhenExistsAndNotForced(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.pdf")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := &config.DownloadTask{
		URL: "https://example.com/a.pdf",
		Save: config.SaveConfig{BaseDownloadDir: dir},
	}
	result, err := Save("task1", task, "https://example.com/a.pdf", []byte("new"), "application/pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Skipped {
		t.Fatal("expected save to be skipped")
	}
}

func TestSave_OverrideWhenForcedAndAllowed(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.pdf")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := &config.DownloadTask{
		URL:           "https://example.com/a.pdf",
		ForceDownload: true,
		Save: config.SaveConfig{
			BaseDownloadDir: dir,
			OverrideIfExists: true,
		},
	}
	_, err := Save("task1", task, "https://example.com/a.pdf", []byte("new"), "application/pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "new" {
		t.Fatalf("expected override with new content, got %q", string(b))
	}
}
