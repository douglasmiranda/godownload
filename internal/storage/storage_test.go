package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/douglasmiranda/godownload/internal/config"
)

func TestSave_ConcurrentSameNameNoDataLoss(t *testing.T) {
	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:           "https://example.com",
		ForceDownload: true,
		Save:          config.SaveConfig{BaseDownloadDir: dir},
	}

	const n = 16
	var wg sync.WaitGroup
	results := make([]SaveResult, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Distinct URLs that all resolve to the same base filename.
			url := fmt.Sprintf("https://host%d.example.com/file.zip", i)
			results[i], errs[i] = Save("task1", task, url, []byte(fmt.Sprintf("content-%d", i)), "application/zip")
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("save %d failed: %v", i, errs[i])
		}
		if results[i].Skipped {
			t.Fatalf("save %d unexpectedly skipped", i)
		}
		if seen[results[i].FilePath] {
			t.Fatalf("two concurrent saves claimed the same path %q", results[i].FilePath)
		}
		seen[results[i].FilePath] = true
	}

	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(files) != n {
		t.Fatalf("expected %d distinct files, got %d", n, len(files))
	}
}

func TestSave_FilenameCannotTraverseBaseDir(t *testing.T) {
	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:  "https://example.com/a",
		Save: config.SaveConfig{BaseDownloadDir: dir, Filename: "../../escape.txt"},
	}

	res, err := Save("task1", task, "https://example.com/a", []byte("x"), "text/plain")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(res.SavedFileName, "/") || strings.Contains(res.SavedFileName, "\\") {
		t.Fatalf("filename contains path separators: %q", res.SavedFileName)
	}
	abs, err := filepath.Abs(res.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(abs, absDir+string(os.PathSeparator)) {
		t.Fatalf("saved file escaped base dir: %s not under %s", abs, absDir)
	}
}

func TestOpenTargetRejectsPathOutsideBaseDir(t *testing.T) {
	dir := t.TempDir()
	task := &config.DownloadTask{}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	f, _, _, _, err := openTarget(root, dir, "../escape.txt", task)
	if err == nil {
		if f != nil {
			f.Close()
		}
		t.Fatal("expected path containment error")
	}
}

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

func TestSkipIfExists_WhenFilePresentAndNotForced(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.pdf")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := &config.DownloadTask{
		URL:  "https://example.com/a.pdf",
		Save: config.SaveConfig{BaseDownloadDir: dir},
	}

	resolved, err := ResolveTarget(task, "https://example.com/a.pdf")
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	res, skip, err := SkipIfExists(task, resolved)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !skip {
		t.Fatal("expected skip")
	}
	if !res.Skipped || res.SkipReason != "file already exists" {
		t.Fatalf("unexpected skip result: %#v", res)
	}
	if res.SavedFileName != "a.pdf" {
		t.Fatalf("expected SavedFileName a.pdf, got %q", res.SavedFileName)
	}
}

func TestSkipIfExists_WhenMissingOrForced(t *testing.T) {
	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:  "https://example.com/a.pdf",
		Save: config.SaveConfig{BaseDownloadDir: dir},
	}

	resolved, err := ResolveTarget(task, "https://example.com/a.pdf")
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	_, skip, err := SkipIfExists(task, resolved)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skip {
		t.Fatal("expected no skip when file is missing")
	}

	if err := os.WriteFile(filepath.Join(dir, "a.pdf"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	task.ForceDownload = true
	_, skip, err = SkipIfExists(task, resolved)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skip {
		t.Fatal("expected no skip when force_download is true")
	}
}

func TestSaveToTarget_ReusesDynamicFilename(t *testing.T) {
	dir := t.TempDir()
	task := &config.DownloadTask{
		URL: "https://example.com/a.pdf",
		Save: config.SaveConfig{
			BaseDownloadDir:  dir,
			FilenameTemplate: "{{.UUID}}_{{.ORIGINAL_FILENAME}}",
		},
	}

	target, err := ResolveTarget(task, "https://example.com/a.pdf")
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	_, skip, err := SkipIfExists(task, target)
	if err != nil {
		t.Fatalf("check target: %v", err)
	}
	if skip {
		t.Fatal("expected resolved target to be missing")
	}

	result, err := SaveToTarget("task1", task, "https://example.com/a.pdf", target, []byte("new"), "application/pdf")
	if err != nil {
		t.Fatalf("save target: %v", err)
	}
	if result.FilePath != target.finalPath {
		t.Fatalf("expected save to reuse %q, got %q", target.finalPath, result.FilePath)
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

func TestSave_ForceDownloadAddsSuffixWhenOverrideDisabled(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.pdf")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := &config.DownloadTask{
		URL:           "https://example.com/a.pdf",
		ForceDownload: true,
		Save:          config.SaveConfig{BaseDownloadDir: dir},
	}

	result, err := Save("task1", task, "https://example.com/a.pdf", []byte("new"), "application/pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Skipped {
		t.Fatal("expected force_download to save a new file")
	}
	if result.SavedFileName != "a_1.pdf" {
		t.Fatalf("expected suffixed filename a_1.pdf, got %q", result.SavedFileName)
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "old" {
		t.Fatalf("expected original file to remain unchanged, got %q", string(b))
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

func TestSave_TemplateDateDirCreatesUTCDateDirectories(t *testing.T) {
	dir := t.TempDir()
	task := &config.DownloadTask{
		URL: "https://example.com/file.gz",
		Save: config.SaveConfig{
			BaseDownloadDir:  dir,
			FilenameTemplate: "{{.DATE_DIR}}/{{.ORIGINAL_FILENAME}}",
		},
	}
	dateBefore := time.Now().UTC().Format("2006/01/02")

	res, err := Save("task1", task, task.URL, []byte("data"), "application/gzip")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dateAfter := time.Now().UTC().Format("2006/01/02")
	expectedBefore := filepath.Join(dateBefore, "file.gz")
	expectedAfter := filepath.Join(dateAfter, "file.gz")
	if res.SavedFileName != expectedBefore && res.SavedFileName != expectedAfter {
		t.Fatalf("expected UTC date directory %q or %q, got %q", expectedBefore, expectedAfter, res.SavedFileName)
	}
	if _, err := os.Stat(res.FilePath); err != nil {
		t.Fatalf("expected file in UTC date directory: %v", err)
	}
}

func TestSave_TemplateURLPathCreatesNestedDirectories(t *testing.T) {
	dir := t.TempDir()
	task := &config.DownloadTask{
		URL: "https://example.com/index",
		Save: config.SaveConfig{
			BaseDownloadDir:  dir,
			FilenameTemplate: "{{.URL_PATH}}",
			SaveMetadata:     true,
		},
	}
	downloadURL := "https://example.com/media/publicacoes/2026/7/24/file.pdf?token=secret#page=1"

	res, err := Save("task1", task, downloadURL, []byte("pdf"), "application/pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	relativeName := filepath.Join("media", "publicacoes", "2026", "7", "24", "file.pdf")
	expectedPath := filepath.Join(dir, relativeName)
	if res.SavedFileName != relativeName {
		t.Fatalf("expected saved relative path %q, got %q", relativeName, res.SavedFileName)
	}
	if res.FilePath != expectedPath {
		t.Fatalf("expected file path %q, got %q", expectedPath, res.FilePath)
	}
	if string(mustReadFile(t, expectedPath)) != "pdf" {
		t.Fatal("saved file content did not match")
	}
	expectedMetadataPath := filepath.Join(dir, "media", "publicacoes", "2026", "7", "24", "file_metadata.json")
	if res.MetadataPath != expectedMetadataPath {
		t.Fatalf("expected metadata path %q, got %q", expectedMetadataPath, res.MetadataPath)
	}
	if _, err := os.Stat(expectedMetadataPath); err != nil {
		t.Fatalf("expected nested metadata file: %v", err)
	}
}

func TestSave_TemplateURLDirWithOriginalFilename(t *testing.T) {
	dir := t.TempDir()
	task := &config.DownloadTask{
		URL: "https://example.com/index",
		Save: config.SaveConfig{
			BaseDownloadDir:  dir,
			FilenameTemplate: "{{.URL_DIR}}/copy_{{.ORIGINAL_FILENAME}}",
		},
	}

	res, err := Save("task1", task, "https://example.com/a/b/file.txt?version=2", []byte("data"), "text/plain")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedName := filepath.Join("a", "b", "copy_file.txt")
	if res.SavedFileName != expectedName {
		t.Fatalf("expected URL_DIR output %q, got %q", expectedName, res.SavedFileName)
	}
}

func TestSave_TemplateURLDirSupportsTopLevelFile(t *testing.T) {
	dir := t.TempDir()
	task := &config.DownloadTask{
		URL: "https://example.com/file.txt",
		Save: config.SaveConfig{
			BaseDownloadDir:  dir,
			FilenameTemplate: "{{.URL_DIR}}/{{.ORIGINAL_FILENAME}}",
		},
	}

	res, err := Save("task1", task, "https://example.com/file.txt", []byte("data"), "text/plain")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.SavedFileName != "file.txt" {
		t.Fatalf("expected top-level filename, got %q", res.SavedFileName)
	}
}

func TestSave_NestedTemplateCollisionAddsSuffixToLeaf(t *testing.T) {
	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:           "https://example.com/a/file.pdf",
		ForceDownload: true,
		Save: config.SaveConfig{
			BaseDownloadDir:  dir,
			FilenameTemplate: "{{.URL_PATH}}",
		},
	}

	if _, err := Save("task1", task, task.URL, []byte("first"), "application/pdf"); err != nil {
		t.Fatalf("first save failed: %v", err)
	}
	res, err := Save("task1", task, task.URL, []byte("second"), "application/pdf")
	if err != nil {
		t.Fatalf("second save failed: %v", err)
	}
	expectedName := filepath.Join("a", "file_1.pdf")
	if res.SavedFileName != expectedName {
		t.Fatalf("expected nested collision name %q, got %q", expectedName, res.SavedFileName)
	}
}

func TestResolveTarget_RejectsTemplateTraversal(t *testing.T) {
	task := &config.DownloadTask{
		URL: "https://example.com/file.txt",
		Save: config.SaveConfig{
			BaseDownloadDir:  t.TempDir(),
			FilenameTemplate: "../../{{.ORIGINAL_FILENAME}}",
		},
	}

	if _, err := ResolveTarget(task, task.URL); err == nil {
		t.Fatal("expected parent traversal to be rejected")
	}
}

func TestResolveTarget_RejectsEncodedURLTraversal(t *testing.T) {
	task := &config.DownloadTask{
		URL: "https://example.com/safe/%2e%2e/file.txt",
		Save: config.SaveConfig{
			BaseDownloadDir:  t.TempDir(),
			FilenameTemplate: "{{.URL_PATH}}",
		},
	}

	if _, err := ResolveTarget(task, task.URL); err == nil {
		t.Fatal("expected encoded URL traversal to be rejected")
	}
}

func TestSanitizeRelativePath_PreservesBackslashSeparatedHierarchy(t *testing.T) {
	// Simulates the second sanitizeRelativePath pass after filepath.Join on Windows.
	got, err := sanitizeRelativePath(`media\publicacoes\2026\file.pdf`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join("media", "publicacoes", "2026", "file.pdf")
	if got != expected {
		t.Fatalf("expected nested path %q, got %q", expected, got)
	}
}

func TestResolveFilename_SecondSanitizeKeepsNestedPath(t *testing.T) {
	task := &config.DownloadTask{
		URL: "https://example.com/media/publicacoes/2026/file.pdf",
		Save: config.SaveConfig{
			FilenameTemplate: "{{.URL_PATH}}",
			Extension:        "pdf",
		},
	}

	got, err := resolveFilename(task, task.URL, "file.pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join("media", "publicacoes", "2026", "file.pdf")
	if got != expected {
		t.Fatalf("expected nested path %q, got %q", expected, got)
	}
}

func TestSave_URLPathEncodedSeparatorDoesNotCreateHierarchy(t *testing.T) {
	dir := t.TempDir()
	task := &config.DownloadTask{
		URL: "https://example.com/a%2Fb/file.txt",
		Save: config.SaveConfig{
			BaseDownloadDir:  dir,
			FilenameTemplate: "{{.URL_PATH}}",
		},
	}

	res, err := Save("task1", task, task.URL, []byte("data"), "text/plain")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedName := filepath.Join("a_b", "file.txt")
	if res.SavedFileName != expectedName {
		t.Fatalf("expected encoded slash to remain within one component %q, got %q", expectedName, res.SavedFileName)
	}
}

func TestSave_URLPathCannotEscapeThroughSymlink(t *testing.T) {
	baseDir := t.TempDir()
	outsideDir := t.TempDir()
	if err := os.Symlink(outsideDir, filepath.Join(baseDir, "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	task := &config.DownloadTask{
		URL: "https://example.com/linked/file.txt",
		Save: config.SaveConfig{
			BaseDownloadDir:  baseDir,
			FilenameTemplate: "{{.URL_PATH}}",
		},
	}

	if _, err := Save("task1", task, task.URL, []byte("data"), "text/plain"); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "file.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no file outside base directory, got error %v", err)
	}
}

func mustReadFile(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
