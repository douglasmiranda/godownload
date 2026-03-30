package downloader

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/douglasmiranda/godownload/internal/config"
	"github.com/douglasmiranda/godownload/internal/discovery"
	"github.com/douglasmiranda/godownload/internal/security"
	"github.com/douglasmiranda/godownload/internal/storage"
	"github.com/douglasmiranda/godownload/internal/urlgen"
)

type URLFileLoader func(path string) ([]string, error)

type TaskResult struct {
	TaskName string
	Saved    []storage.SaveResult
	Errors   []error
	Warnings []string
	AbortAll bool
}

type Service struct {
	HTTPClient *http.Client
	FileLoader URLFileLoader
}

func (s *Service) RunTask(ctx context.Context, taskName string, task *config.DownloadTask, workerCfg config.WorkerConfig) TaskResult {
	result := TaskResult{TaskName: taskName}
	if task == nil {
		result.Errors = append(result.Errors, fmt.Errorf("task is nil"))
		return result
	}

	loader := s.FileLoader
	if loader == nil {
		loader = urlgen.LocalFileLoader
	}
	seedURLs, err := urlgen.Generate(task, urlgen.FileLoader(loader))
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("generate urls: %w", err))
		return result
	}
	if len(seedURLs) == 0 {
		result.Warnings = append(result.Warnings, "no URLs generated; task skipped")
		return result
	}

	candidateURLs, warn, err := s.resolveDownloadCandidates(ctx, seedURLs, task, workerCfg.DefaultAttrMapping)
	if warn != "" {
		result.Warnings = append(result.Warnings, warn)
	}
	if err != nil {
		result.Errors = append(result.Errors, err)
		return result
	}
	if len(candidateURLs) == 0 {
		result.Warnings = append(result.Warnings, "no candidate URLs found; task skipped")
		return result
	}
	if task.DownloadLimit > 0 && len(candidateURLs) > task.DownloadLimit {
		candidateURLs = candidateURLs[:task.DownloadLimit]
	}

	concurrency := workerCfg.Concurrency
	if concurrency <= 0 {
		concurrency = config.DefaultConcurrency
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var stop atomicStop

	for _, u := range candidateURLs {
		if stop.Load() {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(downloadURL string) {
			defer wg.Done()
			defer func() { <-sem }()

			saveRes, dlErr := s.downloadAndSave(ctx, taskName, task, downloadURL)
			mu.Lock()
			defer mu.Unlock()
			if dlErr != nil {
				result.Errors = append(result.Errors, dlErr)
				if shouldStopAllTasks(task, dlErr) {
					stop.Store(true)
					result.AbortAll = true
				}
				return
			}
			if saveRes.Skipped {
				result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %s", downloadURL, saveRes.SkipReason))
			}
			result.Saved = append(result.Saved, saveRes)
		}(u)
	}
	wg.Wait()

	return result
}

func (s *Service) resolveDownloadCandidates(ctx context.Context, seedURLs []string, task *config.DownloadTask, defaultAttrMapping map[string]string) ([]string, string, error) {
	if strings.TrimSpace(task.Selector) == "" {
		return seedURLs, "", nil
	}
	if task.MaxDepth < 1 {
		task.MaxDepth = 1
	}

	frontier := append([]string{}, seedURLs...)
	visited := map[string]struct{}{}
	all := []string{}

	for depth := 1; depth <= task.MaxDepth; depth++ {
		nextFrontier := []string{}
		for _, pageURL := range frontier {
			if _, seen := visited[pageURL]; seen {
				continue
			}
			visited[pageURL] = struct{}{}

			body, _, err := s.fetchURL(ctx, task, pageURL)
			if err != nil {
				if shouldStopAllTasks(task, err) {
					return nil, "", fmt.Errorf("fetch selector page %s: %w", pageURL, err)
				}
				continue
			}
			links, err := discovery.ExtractURLs(pageURL, strings.NewReader(string(body)), discovery.ExtractOptions{
				Selector:             task.Selector,
				Attr:                 task.Attr,
				DefaultAttrMappings:  defaultAttrMapping,
				RegexFilter:          task.RegexFilter,
				AttrValRegexFilter:   task.AttrValRegexFilter,
				SelectorMatchesLimit: task.SelectorMatchesLimit,
			})
			if err != nil {
				if shouldStopAllTasks(task, err) {
					return nil, "", fmt.Errorf("extract links from %s: %w", pageURL, err)
				}
				continue
			}
			all = append(all, links...)
			if depth < task.MaxDepth {
				nextFrontier = append(nextFrontier, links...)
			}
		}
		frontier = nextFrontier
		if len(frontier) == 0 {
			break
		}
	}

	if len(all) == 0 {
		return nil, "selector produced zero matches", nil
	}
	return dedupeStrings(all), "", nil
}

func (s *Service) downloadAndSave(ctx context.Context, taskName string, task *config.DownloadTask, downloadURL string) (storage.SaveResult, error) {
	if !security.IsURLAllowed(downloadURL, task.Security.AllowInsecureHTTP, task.Security.AllowedDomains) {
		return storage.SaveResult{}, fmt.Errorf("URL blocked by security policy: %s", downloadURL)
	}
	if !security.IsAllowedByExtension(downloadURL, task.Security.AllowedFileTypes) {
		return storage.SaveResult{}, fmt.Errorf("URL extension blocked by security.allowed_file_types: %s", downloadURL)
	}

	body, contentType, err := s.fetchURL(ctx, task, downloadURL)
	if err != nil {
		return storage.SaveResult{}, err
	}
	if !security.IsAllowedByContentType(contentType, task.Security.AllowedFileTypes) {
		return storage.SaveResult{}, fmt.Errorf("response content-type blocked by security.allowed_file_types: %s", contentType)
	}

	return storage.Save(taskName, task, downloadURL, body, contentType)
}

func (s *Service) fetchURL(ctx context.Context, task *config.DownloadTask, rawURL string) ([]byte, string, error) {
	client := s.HTTPClient
	if client == nil {
		client = buildClient(task)
	}

	attempts := 1
	if task.Retry.Enabled {
		attempts = task.Retry.Config.Retries + 1
		if attempts <= 0 {
			attempts = 1
		}
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		body, ct, code, err := s.doOnce(ctx, client, task, rawURL)
		if err == nil {
			return body, ct, nil
		}
		lastErr = err

		if !task.Retry.Enabled || !shouldRetry(task.Retry.Config, err, code) || attempt == attempts {
			break
		}
		delay := backoffDelay(task.Retry.Config, attempt)
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, "", ctx.Err()
		case <-t.C:
		}
	}

	if shouldStopOnHTTPCode(task, codeFromError(lastErr)) {
		return nil, "", fmt.Errorf("stop_all_tasks.on_http_code matched %d for %s: %w", codeFromError(lastErr), rawURL, lastErr)
	}

	return nil, "", lastErr
}

func (s *Service) doOnce(ctx context.Context, client *http.Client, task *config.DownloadTask, rawURL string) ([]byte, string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", 0, fmt.Errorf("build request: %w", err)
	}
	if err := applyAuth(req, task.Auth); err != nil {
		return nil, "", 0, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", 0, classifyNetworkError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		io.Copy(io.Discard, resp.Body)
		return nil, "", resp.StatusCode, httpStatusError{Code: resp.StatusCode, Err: fmt.Errorf("HTTPError: request failed with status %d", resp.StatusCode)}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.Header.Get("Content-Type"), resp.StatusCode, fmt.Errorf("read response body: %w", err)
	}
	return body, resp.Header.Get("Content-Type"), resp.StatusCode, nil
}

func buildClient(task *config.DownloadTask) *http.Client {
	timeout := 30 * time.Second
	if task.Timeout > 0 {
		timeout = time.Duration(task.Timeout) * time.Second
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: task.Security.AllowSelfSigned}}

	c := &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}

	followRedirects := true
	if task.Security.FollowRedirects != nil {
		followRedirects = *task.Security.FollowRedirects
	}
	if !followRedirects {
		c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	} else {
		c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if !security.IsURLAllowed(req.URL.String(), task.Security.AllowInsecureHTTP, task.Security.AllowedDomains) {
				return fmt.Errorf("redirect target blocked by security policy: %s", req.URL.String())
			}
			return nil
		}
	}
	return c
}

func applyAuth(req *http.Request, auth *config.AuthConfig) error {
	if auth == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(auth.Type)) {
	case "basic":
		req.SetBasicAuth(auth.Username, auth.Password)
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+auth.Token)
	case "api_key":
		keyIn := strings.ToLower(strings.TrimSpace(auth.KeyIn))
		switch keyIn {
		case "", "header":
			req.Header.Set(auth.KeyName, auth.KeyValue)
		case "query":
			q := req.URL.Query()
			q.Set(auth.KeyName, auth.KeyValue)
			req.URL.RawQuery = q.Encode()
		case "cookie":
			req.AddCookie(&http.Cookie{Name: auth.KeyName, Value: auth.KeyValue})
		default:
			return fmt.Errorf("unsupported auth.key_in: %s", auth.KeyIn)
		}
	}
	return nil
}

func shouldRetry(cfg config.RetryConfig, err error, statusCode int) bool {
	if err != nil {
		if cfg.OnRuntimeError {
			return true
		}
	}
	if statusCode != 0 {
		if len(cfg.OnHTTPCode) == 0 {
			return statusCode >= 400
		}
		for _, c := range cfg.OnHTTPCode {
			if c == statusCode {
				return true
			}
		}
	}
	return false
}

func shouldStopAllTasks(task *config.DownloadTask, err error) bool {
	if task == nil {
		return false
	}
	if task.StopAllTasks.OnRuntimeError {
		return true
	}
	return shouldStopOnHTTPCode(task, codeFromError(err))
}

func shouldStopOnHTTPCode(task *config.DownloadTask, statusCode int) bool {
	if task == nil || statusCode == 0 {
		return false
	}
	for _, c := range task.StopAllTasks.OnHTTPCode {
		if c == statusCode {
			return true
		}
	}
	return false
}

func codeFromError(err error) int {
	if err == nil {
		return 0
	}
	var hsErr httpStatusError
	if !errors.As(err, &hsErr) {
		return 0
	}
	return hsErr.Code
}

type httpStatusError struct {
	Code int
	Err  error
}

func (e httpStatusError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("HTTPError: request failed with status %d", e.Code)
}

func (e httpStatusError) Unwrap() error {
	return e.Err
}

func backoffDelay(cfg config.RetryConfig, attempt int) time.Duration {
	delay := cfg.Delay
	if delay <= 0 {
		delay = 1
	}
	d := time.Duration(delay) * time.Second
	if cfg.ExponentialBackoff != nil && *cfg.ExponentialBackoff {
		if attempt <= 1 {
			return d
		}
		mult := 1 << (attempt - 1)
		return d * time.Duration(mult)
	}
	return d
}

func classifyNetworkError(err error) error {
	if err == nil {
		return nil
	}
	if ue, ok := err.(*url.Error); ok {
		if ue.Timeout() {
			return fmt.Errorf("TimeoutError: %w", err)
		}
		if _, ok := ue.Err.(net.Error); ok {
			return fmt.Errorf("ConnectionError: %w", err)
		}
		return fmt.Errorf("ConnectionError: %w", err)
	}
	return err
}

type atomicStop struct {
	mu sync.Mutex
	v  bool
}

func (a *atomicStop) Store(v bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.v = v
}

func (a *atomicStop) Load() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.v
}

func dedupeStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
