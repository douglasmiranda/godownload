package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRun_NoTasks(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "download.yaml")
	reportPath := filepath.Join(dir, "out", "report.json")
	if err := os.WriteFile(cfg, []byte("downloads: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	var errOut strings.Builder
	exitCode := run(context.Background(), []string{"-config", cfg, "-report-file", reportPath}, &out, &errOut)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr=%s", exitCode, errOut.String())
	}
	if !strings.Contains(out.String(), "no download tasks configured") {
		t.Fatalf("expected no tasks message, got %q", out.String())
	}

	b, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("expected report file to be written: %v", err)
	}
	var rr runReport
	if err := json.Unmarshal(b, &rr); err != nil {
		t.Fatalf("expected valid report json: %v", err)
	}
	if rr.TaskCount != 0 {
		t.Fatalf("expected task_count=0, got %d", rr.TaskCount)
	}
	if rr.ReportVersion != reportSchemaVersion {
		t.Fatalf("expected report_version=%q, got %q", reportSchemaVersion, rr.ReportVersion)
	}
}

func TestRun_ReportsAllTasks(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "download.yaml")
	yml := `
downloads:
  z_task:
    url: https://example.com/a.pdf
    security:
      allowed_domains: ["blocked.test"]
      allowed_file_types: ["pdf"]
  a_task:
    url: https://example.com/b.pdf
    security:
      allowed_domains: ["blocked.test"]
      allowed_file_types: ["pdf"]
`
	if err := os.WriteFile(cfg, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	var errOut strings.Builder
	exitCode := run(context.Background(), []string{"-config", cfg}, &out, &errOut)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}

	stdout := out.String()
	if !strings.Contains(stdout, "task a_task") || !strings.Contains(stdout, "task z_task") {
		t.Fatalf("expected both task lines in stdout, got %q", stdout)
	}
	if !strings.Contains(stdout, "errors=2") {
		t.Fatalf("expected summary to report both errors, got %q", stdout)
	}
	if !strings.Contains(errOut.String(), "[error] a_task") || !strings.Contains(errOut.String(), "[error] z_task") {
		t.Fatalf("expected both task errors in stderr, got %q", errOut.String())
	}
}

func TestRun_TasksSelectsMultipleTasksAndReportsSelectedCount(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "download.yaml")
	reportPath := filepath.Join(dir, "report.json")
	yml := `
downloads:
  a_task:
    url: https://example.com/a.pdf
    security:
      allowed_domains: ["blocked.test"]
      allowed_file_types: ["pdf"]
  ignored:
    url: https://example.com/ignored.pdf
    security:
      allowed_domains: ["blocked.test"]
      allowed_file_types: ["pdf"]
  z_task:
    url: https://example.com/z.pdf
    security:
      allowed_domains: ["blocked.test"]
      allowed_file_types: ["pdf"]
`
	if err := os.WriteFile(cfg, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	var errOut strings.Builder
	exitCode := run(context.Background(), []string{
		"-config", cfg,
		"-tasks", " z_task, a_task, z_task ",
		"-report-file", reportPath,
	}, &out, &errOut)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1 from selected tasks, got %d", exitCode)
	}
	if !strings.Contains(out.String(), "task a_task") || !strings.Contains(out.String(), "task z_task") {
		t.Fatalf("expected both selected tasks to run, got %q", out.String())
	}
	if strings.Contains(out.String(), "task ignored") || strings.Contains(errOut.String(), "[error] ignored") {
		t.Fatalf("expected ignored task not to run, stdout=%q stderr=%q", out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "summary: tasks=2") {
		t.Fatalf("expected de-duplicated selected task count in summary, got %q", out.String())
	}

	b, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("expected report file: %v", err)
	}
	var rr runReport
	if err := json.Unmarshal(b, &rr); err != nil {
		t.Fatalf("expected valid report json: %v", err)
	}
	if rr.TaskCount != 2 || len(rr.Tasks) != 2 {
		t.Fatalf("expected report to contain two selected tasks, got %+v", rr)
	}
	if rr.Tasks[0].TaskName != "a_task" || rr.Tasks[1].TaskName != "z_task" {
		t.Fatalf("expected report tasks sorted by name, got %+v", rr.Tasks)
	}
}

func TestRun_TasksRejectsUnknownNames(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "download.yaml")
	yml := `
downloads:
  known:
    url: https://example.com/file.pdf
`
	if err := os.WriteFile(cfg, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	var errOut strings.Builder
	exitCode := run(context.Background(), []string{"-config", cfg, "-tasks", "missing_b,known,missing_a"}, &out, &errOut)
	if exitCode != 2 {
		t.Fatalf("expected exit code 2 for unknown tasks, got %d", exitCode)
	}
	if got := errOut.String(); !strings.Contains(got, "unknown task(s): missing_a, missing_b") {
		t.Fatalf("expected sorted unknown-task error, got %q", got)
	}
	if strings.Contains(out.String(), "task known") {
		t.Fatalf("expected no tasks to run when selection is invalid, got %q", out.String())
	}
}

func TestRun_TasksStillValidatesUnselectedTasks(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "download.yaml")
	yml := `
downloads:
  selected:
    url: https://example.com/file.pdf
  invalid_unselected:
    url: ""
`
	if err := os.WriteFile(cfg, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	var errOut strings.Builder
	exitCode := run(context.Background(), []string{"-config", cfg, "-tasks", "selected"}, &out, &errOut)
	if exitCode != 1 {
		t.Fatalf("expected full config validation to fail, got exit code %d", exitCode)
	}
	if !strings.Contains(errOut.String(), "invalid_unselected.url") {
		t.Fatalf("expected diagnostic for unselected invalid task, got %q", errOut.String())
	}
	if strings.Contains(out.String(), "task selected") {
		t.Fatalf("expected no tasks to run after config validation failure, got %q", out.String())
	}
}

func TestRun_JSONOutput(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "download.yaml")
	yml := `
downloads:
  bad:
    url: https://example.com/file.pdf
    security:
      allowed_domains: ["blocked.test"]
      allowed_file_types: ["pdf"]
`
	if err := os.WriteFile(cfg, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	var errOut strings.Builder
	exitCode := run(context.Background(), []string{"-config", cfg, "-output", "json"}, &out, &errOut)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if errOut.String() != "" {
		t.Fatalf("expected no stderr in json output mode for task logs, got %q", errOut.String())
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out.String()), &decoded); err != nil {
		t.Fatalf("expected valid json output, got error: %v, output=%q", err, out.String())
	}
	if _, ok := decoded["tasks"]; !ok {
		t.Fatalf("expected tasks field in json output: %v", decoded)
	}
	if got, ok := decoded["report_version"].(string); !ok || got != reportSchemaVersion {
		t.Fatalf("expected report_version=%q in json output, got %#v", reportSchemaVersion, decoded["report_version"])
	}
}

func TestRun_RedactsSensitiveURLInErrors(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "download.yaml")
	yml := `
downloads:
  bad:
    url: https://example.com/file.pdf?token=supersecret
    security:
      allowed_domains: ["blocked.test"]
      allowed_file_types: ["pdf"]
`
	if err := os.WriteFile(cfg, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	var errOut strings.Builder
	_ = run(context.Background(), []string{"-config", cfg}, &out, &errOut)

	if !strings.Contains(errOut.String(), "[error] bad") || !strings.Contains(errOut.String(), "example.com/file.pdf") {
		t.Fatalf("expected the blocked URL in stderr, got %q", errOut.String())
	}
	if strings.Contains(errOut.String(), "supersecret") {
		t.Fatalf("expected stderr to redact token, got %q", errOut.String())
	}
}

func TestRun_ConfigValidationErrorShowsDiagnosticDetails(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "download.yaml")
	yml := `
downloads:
  bad:
    url: https://httpbin.org/stream/{{.N}}
    url_gen_range:
      var_name: X
      range_start: 1
      range_end: 1
      step: 1
`
	if err := os.WriteFile(cfg, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	var errOut strings.Builder
	exitCode := run(context.Background(), []string{"-config", cfg}, &out, &errOut)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	stderr := errOut.String()
	if !strings.Contains(stderr, "bad.url_gen_range.var_name") {
		t.Fatalf("expected detailed diagnostic field in stderr, got %q", stderr)
	}
	if !strings.Contains(stderr, "{{.X}}") {
		t.Fatalf("expected helpful template hint in stderr, got %q", stderr)
	}
}

func TestRun_WritesReportFile_WithTaskError(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "download.yaml")
	reportPath := filepath.Join(dir, "report.json")
	yml := `
downloads:
  bad:
    url: https://example.com/file.pdf
    security:
      allowed_domains: ["blocked.test"]
      allowed_file_types: ["pdf"]
`
	if err := os.WriteFile(cfg, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	var errOut strings.Builder
	exitCode := run(context.Background(), []string{"-config", cfg, "-report-file", reportPath}, &out, &errOut)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}

	b, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("expected report file to be written: %v", err)
	}
	var rr runReport
	if err := json.Unmarshal(b, &rr); err != nil {
		t.Fatalf("expected valid report json: %v", err)
	}
	if rr.ErrorTotal == 0 {
		t.Fatalf("expected error_total > 0, got %+v", rr)
	}
	if len(rr.Tasks) == 0 || len(rr.Tasks[0].Errors) == 0 {
		t.Fatalf("expected task errors in report: %+v", rr)
	}
	if rr.ReportVersion != reportSchemaVersion {
		t.Fatalf("expected report_version=%q, got %q", reportSchemaVersion, rr.ReportVersion)
	}
}

func TestRun_TaskConcurrencyExecutesTasksInParallel(t *testing.T) {
	// Each request waits until both have arrived, which only happens when
	// the two tasks run at the same time.
	var arrived atomic.Int32
	bothArrived := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if arrived.Add(1) == 2 {
			close(bothArrived)
		}
		select {
		case <-bothArrived:
		case <-time.After(5 * time.Second):
			http.Error(w, "tasks did not run in parallel", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	dir := t.TempDir()
	cfg := filepath.Join(dir, "download.yaml")
	yml := `
config:
  task_concurrency: 2
  downloads_per_task_concurrency: 1
  security:
    allow_insecure_http: true
  save:
    base_download_dir: ` + dir + `/downloads
downloads:
  first:
    url: ` + server.URL + `/a
    save:
      filename: first.txt
  second:
    url: ` + server.URL + `/b
    save:
      filename: second.txt
`
	if err := os.WriteFile(cfg, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	var errOut strings.Builder
	exitCode := run(context.Background(), []string{"-config", cfg}, &out, &errOut)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr=%s", exitCode, errOut.String())
	}
}

func TestRun_TreatWarningsAsErrors_MakesRunFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><p>no links</p></body></html>"))
	}))
	defer server.Close()

	dir := t.TempDir()
	cfg := filepath.Join(dir, "download.yaml")
	yml := `
downloads:
  warn_only:
    url: ` + server.URL + `/
    selector: "a"
    security:
      allow_insecure_http: true
    treat_warnings_as_errors: true
`
	if err := os.WriteFile(cfg, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	var errOut strings.Builder
	exitCode := run(context.Background(), []string{"-config", cfg}, &out, &errOut)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(errOut.String(), "[error] warn_only") {
		t.Fatalf("expected promoted warning to be reported as error, got %q", errOut.String())
	}
	if !strings.Contains(out.String(), "warnings=0") {
		t.Fatalf("expected warnings to be promoted and removed from warning count, got %q", out.String())
	}
	if !strings.Contains(out.String(), "errors=1") {
		t.Fatalf("expected the promoted warning in the error count, got %q", out.String())
	}
}

func TestRun_HookFailureSetsExitCodeAndReportError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	dir := t.TempDir()
	cfg := filepath.Join(dir, "download.yaml")
	reportPath := filepath.Join(dir, "report.json")
	yml := `
config:
  env:
    SHELL: /bin/sh
downloads:
  hook_fail:
    url: ` + server.URL + `/file.txt
    security:
      allow_insecure_http: true
    save:
      base_download_dir: ` + filepath.Join(dir, "downloads") + `
    hooks:
      fail_task_if_error: true
      on_success:
        command: echo hook-boom; exit 6
`
	if err := os.WriteFile(cfg, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	var errOut strings.Builder
	exitCode := run(context.Background(), []string{"-config", cfg, "-report-file", reportPath}, &out, &errOut)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d stdout=%s stderr=%s", exitCode, out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), "hook-boom") {
		t.Fatalf("expected hook output in stderr, got %q", errOut.String())
	}

	b, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("expected report file: %v", err)
	}
	var rr runReport
	if err := json.Unmarshal(b, &rr); err != nil {
		t.Fatalf("expected valid report json: %v", err)
	}
	if rr.ErrorTotal != 1 || len(rr.Tasks) != 1 || len(rr.Tasks[0].Errors) != 1 {
		t.Fatalf("expected hook failure in report errors, got %+v", rr)
	}
}

func TestRun_HookWarningPromotedByTreatWarningsAsErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	dir := t.TempDir()
	cfg := filepath.Join(dir, "download.yaml")
	yml := `
config:
  env:
    SHELL: /bin/sh
downloads:
  hook_warn:
    url: ` + server.URL + `/file.txt
    security:
      allow_insecure_http: true
    save:
      base_download_dir: ` + filepath.Join(dir, "downloads") + `
    treat_warnings_as_errors: true
    hooks:
      on_success:
        command: echo hook-warning; exit 3
`
	if err := os.WriteFile(cfg, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	var errOut strings.Builder
	exitCode := run(context.Background(), []string{"-config", cfg}, &out, &errOut)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d stdout=%s stderr=%s", exitCode, out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), "warning treated as error") || !strings.Contains(errOut.String(), "hook-warning") {
		t.Fatalf("expected promoted hook warning in stderr, got %q", errOut.String())
	}
	if !strings.Contains(out.String(), "warnings=0") {
		t.Fatalf("expected warning to be promoted out of warning count, got %q", out.String())
	}
}

func writeConfig(t *testing.T, dir string, yml string) string {
	t.Helper()
	path := filepath.Join(dir, "download.yaml")
	if err := os.WriteFile(path, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRun_Version(t *testing.T) {
	var out, errOut strings.Builder
	if code := run(context.Background(), []string{"-version"}, &out, &errOut); code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, errOut.String())
	}
	if !strings.HasPrefix(out.String(), "godownload ") {
		t.Fatalf("expected version line, got %q", out.String())
	}
}

func TestRun_RejectsPositionalArguments(t *testing.T) {
	var out, errOut strings.Builder
	if code := run(context.Background(), []string{"my.yaml"}, &out, &errOut); code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(errOut.String(), "use -config") {
		t.Fatalf("expected hint about -config, got %q", errOut.String())
	}
}

func TestRun_ValidateDoesNotDownload(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	dir := t.TempDir()
	cfg := writeConfig(t, dir, `
downloads:
  one:
    url: `+server.URL+`/file.txt
    security:
      allow_insecure_http: true
    save:
      base_download_dir: `+filepath.Join(dir, "downloads")+`
`)
	var out, errOut strings.Builder
	if code := run(context.Background(), []string{"-config", cfg, "-validate"}, &out, &errOut); code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "config is valid: 1 task(s)") {
		t.Fatalf("expected validation message, got %q", out.String())
	}
	if requests.Load() != 0 {
		t.Fatalf("expected no requests, got %d", requests.Load())
	}
	if _, err := os.Stat(filepath.Join(dir, "downloads")); !os.IsNotExist(err) {
		t.Fatalf("expected no download directory, stat err=%v", err)
	}
}

func TestRun_ValidateReportsInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := writeConfig(t, dir, `
downloads:
  bad:
    url: https://example.com/file.pdf
    retyr: true
`)
	var out, errOut strings.Builder
	if code := run(context.Background(), []string{"-config", cfg, "-validate"}, &out, &errOut); code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "retry") {
		t.Fatalf("expected unknown key diagnostic with suggestion, got %q", errOut.String())
	}
}

func TestRun_DryRunListsFilesWithoutDownloadingOrRunningHooks(t *testing.T) {
	var fileRequests atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<a href="/files/a.txt">a</a><a href="/files/b.txt">b</a>`))
	})
	mux.HandleFunc("/files/", func(w http.ResponseWriter, r *http.Request) {
		fileRequests.Add(1)
		_, _ = w.Write([]byte("content"))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	dir := t.TempDir()
	downloads := filepath.Join(dir, "downloads")
	hookMarker := filepath.Join(dir, "hook-ran")
	cfg := writeConfig(t, dir, `
config:
  env:
    SHELL: /bin/sh
downloads:
  site:
    url: `+server.URL+`/
    selector: a
    security:
      allow_insecure_http: true
    save:
      base_download_dir: `+downloads+`
    hooks:
      on_complete:
        command: touch `+hookMarker+`
`)
	reportPath := filepath.Join(dir, "report.json")
	var out, errOut strings.Builder
	code := run(context.Background(), []string{"-config", cfg, "-dry-run", "-report-file", reportPath}, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stdout=%s stderr=%s", code, out.String(), errOut.String())
	}
	stdout := out.String()
	for _, want := range []string{
		"task site: would_download=2",
		server.URL + "/files/a.txt -> " + filepath.Join(downloads, "a.txt"),
		server.URL + "/files/b.txt -> " + filepath.Join(downloads, "b.txt"),
		"dry-run summary: tasks=1 would_download=2",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in stdout, got %q", want, stdout)
		}
	}
	if fileRequests.Load() != 0 {
		t.Errorf("expected no file requests, got %d", fileRequests.Load())
	}
	if _, err := os.Stat(downloads); !os.IsNotExist(err) {
		t.Errorf("expected no download directory, stat err=%v", err)
	}
	if _, err := os.Stat(hookMarker); !os.IsNotExist(err) {
		t.Errorf("expected hooks not to run, stat err=%v", err)
	}

	b, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var rr runReport
	if err := json.Unmarshal(b, &rr); err != nil {
		t.Fatal(err)
	}
	if !rr.DryRun || rr.PlannedTotal != 2 || rr.SavedTotal != 0 || len(rr.Tasks) != 1 || len(rr.Tasks[0].Planned) != 2 {
		t.Fatalf("unexpected dry-run report: %+v", rr)
	}
}

func TestRun_InterruptedRunExitsWith130AndReportsNotRunTasks(t *testing.T) {
	dir := t.TempDir()
	cfg := writeConfig(t, dir, `
downloads:
  a_task:
    url: https://example.com/a.pdf
  b_task:
    url: https://example.com/b.pdf
`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	reportPath := filepath.Join(dir, "report.json")
	var out, errOut strings.Builder
	if code := run(ctx, []string{"-config", cfg, "-report-file", reportPath}, &out, &errOut); code != exitInterrupted {
		t.Fatalf("expected exit code %d, got %d", exitInterrupted, code)
	}
	if !strings.Contains(errOut.String(), "tasks not run: a_task, b_task") || !strings.Contains(errOut.String(), "interrupted") {
		t.Fatalf("expected not-run tasks and interruption in stderr, got %q", errOut.String())
	}
	b, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var rr runReport
	if err := json.Unmarshal(b, &rr); err != nil {
		t.Fatal(err)
	}
	if !rr.Interrupted || len(rr.NotRun) != 2 {
		t.Fatalf("expected interrupted report with two not-run tasks, got %+v", rr)
	}
}

func TestRun_StopAllTasksReportsNotRunTasks(t *testing.T) {
	dir := t.TempDir()
	cfg := writeConfig(t, dir, `
config:
  task_concurrency: 1
downloads:
  a_first:
    url: https://example.com/file.pdf
    security:
      allowed_domains: ["blocked.test"]
    stop_all_tasks:
      on_runtime_error: true
  b_second:
    url: https://example.com/other.pdf
`)
	var out, errOut strings.Builder
	if code := run(context.Background(), []string{"-config", cfg, "-output", "json"}, &out, &errOut); code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	var rr runReport
	if err := json.Unmarshal([]byte(out.String()), &rr); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if !rr.StopTriggered || len(rr.NotRun) != 1 || rr.NotRun[0] != "b_second" {
		t.Fatalf("expected b_second to be reported as not run, got %+v", rr)
	}
}

func TestRun_RedactsConfiguredSecretValues(t *testing.T) {
	dir := t.TempDir()
	cfg := writeConfig(t, dir, `
downloads:
  bad:
    url: https://example.com/files/my-secret-token-value/file.pdf
    auth:
      type: bearer
      token: my-secret-token-value
    security:
      allowed_domains: ["blocked.test"]
`)
	var out, errOut strings.Builder
	_ = run(context.Background(), []string{"-config", cfg}, &out, &errOut)
	if !strings.Contains(errOut.String(), "[error] bad") {
		t.Fatalf("expected an error for the task, got %q", errOut.String())
	}
	if strings.Contains(out.String()+errOut.String(), "my-secret-token-value") {
		t.Fatalf("expected configured token to be redacted, stdout=%q stderr=%q", out.String(), errOut.String())
	}
}

func TestRun_DebugLogLevelWritesDebugLogs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	dir := t.TempDir()
	cfg := writeConfig(t, dir, `
config:
  log_level: debug
downloads:
  one:
    url: `+server.URL+`/file.txt
    security:
      allow_insecure_http: true
    save:
      base_download_dir: `+filepath.Join(dir, "downloads")+`
`)
	var out, errOut strings.Builder
	if code := run(context.Background(), []string{"-config", cfg}, &out, &errOut); code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "level=DEBUG") || !strings.Contains(errOut.String(), "GET "+server.URL+"/file.txt") {
		t.Fatalf("expected debug logs in stderr, got %q", errOut.String())
	}
}
