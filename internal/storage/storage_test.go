package storage

import (
	"os"
	"path/filepath"
	"strings"
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
		URL:  "https://example.com/a.pdf",
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
			BaseDownloadDir:  dir,
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

func TestSave_TemplateKeepsOriginalExtensionAtEnd(t *testing.T) {
	dir := t.TempDir()
	task := &config.DownloadTask{
		URL: "https://example.com/file1.txt",
		Save: config.SaveConfig{
			BaseDownloadDir:  dir,
			FilenameTemplate: "downloaded_{{.ORIGINAL_FILENAME}}_{{.TIMESTAMP}}",
		},
	}

	res, err := Save("task1", task, "https://example.com/file1.txt", []byte("data"), "text/plain")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(res.SavedFileName, ".txt") {
		t.Fatalf("expected saved filename to end with .txt, got %q", res.SavedFileName)
	}
	if !strings.Contains(res.SavedFileName, "file1.txt") {
		t.Fatalf("expected saved filename to include ORIGINAL_FILENAME, got %q", res.SavedFileName)
	}
}

func TestSave_TemplateUsesConfiguredExtensionAtEnd(t *testing.T) {
	dir := t.TempDir()
	task := &config.DownloadTask{
		URL: "https://example.com/file1.txt",
		Save: config.SaveConfig{
			BaseDownloadDir:  dir,
			FilenameTemplate: "downloaded_{{.ORIGINAL_FILENAME}}_{{.TIMESTAMP}}",
			Extension:        "pdf",
		},
	}

	res, err := Save("task1", task, "https://example.com/file1.txt", []byte("data"), "application/pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(res.SavedFileName, ".pdf") {
		t.Fatalf("expected saved filename to end with .pdf, got %q", res.SavedFileName)
	}
}

func TestSave_TemplateURLIsSanitizedForFilename(t *testing.T) {
	dir := t.TempDir()
	task := &config.DownloadTask{
		URL: "https://example.com/index",
		Save: config.SaveConfig{
			BaseDownloadDir:  dir,
			FilenameTemplate: "{{.URL}}",
		},
	}

	res, err := Save("task1", task, "https://example.com/path/to/file.zip?foo=bar&x=1", []byte("data"), "application/zip")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(res.SavedFileName, "/") || strings.Contains(res.SavedFileName, "\\") {
		t.Fatalf("expected sanitized filename without path separators, got %q", res.SavedFileName)
	}
	if strings.Contains(res.SavedFileName, ":") || strings.Contains(res.SavedFileName, "?") {
		t.Fatalf("expected sanitized filename without reserved chars, got %q", res.SavedFileName)
	}
	if _, err := os.Stat(res.FilePath); err != nil {
		t.Fatalf("expected saved file to exist: %v", err)
	}
}

func TestSave_TemplateDomainVariable(t *testing.T) {
	dir := t.TempDir()
	task := &config.DownloadTask{
		URL: "https://example.com/index",
		Save: config.SaveConfig{
			BaseDownloadDir:  dir,
			FilenameTemplate: "{{.DOMAIN}}_{{.ORIGINAL_FILENAME}}",
		},
	}

	res, err := Save("task1", task, "https://sub.example.com/path/to/file.zip?x=1", []byte("data"), "application/zip")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(res.SavedFileName, "sub.example.com_") {
		t.Fatalf("expected DOMAIN placeholder to resolve to hostname, got %q", res.SavedFileName)
	}
	if !strings.HasSuffix(res.SavedFileName, ".zip") {
		t.Fatalf("expected saved filename to keep extension, got %q", res.SavedFileName)
	}
}
