package downloader

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/douglasmiranda/godownload/internal/config"
)

func TestBackoffDelay(t *testing.T) {
	on, off := true, false
	cases := []struct {
		cfg     config.RetryConfig
		attempt int
		want    time.Duration
	}{
		{config.RetryConfig{Delay: 0, ExponentialBackoff: &on}, 3, 0},
		{config.RetryConfig{Delay: 2, ExponentialBackoff: &off}, 3, 2 * time.Second},
		{config.RetryConfig{Delay: 2, ExponentialBackoff: &on}, 1, 2 * time.Second},
		{config.RetryConfig{Delay: 2, ExponentialBackoff: &on}, 3, 8 * time.Second},
		{config.RetryConfig{Delay: 2, ExponentialBackoff: &on}, 100, maxRetryDelay},
	}
	for _, tc := range cases {
		if got := backoffDelay(tc.cfg, tc.attempt); got != tc.want {
			t.Errorf("backoffDelay(delay=%d, attempt=%d) = %s, want %s", tc.cfg.Delay, tc.attempt, got, tc.want)
		}
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Duration{
		"":                              0,
		"3":                             3 * time.Second,
		"-1":                            0,
		"garbage":                       0,
		"86400":                         maxRetryDelay,
		"Thu, 01 Jan 2026 12:00:10 GMT": 10 * time.Second,
		"Thu, 01 Jan 2026 11:00:00 GMT": 0,
	}
	for in, want := range cases {
		if got := parseRetryAfter(in, now); got != want {
			t.Errorf("parseRetryAfter(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestShouldRetry(t *testing.T) {
	runtimeOn := config.RetryConfig{OnRuntimeError: true}
	cases := []struct {
		name string
		cfg  config.RetryConfig
		err  error
		want bool
	}{
		{"network error", runtimeOn, errors.New("ConnectionError: reset"), true},
		{"network error without on_runtime_error", config.RetryConfig{}, errors.New("ConnectionError: reset"), false},
		{"default retryable code", config.RetryConfig{}, httpStatusError{Code: http.StatusBadGateway}, true},
		{"404 even with on_runtime_error", runtimeOn, httpStatusError{Code: http.StatusNotFound}, false},
		{"configured code", config.RetryConfig{OnHTTPCode: []int{404}}, httpStatusError{Code: http.StatusNotFound}, true},
		{"code outside configured list", config.RetryConfig{OnHTTPCode: []int{404}}, httpStatusError{Code: http.StatusBadGateway}, false},
		{"wrapped status code", config.RetryConfig{}, fmt.Errorf("ctx: %w", httpStatusError{Code: 503}), true},
		{"permanent error", runtimeOn, permanent(errors.New("blocked")), false},
		{"certificate error", runtimeOn, fmt.Errorf("tls: %w", x509.UnknownAuthorityError{}), false},
	}
	for _, tc := range cases {
		if got := shouldRetry(tc.cfg, tc.err); got != tc.want {
			t.Errorf("%s: shouldRetry = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRunTask_SlowDownloadIsNotCutByTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		for i := range 5 {
			fmt.Fprintf(w, "chunk-%d\n", i)
			w.(http.Flusher).Flush()
			time.Sleep(400 * time.Millisecond)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:      server.URL + "/slow.txt",
		Timeout:  1,
		Save:     config.SaveConfig{BaseDownloadDir: dir},
		Security: config.SecurityConfig{AllowInsecureHTTP: true},
	}
	res := (&Service{}).RunTask(context.Background(), "task1", task, oneWorker())
	if len(res.Errors) > 0 {
		t.Fatalf("expected slow but steady download to succeed, got %v", res.Errors)
	}
	if got := string(mustRead(t, filepath.Join(dir, "slow.txt"))); strings.Count(got, "chunk-") != 5 {
		t.Fatalf("expected all chunks, got %q", got)
	}
}

func TestRunTask_StalledDownloadTimesOutWithoutPartialFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "100")
		fmt.Fprint(w, "first bytes")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:      server.URL + "/stalled.txt",
		Timeout:  1,
		Save:     config.SaveConfig{BaseDownloadDir: dir},
		Security: config.SecurityConfig{AllowInsecureHTTP: true},
	}
	res := (&Service{}).RunTask(context.Background(), "task1", task, oneWorker())
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Error(), "TimeoutError") {
		t.Fatalf("expected a timeout error, got %v", res.Errors)
	}
	assertDirEmpty(t, dir)
}

func TestRunTask_TruncatedBodyIsRetriedWithoutPartialFile(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "13")
		if attempts.Add(1) == 1 {
			// The server closes the connection after this short body.
			fmt.Fprint(w, "trunc")
			return
		}
		fmt.Fprint(w, "complete body")
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:      server.URL + "/file.txt",
		Save:     config.SaveConfig{BaseDownloadDir: dir},
		Security: config.SecurityConfig{AllowInsecureHTTP: true},
		Retry:    config.Retry{Enabled: true, Config: config.RetryConfig{OnRuntimeError: true, Retries: 1}},
	}
	res := (&Service{}).RunTask(context.Background(), "task1", task, oneWorker())
	if len(res.Errors) > 0 {
		t.Fatalf("expected retry to recover from truncated body, got %v", res.Errors)
	}
	if got := string(mustRead(t, filepath.Join(dir, "file.txt"))); got != "complete body" {
		t.Fatalf("expected complete content, got %q", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("expected only the final file, found %d entries", len(entries))
	}
}

func TestRunTask_CompressedResponseIsSavedVerbatim(t *testing.T) {
	// Go's transport asks for gzip and decompresses transparently unless
	// compression is disabled, which would save a .gz file decompressed.
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte("archive contents"))
	_ = zw.Close()

	var acceptEncoding atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acceptEncoding.Store(r.Header.Get("Accept-Encoding"))
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(gz.Bytes())
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &config.DownloadTask{
		URL:      server.URL + "/archive.gz",
		Save:     config.SaveConfig{BaseDownloadDir: dir},
		Security: config.SecurityConfig{AllowInsecureHTTP: true},
	}
	res := (&Service{}).RunTask(context.Background(), "task1", task, oneWorker())
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if got := mustRead(t, filepath.Join(dir, "archive.gz")); !bytes.Equal(got, gz.Bytes()) {
		t.Fatalf("expected gzip bytes to be saved unchanged, got %q", got)
	}
	if ae, _ := acceptEncoding.Load().(string); strings.Contains(ae, "gzip") {
		t.Fatalf("expected no gzip Accept-Encoding, got %q", ae)
	}
}

func TestRunTask_RedirectLoopIsCapped(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		http.Redirect(w, r, fmt.Sprintf("/loop-%d", n), http.StatusFound)
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:      server.URL + "/loop-0",
		Save:     config.SaveConfig{BaseDownloadDir: t.TempDir()},
		Security: config.SecurityConfig{AllowInsecureHTTP: true},
	}
	done := make(chan TaskResult, 1)
	go func() { done <- (&Service{}).RunTask(context.Background(), "task1", task, oneWorker()) }()

	select {
	case res := <-done:
		if len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Error(), "stopped after 10 redirects") {
			t.Fatalf("expected redirect limit error, got %v", res.Errors)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("redirect loop was not stopped")
	}
	if n := requests.Load(); n > 11 {
		t.Fatalf("expected at most 11 requests, got %d", n)
	}
}

func TestRunTask_CredentialsAreNotSentToDiscoveredOtherHosts(t *testing.T) {
	var otherAuth atomic.Value
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "public")
	}))
	defer other.Close()
	otherURL := otherHostURL(t, other.URL)

	var mu sync.Mutex
	seedAuth := map[string]string{}
	seed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seedAuth[r.URL.Path] = r.Header.Get("Authorization")
		mu.Unlock()
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<a href="/private.txt">mine</a><a href="%s/public.txt">theirs</a>`, otherURL)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "private")
	}))
	defer seed.Close()

	task := &config.DownloadTask{
		URL:      seed.URL + "/",
		Selector: "a",
		Auth:     &config.AuthConfig{Type: "bearer", Token: "secret"},
		Save:     config.SaveConfig{BaseDownloadDir: t.TempDir()},
		Security: config.SecurityConfig{AllowInsecureHTTP: true},
	}
	res := (&Service{}).RunTask(context.Background(), "task1", task, oneWorker())
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	mu.Lock()
	defer mu.Unlock()
	if seedAuth["/"] != "Bearer secret" || seedAuth["/private.txt"] != "Bearer secret" {
		t.Fatalf("expected credentials on the configured host, got %v", seedAuth)
	}
	if got, _ := otherAuth.Load().(string); got != "" {
		t.Fatalf("expected no credentials on other host, got %q", got)
	}
}

func TestRunTask_AllowedDomainsReceiveCredentials(t *testing.T) {
	var otherAuth atomic.Value
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "cdn")
	}))
	defer other.Close()
	otherURL := otherHostURL(t, other.URL)

	seed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<a href="%s/file.txt">cdn</a>`, otherURL)
	}))
	defer seed.Close()

	task := &config.DownloadTask{
		URL:      seed.URL + "/",
		Selector: "a",
		Auth:     &config.AuthConfig{Type: "bearer", Token: "secret"},
		Save:     config.SaveConfig{BaseDownloadDir: t.TempDir()},
		Security: config.SecurityConfig{AllowInsecureHTTP: true, AllowedDomains: []string{"127.0.0.1", "localhost"}},
	}
	res := (&Service{}).RunTask(context.Background(), "task1", task, oneWorker())
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if got, _ := otherAuth.Load().(string); got != "Bearer secret" {
		t.Fatalf("expected credentials for a host in allowed_domains, got %q", got)
	}
}

func TestRunTask_RedirectToOtherHostStripsAPIKeyHeader(t *testing.T) {
	// Go strips Authorization on cross-domain redirects, but not custom
	// headers such as an API key.
	var otherKey atomic.Value
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		otherKey.Store(r.Header.Get("X-Api-Key"))
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "redirected")
	}))
	defer other.Close()
	otherURL := otherHostURL(t, other.URL)

	var seedKey atomic.Value
	seed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seedKey.Store(r.Header.Get("X-Api-Key"))
		http.Redirect(w, r, otherURL+"/file.txt", http.StatusFound)
	}))
	defer seed.Close()

	task := &config.DownloadTask{
		URL:      seed.URL + "/file.txt",
		Auth:     &config.AuthConfig{Type: "api_key", KeyName: "X-Api-Key", KeyValue: "k-123", KeyIn: "header"},
		Save:     config.SaveConfig{BaseDownloadDir: t.TempDir()},
		Security: config.SecurityConfig{AllowInsecureHTTP: true},
	}
	res := (&Service{}).RunTask(context.Background(), "task1", task, oneWorker())
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if got, _ := seedKey.Load().(string); got != "k-123" {
		t.Fatalf("expected API key on configured host, got %q", got)
	}
	if got, _ := otherKey.Load().(string); got != "" {
		t.Fatalf("expected API key to be stripped on redirect to other host, got %q", got)
	}
}

func TestRunTask_UnauthorizedExplainsWithheldCredentials(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer other.Close()
	otherURL := otherHostURL(t, other.URL)

	seed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<a href="%s/file.txt">file</a>`, otherURL)
	}))
	defer seed.Close()

	task := &config.DownloadTask{
		URL:      seed.URL + "/",
		Selector: "a",
		Auth:     &config.AuthConfig{Type: "bearer", Token: "secret"},
		Save:     config.SaveConfig{BaseDownloadDir: t.TempDir()},
		Security: config.SecurityConfig{AllowInsecureHTTP: true},
	}
	res := (&Service{}).RunTask(context.Background(), "task1", task, oneWorker())
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Error(), "auth was not sent to localhost") {
		t.Fatalf("expected a hint about withheld credentials, got %v", res.Errors)
	}
}

func TestRunTask_SelfSignedCertOnlyTrustedForTrustedHosts(t *testing.T) {
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "other")
	}))
	defer other.Close()
	otherURL := otherHostURL(t, other.URL)

	seed := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<a href="/own.txt">own</a><a href="%s/other.txt">other</a>`, otherURL)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "own")
	}))
	defer seed.Close()

	run := func(allowedDomains []string) TaskResult {
		task := &config.DownloadTask{
			URL:      seed.URL + "/",
			Selector: "a",
			Save:     config.SaveConfig{BaseDownloadDir: t.TempDir()},
			Security: config.SecurityConfig{AllowSelfSigned: true, AllowedDomains: allowedDomains},
		}
		return (&Service{}).RunTask(context.Background(), "task1", task, oneWorker())
	}

	res := run(nil)
	if got := savedNames(res); strings.Join(got, ",") != "own.txt" {
		t.Fatalf("expected only the task host file, got %v", got)
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Error(), "certificate") {
		t.Fatalf("expected a certificate error for the other host, got %v", res.Errors)
	}

	res = run([]string{"127.0.0.1", "localhost"})
	if len(res.Errors) > 0 {
		t.Fatalf("expected hosts in allowed_domains to skip verification, got %v", res.Errors)
	}
	if got := savedNames(res); strings.Join(got, ",") != "other.txt,own.txt" {
		t.Fatalf("expected both files, got %v", got)
	}
}

func TestRunTask_RetryOnlyTransientHTTPCodes(t *testing.T) {
	var mu sync.Mutex
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/missing.txt":
			http.NotFound(w, r)
		case "/busy.txt":
			http.Error(w, "busy", http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:        server.URL + "/{{.NAME}}.txt",
		URLGenList: &config.URLGenList{VarName: "NAME", Values: []string{"missing", "busy"}},
		Save:       config.SaveConfig{BaseDownloadDir: t.TempDir()},
		Security:   config.SecurityConfig{AllowInsecureHTTP: true},
		Retry:      config.Retry{Enabled: true, Config: config.RetryConfig{OnRuntimeError: true, Retries: 2}},
	}
	res := (&Service{}).RunTask(context.Background(), "task1", task, oneWorker())
	if len(res.Errors) != 2 {
		t.Fatalf("expected both downloads to fail, got %v", res.Errors)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests["/missing.txt"] != 1 {
		t.Fatalf("expected 404 not to be retried, got %d requests", requests["/missing.txt"])
	}
	if requests["/busy.txt"] != 3 {
		t.Fatalf("expected 503 to be retried twice, got %d requests", requests["/busy.txt"])
	}
}

func TestRunTask_RetryAfterIsHonored(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	task := &config.DownloadTask{
		URL:      server.URL + "/file.txt",
		Save:     config.SaveConfig{BaseDownloadDir: t.TempDir()},
		Security: config.SecurityConfig{AllowInsecureHTTP: true},
		Retry:    config.Retry{Enabled: true, Config: config.RetryConfig{Retries: 1}},
	}
	start := time.Now()
	res := (&Service{}).RunTask(context.Background(), "task1", task, oneWorker())
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("expected Retry-After delay of about 1s, retried after %s", elapsed)
	}
}
