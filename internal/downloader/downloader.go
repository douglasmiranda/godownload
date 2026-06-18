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
	"sync/atomic"
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
	if task == nil {
		result := TaskResult{TaskName: taskName}
		result.Errors = append(result.Errors, fmt.Errorf("task is nil"))
		return result
	}

	attempts := 1
	if task.TreatWarningsAsErrors && task.Retry.Enabled && task.Retry.Config.OnRuntimeError {
		attempts = task.Retry.Config.Retries + 1
		if attempts <= 0 {
			attempts = 1
		}
	}

	var last TaskResult
	for attempt := 1; attempt <= attempts; attempt++ {
		result := s.runTaskOnce(ctx, taskName, task, workerCfg)
		last = result

		if len(result.Errors) == 0 {
			return result
		}

		if shouldRetryPromotedWarnings(task, result, attempt, attempts) {
			delay := backoffDelay(task.Retry.Config, attempt)
			t := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				t.Stop()
				result.Errors = append(result.Errors, ctx.Err())
				return result
			case <-t.C:
			}
			continue
		}

		if hasOnlyPromotedWarningErrors(result.Errors) && shouldStopAllTasks(task, result.Errors[0]) {
			result.AbortAll = true
		}
		return result
	}

	if hasOnlyPromotedWarningErrors(last.Errors) && shouldStopAllTasks(task, last.Errors[0]) {
		last.AbortAll = true
	}
	return last
}

func (s *Service) runTaskOnce(ctx context.Context, taskName string, task *config.DownloadTask, workerCfg config.WorkerConfig) TaskResult {
	result := TaskResult{TaskName: taskName}

	loader := s.FileLoader
	if loader == nil {
		loader = urlgen.LocalFileLoader
	}
	seedURLs, err := urlgen.Generate(task, urlgen.FileLoader(loader))
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("generate urls: %w", err))
		return finalizeTaskResult(task, result)
	}
	if len(seedURLs) == 0 {
		result.Warnings = append(result.Warnings, "no URLs generated; task skipped")
		return finalizeTaskResult(task, result)
	}

	// Build the HTTP client once per task: its configuration (timeout, TLS,
	// redirect policy) is task-level, and reusing a single client/transport
	// lets connections be pooled across the task's downloads.
	client := s.HTTPClient
	if client == nil {
		client = buildClient(task)
	}

	candidateURLs, warn, err := s.resolveDownloadCandidates(ctx, client, seedURLs, task, workerCfg.DefaultAttrMapping)
	if warn != "" {
		result.Warnings = append(result.Warnings, warn)
	}
	if err != nil {
		result.Errors = append(result.Errors, err)
		return finalizeTaskResult(task, result)
	}
	if len(candidateURLs) == 0 {
		result.Warnings = append(result.Warnings, "no candidate URLs found; task skipped")
		return finalizeTaskResult(task, result)
	}
	if task.DownloadLimit > 0 && len(candidateURLs) > task.DownloadLimit {
		candidateURLs = candidateURLs[:task.DownloadLimit]
	}

	concurrency := task.DownloadsPerTaskConcurrency
	if concurrency <= 0 {
		concurrency = workerCfg.DownloadsPerTaskConcurrency
	}
	if concurrency <= 0 {
		concurrency = config.DefaultDownloadsPerTaskConcurrency
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var stop atomic.Bool

downloadLoop:
	for _, u := range candidateURLs {
		if stop.Load() {
			break
		}
		select {
		case <-ctx.Done():
			break downloadLoop
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(downloadURL string) {
			defer wg.Done()
			defer func() { <-sem }()

			saveRes, dlErr := s.downloadAndSave(ctx, client, taskName, task, downloadURL)
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

	return finalizeTaskResult(task, result)
}

func finalizeTaskResult(task *config.DownloadTask, result TaskResult) TaskResult {
	if task == nil || !task.TreatWarningsAsErrors || len(result.Warnings) == 0 {
		return result
	}

	for _, w := range result.Warnings {
		result.Errors = append(result.Errors, warningAsError{msg: w})
	}
	result.Warnings = nil

	return result
}

func shouldRetryPromotedWarnings(task *config.DownloadTask, result TaskResult, attempt int, attempts int) bool {
	if task == nil || !task.TreatWarningsAsErrors || !task.Retry.Enabled || !task.Retry.Config.OnRuntimeError {
		return false
	}
	if attempt >= attempts {
		return false
	}
	return hasOnlyPromotedWarningErrors(result.Errors)
}

func hasOnlyPromotedWarningErrors(errs []error) bool {
	if len(errs) == 0 {
		return false
	}
	for _, err := range errs {
		var promoted warningAsError
		if !errors.As(err, &promoted) {
			return false
		}
	}
	return true
}

type warningAsError struct {
	msg string
}

func (w warningAsError) Error() string {
	return fmt.Sprintf("warning treated as error: %s", w.msg)
}

func (s *Service) resolveDownloadCandidates(ctx context.Context, client *http.Client, seedURLs []string, task *config.DownloadTask, defaultAttrMapping map[string]string) ([]string, string, error) {
	if strings.TrimSpace(task.Selector) == "" {
		return seedURLs, "", nil
	}
	maxDepth := task.MaxDepth
	if maxDepth < 1 {
		maxDepth = 1
	}

	frontier := append([]string{}, seedURLs...)
	visited := map[string]struct{}{}
	all := []string{}

	for depth := 1; depth <= maxDepth; depth++ {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		nextFrontier := []string{}
		for _, pageURL := range frontier {
			if err := ctx.Err(); err != nil {
				return nil, "", err
			}
			if _, seen := visited[pageURL]; seen {
				continue
			}
			visited[pageURL] = struct{}{}

			body, _, err := s.fetchURL(ctx, client, task, pageURL)
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
			if depth < maxDepth {
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

func (s *Service) downloadAndSave(ctx context.Context, client *http.Client, taskName string, task *config.DownloadTask, downloadURL string) (storage.SaveResult, error) {
	if !security.IsURLAllowed(downloadURL, task.Security.AllowInsecureHTTP, task.Security.AllowedDomains) {
		return storage.SaveResult{}, fmt.Errorf("URL blocked by security policy: %s", downloadURL)
	}
	if !security.IsAllowedByExtension(downloadURL, task.Security.AllowedFileTypes) {
		return storage.SaveResult{}, fmt.Errorf("URL extension blocked by security.allowed_file_types: %s", downloadURL)
	}

	body, contentType, err := s.fetchURL(ctx, client, task, downloadURL)
	if err != nil {
		return storage.SaveResult{}, err
	}
	if !security.IsAllowedByContentType(contentType, task.Security.AllowedFileTypes) {
		return storage.SaveResult{}, fmt.Errorf("response content-type blocked by security.allowed_file_types: %s", contentType)
	}

	return storage.Save(taskName, task, downloadURL, body, contentType)
}

func (s *Service) fetchURL(ctx context.Context, client *http.Client, task *config.DownloadTask, rawURL string) ([]byte, string, error) {
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

	contentType := resp.Header.Get("Content-Type")
	fileLimit := task.Security.MaxDownloadSize.Int64()
	memLimit := task.Security.MaxInMemorySize.Int64()

	// Reject early using the server-declared size when it is available.
	if fileLimit > 0 && resp.ContentLength > fileLimit {
		io.Copy(io.Discard, resp.Body)
		return nil, contentType, resp.StatusCode, fmt.Errorf("download size %d exceeds security.max_download_size %d for %s", resp.ContentLength, fileLimit, rawURL)
	}
	if memLimit > 0 && resp.ContentLength > memLimit {
		io.Copy(io.Discard, resp.Body)
		return nil, contentType, resp.StatusCode, fmt.Errorf("download size %d exceeds security.max_in_memory_size %d for %s", resp.ContentLength, memLimit, rawURL)
	}

	readCap := minPositive(fileLimit, memLimit)
	if readCap <= 0 {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, contentType, resp.StatusCode, fmt.Errorf("read response body: %w", err)
		}
		return body, contentType, resp.StatusCode, nil
	}

	// Read one extra byte so an oversized body is detectable even when
	// Content-Length is unknown (e.g. chunked responses).
	body, err := io.ReadAll(io.LimitReader(resp.Body, readCap+1))
	if err != nil {
		return nil, contentType, resp.StatusCode, fmt.Errorf("read response body: %w", err)
	}
	if int64(len(body)) > readCap {
		if fileLimit > 0 && int64(len(body)) > fileLimit {
			return nil, contentType, resp.StatusCode, fmt.Errorf("download exceeds security.max_download_size %d for %s", fileLimit, rawURL)
		}
		return nil, contentType, resp.StatusCode, fmt.Errorf("download exceeds security.max_in_memory_size %d for %s", memLimit, rawURL)
	}
	return body, contentType, resp.StatusCode, nil
}

// minPositive returns the smallest value greater than zero, or 0 when none of
// the provided values are positive (meaning "no limit").
func minPositive(values ...int64) int64 {
	min := int64(0)
	for _, v := range values {
		if v <= 0 {
			continue
		}
		if min == 0 || v < min {
			min = v
		}
	}
	return min
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
