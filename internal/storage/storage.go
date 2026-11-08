package storage

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"

	"github.com/douglasmiranda/godownload/internal/config"
	"github.com/google/uuid"
)

type SaveResult struct {
	FilePath      string
	MetadataPath  string
	Skipped       bool
	SkipReason    string
	SavedFileName string
	DownloadURL   string
	Size          int64
	SHA256        string
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

// Path returns the absolute destination path.
func (t Target) Path() string { return t.finalPath }

// Name returns the destination path relative to the base download directory.
func (t Target) Name() string { return t.finalName }

type Metadata struct {
	MainURL          string `json:"main_url"`
	DownloadURL      string `json:"download_url"`
	OriginalFileName string `json:"original_filename"`
	SavedFileName    string `json:"saved_filename"`
	Timestamp        string `json:"timestamp"`
	Size             int64  `json:"size"`
	SHA256           string `json:"sha256"`
	MIMEType         string `json:"mime_type"`
	DownloadTaskName string `json:"download_task_name"`
}

// Save resolves the target for downloadURL and writes content to it.
func Save(taskName string, task *config.DownloadTask, downloadURL string, content []byte, contentType string) (SaveResult, error) {
	if task == nil {
		return SaveResult{}, fmt.Errorf("task is nil")
	}
	target, err := ResolveTarget(task, downloadURL)
	if err != nil {
		return SaveResult{}, err
	}
	return SaveToTarget(taskName, task, downloadURL, target, bytes.NewReader(content), contentType)
}

// SaveToTarget streams body to a previously resolved target. The data is
// written to a temporary file next to the target and only moved to the final
// name after body has been read completely, so a failed or interrupted
// download never leaves a partial file under the final name.
func SaveToTarget(taskName string, task *config.DownloadTask, downloadURL string, target Target, body io.Reader, contentType string) (SaveResult, error) {
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

	tmpName, size, sum, err := writeTemp(root, target.finalName, body)
	if err != nil {
		return SaveResult{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = root.Remove(tmpName)
		}
	}()

	finalName, skipped, err := commitTemp(root, tmpName, target.finalName, task)
	if err != nil {
		return SaveResult{}, fmt.Errorf("save downloaded file: %w", err)
	}
	if skipped {
		return SaveResult{Skipped: true, SkipReason: "file already exists", FilePath: target.finalPath, SavedFileName: target.finalName, DownloadURL: downloadURL}, nil
	}
	committed = true
	finalPath, err := containedPath(target.baseDir, finalName)
	if err != nil {
		return SaveResult{}, err
	}

	result := SaveResult{FilePath: finalPath, SavedFileName: finalName, DownloadURL: downloadURL, Size: size, SHA256: sum}
	if task.Save.SaveMetadata {
		metaName := metadataPathFor(finalName)
		meta := Metadata{
			MainURL:          task.URL,
			DownloadURL:      downloadURL,
			OriginalFileName: target.originalName,
			SavedFileName:    finalName,
			Timestamp:        time.Now().Format(time.RFC3339),
			Size:             size,
			SHA256:           sum,
			MIMEType:         contentType,
			DownloadTaskName: taskName,
		}
		if err := writeMetadata(root, metaName, meta); err != nil {
			return SaveResult{}, err
		}
		result.MetadataPath = metadataPathFor(finalPath)
	}

	return result, nil
}

// writeTemp copies body into a new hidden file in the target's directory and
// returns its name, size and SHA-256. The file is removed on error.
func writeTemp(root *os.Root, finalName string, body io.Reader) (string, int64, string, error) {
	var f *os.File
	var tmpName string
	for range 10 {
		tmpName = tempNameFor(finalName)
		var err error
		f, err = root.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return "", 0, "", fmt.Errorf("create temporary file: %w", err)
		}
		f = nil
	}
	if f == nil {
		return "", 0, "", fmt.Errorf("create temporary file: too many name collisions")
	}

	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(f, h), body)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = root.Remove(tmpName)
		return "", 0, "", fmt.Errorf("write downloaded file: %w", err)
	}
	return tmpName, size, hex.EncodeToString(h.Sum(nil)), nil
}

func tempNameFor(finalName string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	dir, base := filepath.Split(finalName)
	return filepath.Join(dir, "."+base+"."+hex.EncodeToString(b[:])+".part")
}

// commitTemp moves tmpName to name following the task's existing-file rules.
// It returns the name actually used, or skipped=true when name already exists
// and the task does not force downloads.
func commitTemp(root *os.Root, tmpName string, name string, task *config.DownloadTask) (string, bool, error) {
	if task.ForceDownload && task.Save.OverrideIfExists {
		return name, false, root.Rename(tmpName, name)
	}

	err := renameNoReplace(root, tmpName, name)
	if err == nil {
		return name, false, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return "", false, err
	}
	if !task.ForceDownload {
		return "", true, nil
	}

	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s_%d%s", base, i, ext)
		err := renameNoReplace(root, tmpName, candidate)
		if err == nil {
			return candidate, false, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", false, err
		}
	}
}

// renameNoReplace moves oldName to newName, failing with os.ErrExist when
// newName already exists.
func renameNoReplace(root *os.Root, oldName string, newName string) error {
	err := root.Link(oldName, newName)
	if err == nil {
		_ = root.Remove(oldName)
		return nil
	}
	if errors.Is(err, os.ErrExist) {
		return os.ErrExist
	}
	// Some filesystems do not support hard links. Reserve the name
	// exclusively instead, then replace the empty placeholder.
	f, err := root.OpenFile(newName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_ = f.Close()
	if err := root.Rename(oldName, newName); err != nil {
		_ = root.Remove(newName)
		return err
	}
	return nil
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
	now := time.Now()
	vars := map[string]string{
		"ORIGINAL_FILENAME": sanitizeFilenameValue(originalName),
		"URL":               sanitizeFilenameValue(downloadURL),
		"URL_PATH":          urlPath,
		"URL_DIR":           urlDir,
		"DOMAIN":            domainFromURL(downloadURL),
		"TIMESTAMP":         now.Format(time.RFC3339),
		"DATE":              now.Format("2006-01-02"),
		"DATE_DIR":          now.Format("2006/01/02"),
		"TIME":              now.Format("15-04-05"),
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
	if v == "" {
		return ""
	}
	if isWindowsReservedName(v) {
		v = "_" + v
	}
	return truncateComponent(v)
}

// maxComponentBytes leaves room under the common 255-byte filename limit for
// the "_metadata.json" suffix and the temporary ".<name>.<random>.part" name.
const maxComponentBytes = 200

// truncateComponent shortens long names, keeping the extension and adding a
// short hash of the full name so different long names stay distinct.
func truncateComponent(v string) string {
	if len(v) <= maxComponentBytes {
		return v
	}
	ext := path.Ext(v)
	if len(ext) > 16 {
		ext = ""
	}
	sum := sha256.Sum256([]byte(v))
	suffix := "_" + hex.EncodeToString(sum[:4]) + ext
	keep := maxComponentBytes - len(suffix)
	stem := v[:keep]
	for !utf8.ValidString(stem) {
		stem = stem[:len(stem)-1]
	}
	return stem + suffix
}

var windowsReservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM0": true, "COM1": true, "COM2": true, "COM3": true, "COM4": true,
	"COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT0": true, "LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true,
	"LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

func isWindowsReservedName(v string) bool {
	stem := v
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	return windowsReservedNames[strings.ToUpper(strings.TrimSpace(stem))]
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
	tmpName, _, _, err := writeTemp(root, name, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	if err := root.Rename(tmpName, name); err != nil {
		_ = root.Remove(tmpName)
		return fmt.Errorf("write metadata: %w", err)
	}
	return nil
}
