package downloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	res := svc.RunTask(context.Background(), "task1", task, config.WorkerConfig{Concurrency: 2})
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
	res := svc.RunTask(context.Background(), "task1", task, config.WorkerConfig{Concurrency: 2, DefaultAttrMapping: map[string]string{"a": "href"}})
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(res.Saved) != 2 {
		t.Fatalf("expected 2 saved files, got %d", len(res.Saved))
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

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{Concurrency: 1})
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

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{Concurrency: 1})
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

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{Concurrency: 1})
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

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{Concurrency: 1})
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

	res := (&Service{}).RunTask(context.Background(), "task1", task, config.WorkerConfig{Concurrency: 1})
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
