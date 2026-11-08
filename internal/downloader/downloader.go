package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/douglasmiranda/godownload/internal/config"
	"github.com/douglasmiranda/godownload/internal/hooks"
	"github.com/douglasmiranda/godownload/internal/logging"
	"github.com/douglasmiranda/godownload/internal/security"
	"github.com/douglasmiranda/godownload/internal/storage"
	"github.com/douglasmiranda/godownload/internal/urlgen"
)

// maxURLFileBytes bounds the size of a url_gen_file fetched over HTTP.
const maxURLFileBytes = 32 << 20

type URLFileLoader func(path string) ([]string, error)

// TaskProgress reports per-task file download progress for interactive CLI output.
type TaskProgress interface {
	TaskStarted(name string)
	TaskTotal(name string, total int)
	TaskFileDone(name string)
	TaskFinished(name string)
}

type HookRunner interface {
	Run(ctx context.Context, name string, command string, timeoutSeconds int, env map[string]string) hooks.Result
}

type TaskResult struct {
	TaskName string
	Saved    []storage.SaveResult
	// Planned lists the files a dry run would download.
	Planned  []PlannedDownload
	Errors   []error
	Warnings []string
	AbortAll bool
}

type PlannedDownload struct {
	URL  string
	Path string
}

type Service struct {
	// FileLoader overrides how url_gen_file files are read.
	FileLoader   URLFileLoader
	OnAbortAll   func()
	TaskProgress TaskProgress
	HookRunner   HookRunner
	// Logger receives debug messages. Nil disables them.
	Logger *slog.Logger
	// Redact removes secrets from text written to logs and hook result files.
	// Defaults to logging.RedactText.
	Redact func(string) string
	// DryRun fetches pages to discover links but downloads no files and runs
	// no hooks. The files that would be downloaded are reported in
	// TaskResult.Planned.
	DryRun bool
}

func (s *Service) RunTask(ctx context.Context, taskName string, task *config.DownloadTask, workerCfg config.WorkerConfig) TaskResult {
	if task == nil {
		result := TaskResult{TaskName: taskName}
		result.Errors = append(result.Errors, fmt.Errorf("task is nil"))
		return result
	}

	if s.TaskProgress != nil {
		s.TaskProgress.TaskStarted(taskName)
		defer s.TaskProgress.TaskFinished(taskName)
	}

	attempts := 1
	if task.TreatWarningsAsErrors && task.Retry.Enabled && task.Retry.Config.OnRuntimeError {
		attempts = max(task.Retry.Config.Retries+1, 1)
	}

	var result TaskResult
	for attempt := 1; ; attempt++ {
		result = s.runTaskOnce(ctx, taskName, task, workerCfg)
		if len(result.Errors) == 0 {
			return s.finalizeWithHooks(ctx, taskName, task, result)
		}
		if attempt >= attempts || !shouldRetryPromotedWarnings(task, result) {
			break
		}
		s.debugf("task %s: retrying after warnings treated as errors", taskName)
		if err := sleepCtx(ctx, backoffDelay(task.Retry.Config, attempt)); err != nil {
			result.Errors = append(result.Errors, err)
			return s.finalizeWithHooks(ctx, taskName, task, result)
		}
	}

	if hasOnlyPromotedWarningErrors(result.Errors) {
		s.markAbortAll(task, &result, result.Errors[0])
	}
	return s.finalizeWithHooks(ctx, taskName, task, result)
}

func (s *Service) runTaskOnce(ctx context.Context, taskName string, task *config.DownloadTask, workerCfg config.WorkerConfig) TaskResult {
	result := TaskResult{TaskName: taskName}

	concurrency := task.DownloadsPerTaskConcurrency
	if concurrency <= 0 {
		concurrency = workerCfg.DownloadsPerTaskConcurrency
	}
	if concurrency <= 0 {
		concurrency = config.DefaultDownloadsPerTaskConcurrency
	}
	client := newTaskClient(task, concurrency, s.debugf)

	seedURLs, err := s.generateURLs(ctx, client, task)
	if err != nil {
		err = fmt.Errorf("generate urls: %w", err)
		result.Errors = append(result.Errors, err)
		s.markAbortAll(task, &result, err)
		return finalizeTaskResult(task, result)
	}
	seedURLs = dedupeStrings(seedURLs)
	if len(seedURLs) == 0 {
		result.Warnings = append(result.Warnings, "no URLs generated; task skipped")
		return finalizeTaskResult(task, result)
	}

	// Configured and generated URLs are trusted to receive credentials and
	// must satisfy the security policy; violations are errors.
	allowedSeeds := seedURLs[:0]
	for _, u := range seedURLs {
		if !security.IsURLAllowed(u, task.Security.AllowInsecureHTTP, task.Security.AllowedDomains) {
			err := fmt.Errorf("URL blocked by security policy: %s", u)
			result.Errors = append(result.Errors, err)
			if s.markAbortAll(task, &result, err) {
				return finalizeTaskResult(task, result)
			}
			continue
		}
		allowedSeeds = append(allowedSeeds, u)
	}
	client.trustURLs(allowedSeeds...)

	crawl := s.resolveDownloadCandidates(ctx, client, allowedSeeds, task, workerCfg.DefaultAttrMapping, concurrency)
	result.Errors = append(result.Errors, crawl.errors...)
	result.Warnings = append(result.Warnings, crawl.warnings...)
	if crawl.abort != nil {
		s.markAbortAll(task, &result, crawl.abort)
		return finalizeTaskResult(task, result)
	}
	candidateURLs := dedupeStrings(crawl.candidates)
	if len(candidateURLs) == 0 {
		if len(result.Errors) == 0 {
			msg := "no candidate URLs found; task skipped"
			if strings.TrimSpace(task.Selector) != "" {
				msg = "selector produced zero matches"
			}
			if crawl.blocked > 0 {
				msg += fmt.Sprintf(" (%d discovered link(s) skipped by security policy)", crawl.blocked)
			}
			result.Warnings = append(result.Warnings, msg)
		}
		return finalizeTaskResult(task, result)
	}
	if task.DownloadLimit > 0 && len(candidateURLs) > task.DownloadLimit {
		candidateURLs = candidateURLs[:task.DownloadLimit]
	}

	if s.TaskProgress != nil {
		s.TaskProgress.TaskTotal(taskName, len(candidateURLs))
	}

	claims := &targetClaims{byPath: map[string]string{}}
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
			if s.TaskProgress != nil {
				defer s.TaskProgress.TaskFileDone(taskName)
			}

			target, saveRes, dlErr := s.planDownload(claims, task, downloadURL)
			if dlErr == nil && !saveRes.Skipped {
				if s.DryRun {
					mu.Lock()
					result.Planned = append(result.Planned, PlannedDownload{URL: downloadURL, Path: target.Path()})
					mu.Unlock()
					return
				}
				saveRes, dlErr = s.downloadAndSave(ctx, client, taskName, task, downloadURL, target)
			}
			mu.Lock()
			defer mu.Unlock()
			if dlErr != nil {
				result.Errors = append(result.Errors, dlErr)
				if s.markAbortAll(task, &result, dlErr) {
					stop.Store(true)
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
	if err := ctx.Err(); err != nil && !result.AbortAll && !slices.ContainsFunc(result.Errors, func(e error) bool { return errors.Is(e, err) }) {
		result.Errors = append(result.Errors, err)
	}

	return finalizeTaskResult(task, result)
}

// generateURLs expands the task's URL generators. url_gen_file may point to a
// local file or to an http(s) URL fetched with the task's client.
func (s *Service) generateURLs(ctx context.Context, client *taskClient, task *config.DownloadTask) ([]string, error) {
	loader := s.FileLoader
	if loader == nil {
		loader = func(path string) ([]string, error) {
			if !isHTTPURL(path) {
				return urlgen.LocalFileLoader(path)
			}
			if !security.IsURLAllowed(path, task.Security.AllowInsecureHTTP, task.Security.AllowedDomains) {
				return nil, fmt.Errorf("url_gen_file URL blocked by security policy: %s", path)
			}
			client.trustURLs(path)
			var lines []string
			err := client.get(ctx, path, func(_ *http.Response, body io.Reader) error {
				var err error
				lines, err = urlgen.ReadLines(newLimitReader(body, maxURLFileBytes, fmt.Errorf("url_gen_file is larger than %d bytes", maxURLFileBytes)))
				return err
			})
			return lines, err
		}
	}
	return urlgen.Generate(task, urlgen.FileLoader(loader))
}

func isHTTPURL(v string) bool {
	lower := strings.ToLower(strings.TrimSpace(v))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// targetClaims records which URL a destination file belongs to within one
// task run, so two different URLs resolving to the same file are reported
// instead of one silently skipping or replacing the other.
type targetClaims struct {
	mu     sync.Mutex
	byPath map[string]string
}

func (c *targetClaims) claim(path string, url string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if prev, ok := c.byPath[path]; ok {
		return prev, false
	}
	c.byPath[path] = url
	return "", true
}

// planDownload applies the checks that need no network access and resolves
// where downloadURL would be saved. A returned SaveResult with Skipped set
// means the file must not be downloaded.
func (s *Service) planDownload(claims *targetClaims, task *config.DownloadTask, downloadURL string) (storage.Target, storage.SaveResult, error) {
	if !security.IsURLAllowed(downloadURL, task.Security.AllowInsecureHTTP, task.Security.AllowedDomains) {
		return storage.Target{}, storage.SaveResult{}, fmt.Errorf("URL blocked by security policy: %s", downloadURL)
	}
	if !security.IsAllowedByExtension(downloadURL, task.Security.AllowedFileTypes) {
		return storage.Target{}, storage.SaveResult{}, fmt.Errorf("URL extension blocked by security.allowed_file_types: %s", downloadURL)
	}

	target, err := storage.ResolveTarget(task, downloadURL)
	if err != nil {
		return storage.Target{}, storage.SaveResult{}, err
	}
	if prev, ok := claims.claim(target.Path(), downloadURL); !ok {
		// force_download without override keeps both files by adding a suffix.
		if !task.ForceDownload || task.Save.OverrideIfExists {
			return target, storage.SaveResult{
				Skipped:       true,
				SkipReason:    fmt.Sprintf("skipped because it resolves to the same file as %s (%s); use a save.filename_template with a unique part such as {{.URL}}", prev, target.Name()),
				FilePath:      target.Path(),
				SavedFileName: target.Name(),
				DownloadURL:   downloadURL,
			}, nil
		}
	}
	skipRes, skip, err := storage.SkipIfExists(task, target)
	if err != nil {
		return storage.Target{}, storage.SaveResult{}, err
	}
	if skip {
		skipRes.DownloadURL = downloadURL
		return target, skipRes, nil
	}
	return target, storage.SaveResult{}, nil
}

func (s *Service) downloadAndSave(ctx context.Context, client *taskClient, taskName string, task *config.DownloadTask, downloadURL string, target storage.Target) (storage.SaveResult, error) {
	limit := task.Security.MaxDownloadSize.Int64()
	var saved storage.SaveResult
	err := client.get(ctx, downloadURL, func(resp *http.Response, body io.Reader) error {
		contentType := resp.Header.Get("Content-Type")
		if !security.IsAllowedByContentType(contentType, task.Security.AllowedFileTypes) {
			return fmt.Errorf("response content-type blocked by security.allowed_file_types: %s", contentType)
		}
		if limit > 0 {
			if resp.ContentLength > limit {
				return fmt.Errorf("download size %d exceeds security.max_download_size %d for %s", resp.ContentLength, limit, downloadURL)
			}
			body = newLimitReader(body, limit, permanent(fmt.Errorf("download exceeds security.max_download_size %d for %s", limit, downloadURL)))
		}
		res, err := storage.SaveToTarget(taskName, task, downloadURL, target, body, contentType)
		if err != nil {
			return err
		}
		saved = res
		return nil
	})
	if err != nil {
		return storage.SaveResult{}, err
	}
	s.debugf("saved %s to %s (%d bytes)", downloadURL, saved.FilePath, saved.Size)
	return saved, nil
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

func (s *Service) finalizeWithHooks(ctx context.Context, taskName string, task *config.DownloadTask, result TaskResult) TaskResult {
	if task == nil || s.DryRun || !hasHooks(task.Hooks) {
		return result
	}

	wasSuccessful := len(result.Errors) == 0
	runner := s.HookRunner
	if runner == nil {
		runner = hooks.Runner{Env: task.HookEnv, WorkDir: task.HookConfigDir}
	}

	status := "failure"
	if wasSuccessful {
		status = "success"
	}
	env := map[string]string{
		"GODOWNLOAD_TASK":   taskName,
		"GODOWNLOAD_STATUS": status,
	}
	resultFile, err := s.writeHookResultFile(taskName, status, result)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("task %s: could not write hook result file: %v", taskName, err))
	} else {
		env["GODOWNLOAD_RESULT_FILE"] = resultFile
		defer os.Remove(resultFile)
	}

	initialErrorCount := len(result.Errors)
	for _, hook := range selectedHooks(task.Hooks, wasSuccessful) {
		if hook.command == nil {
			continue
		}
		hookResult := runner.Run(ctx, hook.name, hook.command.Command, task.Hooks.Timeout, env)
		if hookResult.Err == nil {
			continue
		}
		msg := formatHookFailure(taskName, hookResult)
		if task.Hooks.FailTaskIfError {
			err := errors.New(msg)
			result.Errors = append(result.Errors, err)
			s.markAbortAll(task, &result, err)
			continue
		}
		result.Warnings = append(result.Warnings, msg)
	}

	warningsBeforePromotion := len(result.Warnings)
	result = finalizeTaskResult(task, result)
	if len(result.Errors) > initialErrorCount && warningsBeforePromotion > 0 {
		s.markAbortAll(task, &result, result.Errors[len(result.Errors)-1])
	}
	return result
}

type hookResultFile struct {
	Task     string          `json:"task"`
	Status   string          `json:"status"`
	Saved    []hookFileEntry `json:"saved"`
	Skipped  []hookFileEntry `json:"skipped"`
	Warnings []string        `json:"warnings"`
	Errors   []string        `json:"errors"`
}

type hookFileEntry struct {
	Path   string `json:"path"`
	URL    string `json:"url"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

// writeHookResultFile writes the task outcome as JSON so hooks can process
// the downloaded files. The caller removes the file after the hooks ran.
func (s *Service) writeHookResultFile(taskName string, status string, result TaskResult) (string, error) {
	out := hookResultFile{
		Task:     taskName,
		Status:   status,
		Saved:    []hookFileEntry{},
		Skipped:  []hookFileEntry{},
		Warnings: []string{},
		Errors:   []string{},
	}
	for _, saved := range result.Saved {
		entry := hookFileEntry{Path: saved.FilePath, URL: s.redact(saved.DownloadURL), Size: saved.Size, SHA256: saved.SHA256}
		if saved.Skipped {
			out.Skipped = append(out.Skipped, entry)
		} else {
			out.Saved = append(out.Saved, entry)
		}
	}
	for _, w := range result.Warnings {
		out.Warnings = append(out.Warnings, s.redact(w))
	}
	for _, e := range result.Errors {
		out.Errors = append(out.Errors, s.redact(e.Error()))
	}

	f, err := os.CreateTemp("", "godownload-result-*.json")
	if err != nil {
		return "", err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

type hookSelection struct {
	name    string
	command *config.HookCommand
}

func selectedHooks(hooksCfg config.HooksConfig, wasSuccessful bool) []hookSelection {
	out := make([]hookSelection, 0, 2)
	if wasSuccessful {
		out = append(out, hookSelection{name: "on_success", command: hooksCfg.OnSuccess})
	} else {
		out = append(out, hookSelection{name: "on_failure", command: hooksCfg.OnFailure})
	}
	out = append(out, hookSelection{name: "on_complete", command: hooksCfg.OnComplete})
	return out
}

func hasHooks(hooksCfg config.HooksConfig) bool {
	return hooksCfg.OnComplete != nil || hooksCfg.OnSuccess != nil || hooksCfg.OnFailure != nil
}

func formatHookFailure(taskName string, result hooks.Result) string {
	msg := result.Err.Error()
	if result.Output != "" {
		msg = fmt.Sprintf("%s: %s", msg, result.Output)
	}
	return fmt.Sprintf("task %s %s", taskName, msg)
}

// shouldRetryPromotedWarnings reports whether the whole task should run again
// because its only errors are promoted warnings. Tasks that already saved
// files are not retried: a second run would only find those files and
// report them as existing.
func shouldRetryPromotedWarnings(task *config.DownloadTask, result TaskResult) bool {
	if task == nil || !task.TreatWarningsAsErrors || !task.Retry.Enabled || !task.Retry.Config.OnRuntimeError {
		return false
	}
	for _, saved := range result.Saved {
		if !saved.Skipped {
			return false
		}
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

func (s *Service) markAbortAll(task *config.DownloadTask, result *TaskResult, err error) bool {
	if !shouldStopAllTasks(task, err) {
		return false
	}
	result.AbortAll = true
	if s != nil && s.OnAbortAll != nil {
		s.OnAbortAll()
	}
	return true
}

func shouldStopAllTasks(task *config.DownloadTask, err error) bool {
	if task == nil || err == nil || errors.Is(err, context.Canceled) {
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

func (s *Service) debugf(format string, args ...any) {
	if s == nil || s.Logger == nil {
		return
	}
	s.Logger.Debug(s.redact(fmt.Sprintf(format, args...)))
}

func (s *Service) redact(v string) string {
	if s != nil && s.Redact != nil {
		return s.Redact(v)
	}
	return logging.RedactText(v)
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
