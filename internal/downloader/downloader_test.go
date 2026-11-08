package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/douglasmiranda/godownload/internal/config"
	"github.com/douglasmiranda/godownload/internal/hooks"
)

func TestRunTask_FollowRedirectsDisabledReportsRedirectError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect.json":
			http.Redirect(w, r, "/final.json", http.StatusFound)
		case "/final.json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	followRedirects := false
	task := &config.DownloadTask{
		URL:  server.URL + "/redirect.json",
		Save: config.SaveConfig{BaseDownloadDir: dir},
		Security: config.SecurityConfig{
			AllowedFileTypes:  []string{"json"},
			AllowInsecureHTTP: true,
			FollowRedirects:   &followRedirects,
		},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(res.Errors) == 0 {
		t.Fatal("expected error when URL redirects with follow_redirects disabled")
	}
	errMsg := res.Errors[0].Error()
	if !strings.Contains(errMsg, "security.follow_redirects is false") {
		t.Fatalf("expected follow_redirects error, got %v", res.Errors[0])
	}
	if !strings.Contains(errMsg, "302") {
		t.Fatalf("expected redirect status in error, got %v", res.Errors[0])
	}
	if !strings.Contains(errMsg, "/final.json") {
		t.Fatalf("expected redirect location in error, got %v", res.Errors[0])
	}
}

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

func TestRunTask_SkipsExistingFileBeforeFetch(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/pdf")
		fmt.Fprint(w, "new-bytes")
	}))
	defer server.Close()

	dir := t.TempDir()
	existing := filepath.Join(dir, "file.pdf")
	if err := os.WriteFile(existing, []byte("old-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	task := &config.DownloadTask{
		URL:      server.URL + "/file.pdf",
		Save:     config.SaveConfig{BaseDownloadDir: dir},
		Security: config.SecurityConfig{AllowedFileTypes: []string{"pdf"}, AllowInsecureHTTP: true},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(res.Saved) != 1 || !res.Saved[0].Skipped {
		t.Fatalf("expected one skipped save, got %#v", res.Saved)
	}
	if requests.Load() != 0 {
		t.Fatalf("expected existing file to avoid fetch, got %d requests", requests.Load())
	}
	b, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "old-bytes" {
		t.Fatalf("expected original file unchanged, got %q", string(b))
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

func TestRunTask_DownloadLimitTruncatesCandidates(t *testing.T) {
	var secondRequests atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/second.pdf" {
			secondRequests.Add(1)
		}
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<a href="%s/first.pdf">First</a><a href="%s/second.pdf">Second</a>`, server.URL, server.URL)
		case "/first.pdf", "/second.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			fmt.Fprint(w, "pdf")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:           server.URL + "/",
		Selector:      "a",
		DownloadLimit: 1,
		Save:          config.SaveConfig{BaseDownloadDir: t.TempDir()},
		Security:      config.SecurityConfig{AllowedFileTypes: []string{"pdf"}, AllowInsecureHTTP: true},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 2, DefaultAttrMapping: map[string]string{"a": "href"}})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(res.Saved) != 1 {
		t.Fatalf("expected one saved file after download_limit, got %d", len(res.Saved))
	}
	if secondRequests.Load() != 0 {
		t.Fatalf("expected second candidate not to be downloaded, got %d requests", secondRequests.Load())
	}
}

func TestRunTask_SelectorMaxDepthCrawlsNestedPages(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<a href="%s/page-2">Next</a><a href="/top.pdf">Top-level file</a>`, server.URL)
		case "/page-2":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<a href="%s/file.pdf">File</a><a href="/">Back to start</a>`, server.URL)
		case "/file.pdf", "/top.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			fmt.Fprint(w, "pdf"+r.URL.Path)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:      server.URL + "/",
		Selector: "a",
		MaxDepth: 2,
		Save:     config.SaveConfig{BaseDownloadDir: dir},
		Security: config.SecurityConfig{AllowInsecureHTTP: true},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1, DefaultAttrMapping: map[string]string{"a": "href"}})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	// page-2 is only crawled for links; the PDFs from both depths are saved.
	got := savedNames(res)
	want := []string{"file.pdf", "top.pdf"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("expected saved files %v, got %v", want, got)
	}
	if string(mustRead(t, filepath.Join(dir, "top.pdf"))) != "pdf/top.pdf" {
		t.Fatal("unexpected content for top.pdf")
	}
}

func TestRunTask_AllowedFileTypesBlockURLBeforeFetch(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		fmt.Fprint(w, "bin")
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:      server.URL + "/file.bin",
		Save:     config.SaveConfig{BaseDownloadDir: t.TempDir()},
		Security: config.SecurityConfig{AllowedFileTypes: []string{"pdf"}, AllowInsecureHTTP: true},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(res.Errors) == 0 {
		t.Fatal("expected URL extension to be blocked")
	}
	if !strings.Contains(res.Errors[0].Error(), "URL extension blocked") {
		t.Fatalf("expected extension block error, got %v", res.Errors[0])
	}
	if requests.Load() != 0 {
		t.Fatalf("expected blocked URL extension to avoid fetch, got %d requests", requests.Load())
	}
}

func TestRunTask_AllowedFileTypesBlockResponseContentType(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html>login required</html>")
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:      server.URL + "/file.pdf",
		Save:     config.SaveConfig{BaseDownloadDir: dir},
		Security: config.SecurityConfig{AllowedFileTypes: []string{"pdf"}, AllowInsecureHTTP: true},
		Retry:    config.Retry{Enabled: true, Config: config.RetryConfig{OnRuntimeError: true, Retries: 2}},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(res.Errors) == 0 {
		t.Fatal("expected response content-type to be blocked")
	}
	if !strings.Contains(res.Errors[0].Error(), "response content-type blocked") {
		t.Fatalf("expected content-type block error, got %v", res.Errors[0])
	}
	if requests.Load() != 1 {
		t.Fatalf("expected a blocked content type not to be retried, got %d requests", requests.Load())
	}
	assertDirEmpty(t, dir)
}

func TestRunTask_AllowedFileTypesAcceptGenericBinaryContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		fmt.Fprint(w, "%PDF-1.7")
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:      server.URL + "/file.pdf",
		Save:     config.SaveConfig{BaseDownloadDir: t.TempDir()},
		Security: config.SecurityConfig{AllowedFileTypes: []string{"pdf"}, AllowInsecureHTTP: true},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(res.Errors) > 0 {
		t.Fatalf("expected octet-stream response to be accepted for .pdf URL, got %v", res.Errors)
	}
}

type recordingProgress struct {
	mu      sync.Mutex
	started []string
	totals  map[string]int
	done    map[string]int
}

func (r *recordingProgress) TaskStarted(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = append(r.started, name)
}

func (r *recordingProgress) TaskTotal(name string, total int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.totals == nil {
		r.totals = make(map[string]int)
	}
	r.totals[name] = total
}

func (r *recordingProgress) TaskFileDone(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done == nil {
		r.done = make(map[string]int)
	}
	r.done[name]++
}

func (r *recordingProgress) TaskFinished(name string) {}

func TestRunTask_ReportsProgress(t *testing.T) {
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

	progress := &recordingProgress{}
	svc := &Service{TaskProgress: progress}
	res := svc.RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 2, DefaultAttrMapping: map[string]string{"a": "href"}})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}

	progress.mu.Lock()
	defer progress.mu.Unlock()
	if len(progress.started) != 1 || progress.started[0] != "task1" {
		t.Fatalf("expected one TaskStarted for task1, got %v", progress.started)
	}
	if progress.totals["task1"] != 2 {
		t.Fatalf("expected total 2, got %d", progress.totals["task1"])
	}
	if progress.done["task1"] != 2 {
		t.Fatalf("expected 2 TaskFileDone calls, got %d", progress.done["task1"])
	}
}

func TestRunTask_DedupesGeneratedCandidateURLs(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
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
	if requests.Load() != 1 {
		t.Fatalf("expected one request after dedupe, got %d", requests.Load())
	}
	if len(res.Saved) != 1 {
		t.Fatalf("expected one saved file, got %d", len(res.Saved))
	}
}

func TestRunTask_BlocksDisallowedSelectorSeedURL(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
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
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Error(), "blocked by security policy") {
		t.Fatalf("expected a security policy error for the configured URL, got %v", res.Errors)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("expected no extra warnings, got %v", res.Warnings)
	}
	if requests.Load() != 0 {
		t.Fatalf("expected blocked selector seed to avoid request, got %d requests", requests.Load())
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
	var tokenSeen atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer secret" {
			tokenSeen.Store(true)
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
	if !tokenSeen.Load() {
		t.Fatal("expected bearer token to be sent")
	}
}

func TestRunTask_RetryOnServerError(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
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
		Retry:    config.Retry{Enabled: true, Config: config.RetryConfig{OnRuntimeError: true, Retries: 2}},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if attempts.Load() != 2 {
		t.Fatalf("expected exactly one retry, got %d attempts", attempts.Load())
	}
	if got := string(mustRead(t, filepath.Join(dir, "file.pdf"))); got != "ok" {
		t.Fatalf("expected the successful retry's content, got %q", got)
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
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "temporary", http.StatusInternalServerError)
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:          server.URL + "/file.pdf",
		Save:         config.SaveConfig{BaseDownloadDir: dir},
		Security:     config.SecurityConfig{AllowedFileTypes: []string{"pdf"}, AllowInsecureHTTP: true},
		Retry:        config.Retry{Enabled: true, Config: config.RetryConfig{OnHTTPCode: []int{500}, Retries: 2}},
		StopAllTasks: config.StopAllTasks{OnHTTPCode: []int{500}},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(res.Errors) == 0 {
		t.Fatal("expected task errors")
	}
	if !res.AbortAll {
		t.Fatal("expected AbortAll to be true")
	}
	if attempts.Load() != 3 {
		t.Fatalf("expected 3 attempts (retries respected before stop), got %d", attempts.Load())
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

func TestRunTask_TreatWarningsAsErrors_RetriesThenStopsAllTasksOnRuntimeError(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
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
	if attempts.Load() != 3 {
		t.Fatalf("expected 3 task attempts (initial + 2 retries), got %d", attempts.Load())
	}
}

func TestRunTask_StopAllTasksOnSelectorHTTPCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:          server.URL + "/",
		Selector:     "a",
		StopAllTasks: config.StopAllTasks{OnHTTPCode: []int{401}},
		Security:     config.SecurityConfig{AllowInsecureHTTP: true},
	}

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1, DefaultAttrMapping: map[string]string{"a": "href"}})
	if len(res.Errors) == 0 {
		t.Fatal("expected selector fetch error")
	}
	if !res.AbortAll {
		t.Fatal("expected AbortAll to be true for selector-phase HTTP stop")
	}
}

func TestRunTask_StopAllTasksOnURLGenError(t *testing.T) {
	task := &config.DownloadTask{
		URLGenFile: &config.URLGenFile{
			VarName: "URL",
			File:    "missing.txt",
		},
		StopAllTasks: config.StopAllTasks{OnRuntimeError: true},
	}

	svc := &Service{
		FileLoader: func(path string) ([]string, error) {
			return nil, fmt.Errorf("file not found: %s", path)
		},
	}
	res := svc.RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(res.Errors) == 0 {
		t.Fatal("expected urlgen error")
	}
	if !res.AbortAll {
		t.Fatal("expected AbortAll to be true for urlgen failure with on_runtime_error")
	}
}

func TestRunTask_OnAbortAllCalledBeforeReturn(t *testing.T) {
	releaseSlow := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fail.json":
			http.Error(w, "boom", http.StatusInternalServerError)
		case "/slow.json":
			<-releaseSlow
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL: server.URL + "/{{.PATH}}.json",
		URLGenList: &config.URLGenList{
			VarName: "PATH",
			Values:  []string{"fail", "slow"},
		},
		Save:         config.SaveConfig{BaseDownloadDir: dir, FilenameTemplate: "{{.URL}}"},
		StopAllTasks: config.StopAllTasks{OnHTTPCode: []int{500}},
		Security: config.SecurityConfig{
			AllowedFileTypes:  []string{"json"},
			AllowInsecureHTTP: true,
		},
	}

	hookCalled := make(chan struct{})
	var hookOnce sync.Once
	var runDone int32
	svc := &Service{
		OnAbortAll: func() {
			hookOnce.Do(func() { close(hookCalled) })
		},
	}

	done := make(chan struct{})
	go func() {
		svc.RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 2})
		atomic.StoreInt32(&runDone, 1)
		close(done)
	}()

	select {
	case <-hookCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for OnAbortAll")
	}
	if atomic.LoadInt32(&runDone) != 0 {
		t.Fatal("OnAbortAll should fire before RunTask returns")
	}
	close(releaseSlow)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for RunTask to return")
	}
}

func TestRunTask_HooksRunSuccessThenComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:      server.URL + "/file.txt",
		Save:     config.SaveConfig{BaseDownloadDir: t.TempDir()},
		Security: config.SecurityConfig{AllowInsecureHTTP: true},
		Hooks: config.HooksConfig{
			OnSuccess:  &config.HookCommand{Command: "success"},
			OnFailure:  &config.HookCommand{Command: "failure"},
			OnComplete: &config.HookCommand{Command: "complete"},
		},
	}
	runner := &recordingHookRunner{}

	res := (&Service{HookRunner: runner}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if got := strings.Join(runner.names, ","); got != "on_success,on_complete" {
		t.Fatalf("expected success then complete hooks, got %q", got)
	}
}

func TestRunTask_HooksRunFailureThenComplete(t *testing.T) {
	task := &config.DownloadTask{
		URL:      "https://example.com/file.txt",
		Security: config.SecurityConfig{AllowedDomains: []string{"blocked.test"}},
		Hooks: config.HooksConfig{
			OnSuccess:  &config.HookCommand{Command: "success"},
			OnFailure:  &config.HookCommand{Command: "failure"},
			OnComplete: &config.HookCommand{Command: "complete"},
		},
	}
	runner := &recordingHookRunner{}

	res := (&Service{HookRunner: runner}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(res.Errors) == 0 {
		t.Fatal("expected download error")
	}
	if got := strings.Join(runner.names, ","); got != "on_failure,on_complete" {
		t.Fatalf("expected failure then complete hooks, got %q", got)
	}
}

func TestRunTask_HookFailureCanWarnOrFailTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	makeTask := func(failTask bool) *config.DownloadTask {
		return &config.DownloadTask{
			URL:      server.URL + "/file.txt",
			Save:     config.SaveConfig{BaseDownloadDir: t.TempDir()},
			Security: config.SecurityConfig{AllowInsecureHTTP: true},
			Hooks: config.HooksConfig{
				FailTaskIfError: failTask,
				OnSuccess:       &config.HookCommand{Command: "success"},
			},
		}
	}

	warnRes := (&Service{HookRunner: &recordingHookRunner{failNames: map[string]bool{"on_success": true}}}).RunTask(context.Background(), "task1", makeTask(false), config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(warnRes.Errors) > 0 {
		t.Fatalf("expected hook failure warning only, got errors %v", warnRes.Errors)
	}
	if len(warnRes.Warnings) != 1 || !strings.Contains(warnRes.Warnings[0], "hook on_success failed") {
		t.Fatalf("expected hook warning, got %#v", warnRes.Warnings)
	}

	failRes := (&Service{HookRunner: &recordingHookRunner{failNames: map[string]bool{"on_success": true}}}).RunTask(context.Background(), "task1", makeTask(true), config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(failRes.Errors) == 0 {
		t.Fatal("expected hook failure to fail task")
	}
	if !strings.Contains(failRes.Errors[len(failRes.Errors)-1].Error(), "hook on_success failed") {
		t.Fatalf("expected hook failure error, got %v", failRes.Errors)
	}
}

func TestRunTask_HookWarningPromotionTriggersAbortAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:                   server.URL + "/file.txt",
		Save:                  config.SaveConfig{BaseDownloadDir: t.TempDir()},
		Security:              config.SecurityConfig{AllowInsecureHTTP: true},
		TreatWarningsAsErrors: true,
		StopAllTasks:          config.StopAllTasks{OnRuntimeError: true},
		Hooks: config.HooksConfig{
			OnSuccess: &config.HookCommand{Command: "success"},
		},
	}

	var called atomic.Bool
	res := (&Service{
		HookRunner: &recordingHookRunner{failNames: map[string]bool{"on_success": true}},
		OnAbortAll: func() {
			called.Store(true)
		},
	}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})

	if len(res.Errors) == 0 || len(res.Warnings) != 0 {
		t.Fatalf("expected hook warning promoted to error, got warnings=%v errors=%v", res.Warnings, res.Errors)
	}
	if !res.AbortAll || !called.Load() {
		t.Fatalf("expected promoted hook warning to trigger abort all: result=%+v called=%v", res, called.Load())
	}
}

func TestRunTask_HooksReceiveTaskContext(t *testing.T) {
	task := &config.DownloadTask{
		Hooks: config.HooksConfig{
			OnSuccess: &config.HookCommand{Command: "success"},
		},
	}
	runner := &recordingHookRunner{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res := (&Service{HookRunner: runner}).finalizeWithHooks(ctx, "task1", task, TaskResult{TaskName: "task1"})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(runner.contextErrs) != 1 || !errors.Is(runner.contextErrs[0], context.Canceled) {
		t.Fatalf("expected hook to receive canceled task context, got %#v", runner.contextErrs)
	}
}

func TestRunTask_OnCompleteRunsAfterFailingOutcomeHook(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:      server.URL + "/file.txt",
		Save:     config.SaveConfig{BaseDownloadDir: t.TempDir()},
		Security: config.SecurityConfig{AllowInsecureHTTP: true},
		Hooks: config.HooksConfig{
			FailTaskIfError: true,
			OnSuccess:       &config.HookCommand{Command: "success"},
			OnComplete:      &config.HookCommand{Command: "complete"},
		},
	}
	runner := &recordingHookRunner{failNames: map[string]bool{"on_success": true}}

	res := (&Service{HookRunner: runner}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 1})
	if len(res.Errors) == 0 {
		t.Fatal("expected failing outcome hook to fail task")
	}
	if got := strings.Join(runner.names, ","); got != "on_success,on_complete" {
		t.Fatalf("expected on_complete after failing outcome hook, got %q", got)
	}
}

func TestRunTask_WarningOnlyResultRunsSuccessHook(t *testing.T) {
	task := &config.DownloadTask{
		Hooks: config.HooksConfig{
			OnSuccess: &config.HookCommand{Command: "success"},
			OnFailure: &config.HookCommand{Command: "failure"},
		},
	}
	runner := &recordingHookRunner{}

	res := (&Service{HookRunner: runner}).finalizeWithHooks(context.Background(), "task1", task, TaskResult{
		TaskName: "task1",
		Warnings: []string{"file already exists; skipped"},
	})
	if len(res.Errors) > 0 || len(res.Warnings) == 0 {
		t.Fatalf("expected warning-only task result, got warnings=%v errors=%v", res.Warnings, res.Errors)
	}
	if got := strings.Join(runner.names, ","); got != "on_success" {
		t.Fatalf("expected warning-only result to run on_success, got %q", got)
	}
}

func TestRunTask_HooksReceiveTaskEnvAndResultFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "hello")
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:      server.URL + "/file.txt",
		Save:     config.SaveConfig{BaseDownloadDir: t.TempDir()},
		Security: config.SecurityConfig{AllowInsecureHTTP: true},
		Hooks:    config.HooksConfig{OnSuccess: &config.HookCommand{Command: "process"}},
	}
	runner := &recordingHookRunner{}
	res := (&Service{HookRunner: runner}).RunTask(context.Background(), "reports", task, oneWorker())
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(runner.envs) != 1 {
		t.Fatalf("expected one hook run, got %d", len(runner.envs))
	}
	env := runner.envs[0]
	if env["GODOWNLOAD_TASK"] != "reports" || env["GODOWNLOAD_STATUS"] != "success" {
		t.Fatalf("unexpected hook env: %v", env)
	}

	var result hookResultFile
	if err := json.Unmarshal([]byte(runner.resultFiles[0]), &result); err != nil {
		t.Fatalf("decode result file: %v (%q)", err, runner.resultFiles[0])
	}
	const helloSHA256 = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if len(result.Saved) != 1 || result.Saved[0].SHA256 != helloSHA256 || result.Saved[0].URL != task.URL {
		t.Fatalf("unexpected result file: %+v", result)
	}
	if _, err := os.Stat(env["GODOWNLOAD_RESULT_FILE"]); !os.IsNotExist(err) {
		t.Fatalf("expected result file to be removed after hooks, got %v", err)
	}
}

func TestRunTask_DifferentURLsResolvingToSameFileAreReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, r.URL.Query().Get("item"))
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:        server.URL + "/report.txt?item={{.ITEM}}",
		URLGenList: &config.URLGenList{VarName: "ITEM", Values: []string{"A", "B"}},
		Save:       config.SaveConfig{BaseDownloadDir: dir},
		Security:   config.SecurityConfig{AllowInsecureHTTP: true},
	}
	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 2})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "resolves to the same file as") {
		t.Fatalf("expected a collision warning, got %v", res.Warnings)
	}
	if got := string(mustRead(t, filepath.Join(dir, "report.txt"))); got != "A" && got != "B" {
		t.Fatalf("unexpected content %q", got)
	}
}

func TestRunTask_PromotedWarningRetrySkippedWhenFilesWereSaved(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "data")
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:                   server.URL + "/report.txt?item={{.ITEM}}",
		URLGenList:            &config.URLGenList{VarName: "ITEM", Values: []string{"A", "B"}},
		TreatWarningsAsErrors: true,
		Save:                  config.SaveConfig{BaseDownloadDir: t.TempDir()},
		Security:              config.SecurityConfig{AllowInsecureHTTP: true},
		Retry:                 config.Retry{Enabled: true, Config: config.RetryConfig{OnRuntimeError: true, Retries: 3}},
	}
	res := (&Service{}).RunTask(context.Background(), "task1", task, oneWorker())
	if len(res.Errors) != 1 {
		t.Fatalf("expected the promoted collision warning as the only error, got %v", res.Errors)
	}
	if requests.Load() != 1 {
		t.Fatalf("expected the task not to be re-run, got %d requests", requests.Load())
	}
}

func TestRunTask_URLGenFileCanBeAURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/list.txt" {
			fmt.Fprint(w, "\ufeff# files to fetch\r\n/a.txt\r\n\r\n/b.txt\r\n")
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, r.URL.Path)
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:        server.URL + "{{.PATH}}",
		URLGenFile: &config.URLGenFile{VarName: "PATH", File: server.URL + "/list.txt"},
		Save:       config.SaveConfig{BaseDownloadDir: dir},
		Security:   config.SecurityConfig{AllowInsecureHTTP: true},
	}
	res := (&Service{}).RunTask(context.Background(), "task1", task, oneWorker())
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if got := savedNames(res); strings.Join(got, ",") != "a.txt,b.txt" {
		t.Fatalf("expected a.txt and b.txt, got %v", got)
	}
}

type recordingHookRunner struct {
	names       []string
	commands    []string
	contextErrs []error
	envs        []map[string]string
	resultFiles []string
	failNames   map[string]bool
}

func (r *recordingHookRunner) Run(ctx context.Context, name string, command string, timeoutSeconds int, env map[string]string) hooks.Result {
	r.names = append(r.names, name)
	r.commands = append(r.commands, command)
	r.contextErrs = append(r.contextErrs, ctx.Err())
	r.envs = append(r.envs, env)
	if path := env["GODOWNLOAD_RESULT_FILE"]; path != "" {
		b, _ := os.ReadFile(path)
		r.resultFiles = append(r.resultFiles, string(b))
	}
	if r.failNames[name] {
		return hooks.Result{Name: name, Command: command, ExitCode: 2, Output: "hook output", Err: errors.New("hook " + name + " failed")}
	}
	return hooks.Result{Name: name, Command: command}
}

// savedNames returns the sorted relative names of files saved (not skipped).
func savedNames(res TaskResult) []string {
	var names []string
	for _, s := range res.Saved {
		if !s.Skipped {
			names = append(names, filepath.ToSlash(s.SavedFileName))
		}
	}
	sort.Strings(names)
	return names
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func assertDirEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected %s to be empty, found %v", dir, names)
	}
}

func oneWorker() config.WorkerConfig {
	return config.WorkerConfig{DownloadsPerTaskConcurrency: 1, DefaultAttrMapping: map[string]string{"a": "href"}}
}

// otherHostURL rewrites a 127.0.0.1 test server URL to use "localhost", so
// tests can have two servers on different hostnames.
func otherHostURL(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	u.Host = "localhost:" + u.Port()
	return u.String()
}
