package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/douglasmiranda/godownload/internal/config"
	"github.com/google/uuid"
)

type SaveResult struct {
	FilePath      string
	MetadataPath  string
	Skipped       bool
	SkipReason    string
	SavedFileName string
}

// Target is the resolved destination for a download. Its values are kept
// private so callers can pass it between storage operations without changing
// the resolved filename.
type Target struct {
	baseDir      string
	finalName    string
	finalPath    string
	originalName string
}

type Metadata struct {
	MainURL          string `json:"main_url"`
	DownloadURL      string `json:"download_url"`
	OriginalFileName string `json:"original_filename"`
	SavedFileName    string `json:"saved_filename"`
	Timestamp        string `json:"timestamp"`
	Size             int64  `json:"size"`
	MIMEType         string `json:"mime_type"`
	DownloadTaskName string `json:"download_task_name"`
}

func Save(taskName string, task *config.DownloadTask, downloadURL string, content []byte, contentType string) (SaveResult, error) {
	if task == nil {
		return SaveResult{}, fmt.Errorf("task is nil")
	}
	target, err := ResolveTarget(task, downloadURL)
	if err != nil {
		return SaveResult{}, err
	}
	return SaveToTarget(taskName, task, downloadURL, target, content, contentType)
}

// SaveToTarget saves content to a previously resolved target.
func SaveToTarget(taskName string, task *config.DownloadTask, downloadURL string, target Target, content []byte, contentType string) (SaveResult, error) {
	if task == nil {
		return SaveResult{}, fmt.Errorf("task is nil")
	}
	if err := os.MkdirAll(target.baseDir, 0o755); err != nil {
		return SaveResult{}, fmt.Errorf("create base directory: %w", err)
	}
	root, err := os.OpenRoot(target.baseDir)
	if err != nil {
		return SaveResult{}, fmt.Errorf("open base directory: %w", err)
	}
	defer root.Close()

	parentDir := filepath.Dir(target.finalName)
	if err := root.MkdirAll(parentDir, 0o755); err != nil {
		return SaveResult{}, fmt.Errorf("create target directory: %w", err)
	}

	f, finalPath, finalName, skipped, err := openTarget(root, target.baseDir, target.finalName, task)
	if err != nil {
		return SaveResult{}, fmt.Errorf("create downloaded file: %w", err)
	}
	if skipped {
		return SaveResult{Skipped: true, SkipReason: "file already exists", FilePath: finalPath, SavedFileName: finalName}, nil
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return SaveResult{}, fmt.Errorf("write downloaded file: %w", err)
	}
	if err := f.Close(); err != nil {
		return SaveResult{}, fmt.Errorf("write downloaded file: %w", err)
	}

	result := SaveResult{FilePath: finalPath, SavedFileName: finalName}
	if task.Save.SaveMetadata {
		metaName := metadataPathFor(finalName)
		metaPath := metadataPathFor(finalPath)
		meta := Metadata{
			MainURL:          task.URL,
			DownloadURL:      downloadURL,
			OriginalFileName: target.originalName,
			SavedFileName:    finalName,
			Timestamp:        time.Now().UTC().Format(time.RFC3339),
			Size:             int64(len(content)),
			MIMEType:         contentType,
			DownloadTaskName: taskName,
		}
		if err := writeMetadata(root, metaName, meta); err != nil {
			return SaveResult{}, err
		}
		result.MetadataPath = metaPath
	}

	return result, nil
}

// SkipIfExists reports whether target already exists and the task would skip
// writing (force_download is false). Callers can use this to avoid downloading
// content that would be discarded.
func SkipIfExists(task *config.DownloadTask, target Target) (SaveResult, bool, error) {
	if task == nil {
		return SaveResult{}, false, fmt.Errorf("task is nil")
	}
	if task.ForceDownload {
		return SaveResult{}, false, nil
	}

	root, err := os.OpenRoot(target.baseDir)
	if errors.Is(err, os.ErrNotExist) {
		return SaveResult{}, false, nil
	}
	if err != nil {
		return SaveResult{}, false, fmt.Errorf("open base directory: %w", err)
	}
	defer root.Close()

	_, err = root.Stat(target.finalName)
	if err == nil {
		return SaveResult{
			Skipped:       true,
			SkipReason:    "file already exists",
			FilePath:      target.finalPath,
			SavedFileName: target.finalName,
		}, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return SaveResult{}, false, nil
	}
	return SaveResult{}, false, fmt.Errorf("stat target file: %w", err)
}

// ResolveTarget resolves a download destination exactly once so dynamic
// filename template values remain stable through existence checks and saving.
func ResolveTarget(task *config.DownloadTask, downloadURL string) (Target, error) {
	if task == nil {
		return Target{}, fmt.Errorf("task is nil")
	}
	baseDir := task.Save.BaseDownloadDir
	if strings.TrimSpace(baseDir) == "" {
		baseDir = "."
	}
	originalName := inferOriginalName(downloadURL)
	finalName, err := resolveFilename(task, downloadURL, originalName)
	if err != nil {
		return Target{}, err
	}
	finalPath, err := containedPath(baseDir, finalName)
	if err != nil {
		return Target{}, err
	}
	return Target{
		baseDir:      baseDir,
		finalName:    finalName,
		finalPath:    finalPath,
		originalName: originalName,
	}, nil
}

// openTarget atomically reserves the destination path so that concurrent
// downloads within a task cannot race on os.Stat/os.WriteFile (TOCTOU) and
// silently overwrite each other. It returns an open file handle for the
// caller to write to, or skipped=true when the file already exists and the
// task is not configured to force/override.
func openTarget(root *os.Root, baseDir string, name string, task *config.DownloadTask) (f *os.File, finalPath string, finalName string, skipped bool, err error) {
	if root == nil {
		return nil, "", name, false, fmt.Errorf("base directory root is nil")
	}
	finalPath, err = containedPath(baseDir, name)
	if err != nil {
		return nil, "", name, false, err
	}

	if task.ForceDownload && task.Save.OverrideIfExists {
		f, err = root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		return f, finalPath, name, false, err
	}

	f, err = root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err == nil {
		return f, finalPath, name, false, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return nil, finalPath, name, false, err
	}

	if !task.ForceDownload {
		return nil, finalPath, name, true, nil
	}

	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		candidateName := fmt.Sprintf("%s_%d%s", base, i, ext)
		candidatePath, err := containedPath(baseDir, candidateName)
		if err != nil {
			return nil, "", candidateName, false, err
		}
		f, err = root.OpenFile(candidateName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			return f, candidatePath, candidateName, false, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, candidatePath, candidateName, false, err
		}
	}
}

func containedPath(baseDir string, name string) (string, error) {
	if filepath.IsAbs(name) || filepath.VolumeName(name) != "" || path.IsAbs(filepath.ToSlash(name)) {
		return "", fmt.Errorf("resolved path must be relative to base directory: %s", name)
	}
	base, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("resolve base directory: %w", err)
	}
	target := filepath.Join(base, name)
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", fmt.Errorf("resolve target path: %w", err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("resolved path escapes base directory: %s", name)
	}
	return target, nil
}

func resolveFilename(task *config.DownloadTask, downloadURL string, originalName string) (string, error) {
	if strings.TrimSpace(task.Save.Filename) != "" {
		// Sanitize to a single path-component so an explicit filename cannot
		// traverse outside the base download directory (e.g. "../../etc/x").
		cleaned := sanitizeFilenameValue(filepath.Base(task.Save.Filename))
		if cleaned == "" {
			return "", fmt.Errorf("resolved filename is empty after sanitization")
		}
		return applyExtension(cleaned, task.Save.Extension), nil
	}

	tplText := task.Save.FilenameTemplate
	if strings.TrimSpace(tplText) == "" {
		tplText = "{{.ORIGINAL_FILENAME}}"
	}
	urlPath, urlDir := "", ""
	if strings.Contains(tplText, "URL_PATH") || strings.Contains(tplText, "URL_DIR") {
		resolvedURLPath, resolvedURLDir, err := urlPathTemplateValues(downloadURL, originalName)
		if err != nil {
			return "", err
		}
		urlPath, urlDir = resolvedURLPath, resolvedURLDir
	}
	vars := map[string]string{
		"ORIGINAL_FILENAME": sanitizeFilenameValue(originalName),
		"URL":               sanitizeFilenameValue(downloadURL),
		"URL_PATH":          urlPath,
		"URL_DIR":           urlDir,
		"DOMAIN":            domainFromURL(downloadURL),
		"TIMESTAMP":         time.Now().UTC().Format(time.RFC3339),
		"DATE":              time.Now().UTC().Format("2006-01-02"),
		"TIME":              time.Now().UTC().Format("15-04-05"),
		"UUID":              uuid.NewString(),
	}

	tpl, err := template.New("filename").Option("missingkey=error").Parse(tplText)
	if err != nil {
		return "", fmt.Errorf("parse filename_template: %w", err)
	}
	var b strings.Builder
	if err := tpl.Execute(&b, vars); err != nil {
		return "", fmt.Errorf("execute filename_template: %w", err)
	}
	name := strings.TrimSpace(b.String())
	if name == "" {
		return "", fmt.Errorf("resolved filename is empty")
	}
	name, err = sanitizeRelativePath(name)
	if err != nil {
		return "", err
	}
	targetExt := normalizeExtension(task.Save.Extension)
	if targetExt == "" {
		targetExt = normalizeExtension(path.Ext(originalName))
	}
	name = ensureTrailingExtension(name, targetExt)
	return sanitizeRelativePath(name)
}

var invalidFilenameCharsPattern = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
var windowsAbsolutePathPattern = regexp.MustCompile(`^[A-Za-z]:[/\\]`)

func sanitizeFilenameValue(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	v = invalidFilenameCharsPattern.ReplaceAllString(v, "_")
	v = strings.ReplaceAll(v, "\n", "_")
	v = strings.ReplaceAll(v, "\r", "_")
	v = strings.Trim(v, " .")
	for strings.Contains(v, "__") {
		v = strings.ReplaceAll(v, "__", "_")
	}
	return v
}

func sanitizeRelativePath(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", fmt.Errorf("resolved filename is empty")
	}
	if strings.HasPrefix(v, "/") || strings.HasPrefix(v, `\`) || windowsAbsolutePathPattern.MatchString(v) {
		return "", fmt.Errorf("resolved path must be relative to base directory: %s", v)
	}
	if strings.HasSuffix(v, "/") || strings.HasSuffix(v, `\`) {
		return "", fmt.Errorf("resolved path must end with a filename: %s", v)
	}

	// Normalize separators before splitting so a second pass after filepath.Join
	// still preserves nested directories on Windows (where Join uses `\`).
	rawParts := strings.Split(strings.ReplaceAll(v, `\`, "/"), "/")
	parts := make([]string, 0, len(rawParts))
	for _, rawPart := range rawParts {
		part := strings.TrimSpace(rawPart)
		switch part {
		case "", ".":
			continue
		case "..":
			return "", fmt.Errorf("resolved path contains parent traversal: %s", v)
		}
		part = sanitizeFilenameValue(part)
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("resolved path contains an invalid component: %s", v)
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("resolved filename is empty after sanitization")
	}
	return filepath.Join(parts...), nil
}

func urlPathTemplateValues(downloadURL string, fallbackName string) (string, string, error) {
	u, err := url.Parse(downloadURL)
	if err != nil {
		return "", "", fmt.Errorf("parse download URL path: %w", err)
	}

	rawSegments := strings.Split(u.EscapedPath(), "/")
	segments := make([]string, 0, len(rawSegments))
	for _, rawSegment := range rawSegments {
		if rawSegment == "" {
			continue
		}
		segment, err := url.PathUnescape(rawSegment)
		if err != nil {
			return "", "", fmt.Errorf("decode download URL path component: %w", err)
		}
		if segment == "." || segment == ".." {
			return "", "", fmt.Errorf("download URL path contains traversal component: %s", downloadURL)
		}
		segment = sanitizeFilenameValue(segment)
		if segment == "" || segment == "." || segment == ".." {
			return "", "", fmt.Errorf("download URL path contains an invalid component: %s", downloadURL)
		}
		segments = append(segments, segment)
	}

	if len(segments) == 0 {
		fallback := sanitizeFilenameValue(fallbackName)
		if fallback == "" {
			return "", "", fmt.Errorf("download URL path does not contain a usable filename: %s", downloadURL)
		}
		segments = append(segments, fallback)
	}

	urlPath := strings.Join(segments, "/")
	return urlPath, path.Dir(urlPath), nil
}

func applyExtension(filename string, ext string) string {
	norm := normalizeExtension(ext)
	if norm == "" {
		return filename
	}
	base := strings.TrimSuffix(filename, path.Ext(filename))
	return base + "." + norm
}

func ensureTrailingExtension(filename string, ext string) string {
	norm := normalizeExtension(ext)
	if norm == "" {
		return filename
	}
	suffix := "." + norm
	if strings.HasSuffix(strings.ToLower(filename), strings.ToLower(suffix)) {
		return filename
	}
	return filename + suffix
}

func normalizeExtension(ext string) string {
	return strings.TrimPrefix(strings.TrimSpace(ext), ".")
}

func inferOriginalName(downloadURL string) string {
	u, err := url.Parse(downloadURL)
	if err != nil {
		return "downloaded_file"
	}
	name := path.Base(u.Path)
	if strings.TrimSpace(name) == "" || name == "/" || name == "." {
		return u.Hostname() + "_downloaded_file"
	}
	return name
}

func domainFromURL(downloadURL string) string {
	u, err := url.Parse(downloadURL)
	if err != nil {
		return "unknown-domain"
	}
	host := strings.TrimSpace(u.Hostname())
	if host == "" {
		return "unknown-domain"
	}
	return host
}

func metadataPathFor(filePath string) string {
	ext := filepath.Ext(filePath)
	base := strings.TrimSuffix(filePath, ext)
	return base + "_metadata.json"
}

func writeMetadata(root *os.Root, name string, metadata Metadata) error {
	b, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if err := root.WriteFile(name, b, 0o644); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	return nil
}
