package downloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/douglasmiranda/godownload/internal/config"
)

func TestRunTask_DirectDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/file.pdf" {
			w.Header().Set("Content-Type", "application/pdf")
			fmt.Fprint(w, "pdf-bytes")
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:      server.URL + "/file.pdf",
		Save:     config.SaveConfig{BaseDownloadDir: dir},
		Security: config.SecurityConfig{AllowedFileTypes: []string{"pdf"}, AllowInsecureHTTP: true},
	}

	svc := &Service{}
	res := svc.RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 2})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(res.Saved) != 1 {
		t.Fatalf("expected 1 saved file, got %d", len(res.Saved))
	}
	if _, err := os.Stat(res.Saved[0].FilePath); err != nil {
		t.Fatalf("expected saved file to exist: %v", err)
	}
}

func TestRunTask_RejectsDownloadExceedingMaxSize(t *testing.T) {
	payload := "0123456789" // 10 bytes
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		fmt.Fprint(w, payload)
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:  server.URL + "/file.pdf",
		Save: config.SaveConfig{BaseDownloadDir: dir},
		Security: config.SecurityConfig{
			AllowedFileTypes:  []string{"pdf"},
			AllowInsecureHTTP: true,
			MaxDownloadSize:   5,
		},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(res.Errors) == 0 {
		t.Fatal("expected error when download exceeds max_download_size")
	}
	if !strings.Contains(res.Errors[0].Error(), "max_download_size") {
		t.Fatalf("expected max_download_size error, got %v", res.Errors[0])
	}
	if len(res.Saved) != 0 {
		t.Fatalf("expected no files saved, got %d", len(res.Saved))
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(files) != 0 {
		t.Fatalf("expected nothing written to disk, got %v", files)
	}
}

func TestRunTask_AllowsDownloadWithinMaxSize(t *testing.T) {
	payload := "0123456789"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		fmt.Fprint(w, payload)
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:  server.URL + "/file.pdf",
		Save: config.SaveConfig{BaseDownloadDir: dir},
		Security: config.SecurityConfig{
			AllowedFileTypes:  []string{"pdf"},
			AllowInsecureHTTP: true,
			MaxDownloadSize:   config.ByteSize(len(payload)),
		},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(res.Saved) != 1 {
		t.Fatalf("expected 1 saved file, got %d", len(res.Saved))
	}
}

func TestRunTask_TaskMaxDownloadSizeOverridesGlobalLimit(t *testing.T) {
	payload := "0123456789"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		fmt.Fprint(w, payload)
	}))
	defer server.Close()

	dir := t.TempDir()
	cfg, _, err := config.LoadBytes([]byte(fmt.Sprintf(`
config:
  downloads_per_task_concurrency: 1
  security:
    allow_insecure_http: true
    max_download_size: 9b
  save:
    base_download_dir: %s
downloads:
  one:
    url: %s/file.pdf
    security:
      max_download_size: 10b
`, dir, server.URL)))
	if err != nil {
		t.Fatalf("unexpected config load error: %v", err)
	}

	task := cfg.Downloads["one"]
	res := (&Service{}).RunTask(context.Background(), "one", task, cfg.Config)
	if len(res.Errors) > 0 {
		t.Fatalf("expected task max_download_size override to allow download, got errors: %v", res.Errors)
	}
	if len(res.Saved) != 1 {
		t.Fatalf("expected 1 saved file, got %d", len(res.Saved))
	}
}

func TestRunTask_RejectsDownloadExceedingMaxInMemorySize(t *testing.T) {
	payload := "0123456789"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		fmt.Fprint(w, payload)
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:  server.URL + "/file.pdf",
		Save: config.SaveConfig{BaseDownloadDir: dir},
		Security: config.SecurityConfig{
			AllowedFileTypes:  []string{"pdf"},
			AllowInsecureHTTP: true,
			MaxInMemorySize:   4,
		},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(res.Errors) == 0 {
		t.Fatal("expected error when download exceeds max_in_memory_size")
	}
	if !strings.Contains(res.Errors[0].Error(), "max_in_memory_size") {
		t.Fatalf("expected max_in_memory_size error, got %v", res.Errors[0])
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(files) != 0 {
		t.Fatalf("expected nothing written to disk, got %v", files)
	}
}

func TestRunTask_SelectorFlow(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<a href="%s/f1.pdf">File 1</a><a href="%s/f2.pdf">File 2</a>`, server.URL, server.URL)
		case "/f1.pdf", "/f2.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			fmt.Fprint(w, "pdf")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:      server.URL + "/",
		Selector: "a",
		Save:     config.SaveConfig{BaseDownloadDir: dir},
		Security: config.SecurityConfig{AllowedFileTypes: []string{"pdf"}, AllowInsecureHTTP: true},
	}

	svc := &Service{}
	res := svc.RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 2, DefaultAttrMapping: map[string]string{"a": "href"}})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(res.Saved) != 2 {
		t.Fatalf("expected 2 saved files, got %d", len(res.Saved))
	}
}

func TestRunTask_DedupesGeneratedCandidateURLs(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL: server.URL + "/{{.PATH}}.json",
		URLGenList: &config.URLGenList{
			VarName: "PATH",
			Values:  []string{"same", "same"},
		},
		Save: config.SaveConfig{BaseDownloadDir: dir, FilenameTemplate: "{{.URL}}"},
		Security: config.SecurityConfig{
			AllowedFileTypes:  []string{"json"},
			AllowInsecureHTTP: true,
		},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 2})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if requests != 1 {
		t.Fatalf("expected one request after dedupe, got %d", requests)
	}
	if len(res.Saved) != 1 {
		t.Fatalf("expected one saved file, got %d", len(res.Saved))
	}
}

func TestRunTask_BlocksDisallowedSelectorSeedURL(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<a href="/file.pdf">file</a>`)
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:      server.URL + "/",
		Selector: "a",
		Security: config.SecurityConfig{
			AllowedDomains:    []string{"example.com"},
			AllowedFileTypes:  []string{"pdf"},
			AllowInsecureHTTP: true,
		},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1, DefaultAttrMapping: map[string]string{"a": "href"}})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("expected warning when selector seed URL is blocked")
	}
	if requests != 0 {
		t.Fatalf("expected blocked selector seed to avoid request, got %d requests", requests)
	}
}

func TestRunTask_BlocksDisallowedDomain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		fmt.Fprint(w, "pdf")
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:  server.URL + "/file.pdf",
		Save: config.SaveConfig{BaseDownloadDir: dir},
		Security: config.SecurityConfig{
			AllowedDomains:   []string{"example.com"},
			AllowedFileTypes: []string{"pdf"},
		},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(res.Errors) == 0 {
		t.Fatal("expected security error for disallowed domain")
	}
}

func TestRunTask_AppliesAuthHeader(t *testing.T) {
	tokenSeen := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer secret" {
			tokenSeen = true
		}
		w.Header().Set("Content-Type", "application/pdf")
		fmt.Fprint(w, "pdf")
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:      server.URL + "/file.pdf",
		Save:     config.SaveConfig{BaseDownloadDir: dir},
		Security: config.SecurityConfig{AllowedFileTypes: []string{"pdf"}, AllowInsecureHTTP: true},
		Auth:     &config.AuthConfig{Type: "bearer", Token: "secret"},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if !tokenSeen {
		t.Fatal("expected bearer token to be sent")
	}
}

func TestRunTask_RetryOnServerError(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			http.Error(w, "temporary", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:      server.URL + "/file.pdf",
		Save:     config.SaveConfig{BaseDownloadDir: dir},
		Security: config.SecurityConfig{AllowedFileTypes: []string{"pdf"}, AllowInsecureHTTP: true},
		Retry:    config.Retry{Enabled: true, Config: config.RetryConfig{OnRuntimeError: true, Retries: 2, Delay: 1}},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if attempt < 2 {
		t.Fatalf("expected retry attempts, got %d", attempt)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*"))
	if len(files) == 0 {
		t.Fatal("expected downloaded file")
	}
}

func TestRunTask_StopAllTasksOnRuntimeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:          server.URL + "/file.pdf",
		Save:         config.SaveConfig{BaseDownloadDir: dir},
		Security:     config.SecurityConfig{AllowedFileTypes: []string{"pdf"}, AllowInsecureHTTP: true},
		StopAllTasks: config.StopAllTasks{OnRuntimeError: true},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(res.Errors) == 0 {
		t.Fatal("expected task errors")
	}
	if !res.AbortAll {
		t.Fatal("expected AbortAll to be true")
	}
}

func TestRunTask_StopAllTasksOnHTTPCodeAfterRetries(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		http.Error(w, "temporary", http.StatusInternalServerError)
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:          server.URL + "/file.pdf",
		Save:         config.SaveConfig{BaseDownloadDir: dir},
		Security:     config.SecurityConfig{AllowedFileTypes: []string{"pdf"}, AllowInsecureHTTP: true},
		Retry:        config.Retry{Enabled: true, Config: config.RetryConfig{OnHTTPCode: []int{500}, Retries: 2, Delay: 1}},
		StopAllTasks: config.StopAllTasks{OnHTTPCode: []int{500}},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(res.Errors) == 0 {
		t.Fatal("expected task errors")
	}
	if !res.AbortAll {
		t.Fatal("expected AbortAll to be true")
	}
	if attempt != 3 {
		t.Fatalf("expected 3 attempts (retries respected before stop), got %d", attempt)
	}
}

func TestRunTask_TreatWarningsAsErrors_PromotesWarnings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><p>no links here</p></body></html>"))
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:                   server.URL + "/",
		Selector:              "a",
		TreatWarningsAsErrors: true,
		Security:              config.SecurityConfig{AllowInsecureHTTP: true},
	}

	svc := &Service{}
	res := svc.RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1, DefaultAttrMapping: map[string]string{"a": "href"}})

	if len(res.Warnings) != 0 {
		t.Fatalf("expected warnings to be promoted and cleared, got %v", res.Warnings)
	}
	if len(res.Errors) == 0 {
		t.Fatal("expected promoted warning to appear as error")
	}
	if res.Errors[0] == nil || res.Errors[0].Error() == "" {
		t.Fatal("expected promoted error message")
	}
}

func TestRunTask_TreatWarningsAsErrors_TriggersStopAllTasksOnRuntimeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><p>no links here</p></body></html>"))
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:                   server.URL + "/",
		Selector:              "a",
		TreatWarningsAsErrors: true,
		StopAllTasks:          config.StopAllTasks{OnRuntimeError: true},
		Security:              config.SecurityConfig{AllowInsecureHTTP: true},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1, DefaultAttrMapping: map[string]string{"a": "href"}})
	if len(res.Errors) == 0 {
		t.Fatal("expected promoted warning to be treated as error")
	}
	if !res.AbortAll {
		t.Fatal("expected AbortAll to be true when stop_all_tasks.on_runtime_error is enabled")
	}
}

func TestRunTask_TreatWarningsAsErrors_RetriesThenStopsAllTasksOnRuntimeError(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><p>no links here</p></body></html>"))
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:      server.URL + "/",
		Selector: "a",
		Retry: config.Retry{Enabled: true, Config: config.RetryConfig{
			OnRuntimeError: true,
			Retries:        2,
			Delay:          1,
		}},
		TreatWarningsAsErrors: true,
		StopAllTasks:          config.StopAllTasks{OnRuntimeError: true},
		Security:              config.SecurityConfig{AllowInsecureHTTP: true},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1, DefaultAttrMapping: map[string]string{"a": "href"}})
	if len(res.Errors) == 0 {
		t.Fatal("expected promoted warning to be treated as error")
	}
	if !res.AbortAll {
		t.Fatal("expected AbortAll to be true after retries are exhausted")
	}
	if attempt != 3 {
		t.Fatalf("expected 3 task attempts (initial + 2 retries), got %d", attempt)
	}
}
