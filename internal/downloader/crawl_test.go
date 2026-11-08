package downloader

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/douglasmiranda/godownload/internal/config"
)

func TestRunTask_CrawlDownloadsFileLinksWithoutFetchingThemAsPages(t *testing.T) {
	var mu sync.Mutex
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<a href="/report.pdf">pdf</a><a href="/download">binary without extension</a>`)
		case "/report.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			fmt.Fprint(w, "pdf")
		case "/download":
			w.Header().Set("Content-Type", "application/octet-stream")
			fmt.Fprint(w, "bin")
		}
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:      server.URL + "/",
		Selector: "a",
		MaxDepth: 2,
		Save:     config.SaveConfig{BaseDownloadDir: t.TempDir()},
		Security: config.SecurityConfig{AllowInsecureHTTP: true},
	}
	res := (&Service{}).RunTask(context.Background(), "task1", task, oneWorker())
	if len(res.Errors) > 0 || len(res.Warnings) > 0 {
		t.Fatalf("unexpected errors=%v warnings=%v", res.Errors, res.Warnings)
	}
	if got := savedNames(res); strings.Join(got, ",") != "download,report.pdf" {
		t.Fatalf("expected both links to be saved, got %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests["/report.pdf"] != 1 {
		t.Fatalf("expected report.pdf to be requested once, got %d", requests["/report.pdf"])
	}
}

func TestRunTask_CrawlStopsOnceDownloadLimitIsReached(t *testing.T) {
	var deeper atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<a href="/a.pdf">a</a><a href="/b.pdf">b</a><a href="/more.html">more</a>`)
		case "/more.html":
			deeper.Add(1)
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<a href="/c.pdf">c</a>`)
		default:
			w.Header().Set("Content-Type", "application/pdf")
			fmt.Fprint(w, r.URL.Path)
		}
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:           server.URL + "/",
		Selector:      "a",
		MaxDepth:      3,
		DownloadLimit: 2,
		Save:          config.SaveConfig{BaseDownloadDir: t.TempDir()},
		Security:      config.SecurityConfig{AllowInsecureHTTP: true},
	}
	res := (&Service{}).RunTask(context.Background(), "task1", task, oneWorker())
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if got := savedNames(res); strings.Join(got, ",") != "a.pdf,b.pdf" {
		t.Fatalf("expected the first two files, got %v", got)
	}
	if deeper.Load() != 0 {
		t.Fatalf("expected no deeper pages to be read once download_limit was reached, got %d", deeper.Load())
	}
}

func TestRunTask_CrawlStopsAtPageLimitWithWarning(t *testing.T) {
	var pages atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/" {
			for i := range maxCrawlPages + 5 {
				fmt.Fprintf(w, `<a href="/p/%d">page</a>`, i)
			}
			return
		}
		pages.Add(1)
		fmt.Fprint(w, `<a href="/file.pdf">file</a>`)
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:      server.URL + "/",
		Selector: "a",
		MaxDepth: 2,
		Security: config.SecurityConfig{AllowInsecureHTTP: true},
	}
	res := (&Service{DryRun: true}).RunTask(context.Background(), "task1", task, config.WorkerConfig{DownloadsPerTaskConcurrency: 8, DefaultAttrMapping: map[string]string{"a": "href"}})
	if got := pages.Load(); got != maxCrawlPages {
		t.Fatalf("expected %d linked pages to be read, got %d", maxCrawlPages, got)
	}
	if len(res.Warnings) == 0 || !strings.Contains(res.Warnings[0], "5 more were not read") {
		t.Fatalf("expected a crawl limit warning, got %v", res.Warnings)
	}
}

func TestMayBeHTMLPage(t *testing.T) {
	cases := map[string]bool{
		"https://example.com/":                true,
		"https://example.com/docs/page":       true,
		"https://example.com/index.html":      true,
		"https://example.com/list.php?page=2": true,
		"https://example.com/release-1.2":     true,
		"https://example.com/file.PDF":        false,
		"https://example.com/archive.zip":     false,
		"https://example.com/data.json":       false,
	}
	for in, want := range cases {
		if got := mayBeHTMLPage(in); got != want {
			t.Errorf("mayBeHTMLPage(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestLooksLikeHTML(t *testing.T) {
	cases := []struct {
		contentType string
		body        string
		want        bool
	}{
		{"text/html; charset=utf-8", "", true},
		{"application/xhtml+xml", "", true},
		{"text/plain", "<html><body>hi</body></html>", false},
		{"application/octet-stream", "<html></html>", false},
		{"", "<!DOCTYPE html><html><body>hi</body></html>", true},
		{"", "%PDF-1.7 binary", false},
		{"not a media type", "", false},
	}
	for _, tc := range cases {
		br := bufio.NewReader(strings.NewReader(tc.body))
		if got := looksLikeHTML(tc.contentType, br); got != tc.want {
			t.Errorf("looksLikeHTML(%q, %q) = %v, want %v", tc.contentType, tc.body, got, tc.want)
		}
	}
}

func TestRunTask_SelectorSeedPageErrorIsReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:      server.URL + "/index.html",
		Selector: "a",
		Security: config.SecurityConfig{AllowInsecureHTTP: true},
	}
	res := (&Service{}).RunTask(context.Background(), "task1", task, oneWorker())
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Error(), "status 404") {
		t.Fatalf("expected the 404 of the selector page as error, got errors=%v warnings=%v", res.Errors, res.Warnings)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("expected no zero-match warning, got %v", res.Warnings)
	}
}

func TestRunTask_DiscoveredLinksBlockedByPolicyAreSkipped(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<a href="/doc.pdf">doc</a><a href="/notes.txt">notes</a><a href="%s/elsewhere.pdf">external</a>`,
				otherHostURL(t, server.URL))
		case "/doc.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			fmt.Fprint(w, "pdf")
		default:
			t.Errorf("unexpected request for %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:      server.URL + "/",
		Selector: "a",
		Save:     config.SaveConfig{BaseDownloadDir: t.TempDir()},
		Security: config.SecurityConfig{
			AllowInsecureHTTP: true,
			AllowedDomains:    []string{"127.0.0.1"},
			AllowedFileTypes:  []string{"pdf"},
		},
	}
	res := (&Service{}).RunTask(context.Background(), "task1", task, oneWorker())
	if len(res.Errors) > 0 || len(res.Warnings) > 0 {
		t.Fatalf("expected blocked discovered links to be skipped quietly, got errors=%v warnings=%v", res.Errors, res.Warnings)
	}
	if got := savedNames(res); len(got) != 1 || got[0] != "doc.pdf" {
		t.Fatalf("expected only doc.pdf, got %v", got)
	}
}
