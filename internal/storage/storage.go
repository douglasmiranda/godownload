package storage

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
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
	baseDir := task.Save.BaseDownloadDir
	if strings.TrimSpace(baseDir) == "" {
		baseDir = "."
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return SaveResult{}, fmt.Errorf("create base directory: %w", err)
	}

	originalName := inferOriginalName(downloadURL)
	finalName, err := resolveFilename(task, downloadURL, originalName)
	if err != nil {
		return SaveResult{}, err
	}
	finalPath := filepath.Join(baseDir, finalName)

	if st, err := os.Stat(finalPath); err == nil && !st.IsDir() {
		if !task.ForceDownload {
			return SaveResult{Skipped: true, SkipReason: "file already exists", FilePath: finalPath, SavedFileName: finalName}, nil
		}
		if task.Save.OverrideIfExists {
			// Continue and overwrite below.
		} else {
			finalPath, finalName = nextAvailablePath(finalPath)
		}
	}

	if err := os.WriteFile(finalPath, content, 0o644); err != nil {
		return SaveResult{}, fmt.Errorf("write downloaded file: %w", err)
	}

	result := SaveResult{FilePath: finalPath, SavedFileName: finalName}
	if task.Save.SaveMetadata {
		metaPath := metadataPathFor(finalPath)
		meta := Metadata{
			MainURL:          task.URL,
			DownloadURL:      downloadURL,
			OriginalFileName: originalName,
			SavedFileName:    finalName,
			Timestamp:        time.Now().UTC().Format(time.RFC3339),
			Size:             int64(len(content)),
			MIMEType:         contentType,
			DownloadTaskName: taskName,
		}
		if err := writeMetadata(metaPath, meta); err != nil {
			return SaveResult{}, err
		}
		result.MetadataPath = metaPath
	}

	return result, nil
}

func resolveFilename(task *config.DownloadTask, downloadURL string, originalName string) (string, error) {
	if strings.TrimSpace(task.Save.Filename) != "" {
		return applyExtension(task.Save.Filename, task.Save.Extension), nil
	}

	tplText := task.Save.FilenameTemplate
	if strings.TrimSpace(tplText) == "" {
		tplText = "{{.ORIGINAL_FILENAME}}"
	}
	vars := map[string]string{
		"ORIGINAL_FILENAME": originalName,
		"URL":               downloadURL,
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
	targetExt := normalizeExtension(task.Save.Extension)
	if targetExt == "" {
		targetExt = normalizeExtension(path.Ext(originalName))
	}
	return ensureTrailingExtension(name, targetExt), nil
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
		return "downloaded_file"
	}
	return name
}

func nextAvailablePath(path string) (string, string) {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s_%d%s", base, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, filepath.Base(candidate)
		}
	}
}

func metadataPathFor(filePath string) string {
	ext := filepath.Ext(filePath)
	base := strings.TrimSuffix(filePath, ext)
	return base + "_metadata.json"
}

func writeMetadata(path string, metadata Metadata) error {
	b, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("write metadata: %w", err)
	}
	return nil
}
