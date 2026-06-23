package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRun_NoTasks(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "download.yaml")
	if err := os.WriteFile(cfg, []byte("downloads: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	var errOut strings.Builder
	exitCode := run([]string{"-config", cfg}, &out, &errOut)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr=%s", exitCode, errOut.String())
	}
	if !strings.Contains(out.String(), "no download tasks configured") {
		t.Fatalf("expected no tasks message, got %q", out.String())
	}
}

func TestRun_TaskFailureSetsExitCode1(t *testing.T) {
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
	exitCode := run([]string{"-config", cfg}, &out, &errOut)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(errOut.String(), "[error]") {
		t.Fatalf("expected errors in stderr, got %q", errOut.String())
	}
	if !strings.Contains(out.String(), "summary:") {
		t.Fatalf("expected summary in stdout, got %q", out.String())
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
	exitCode := run([]string{"-config", cfg}, &out, &errOut)
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
	exitCode := run([]string{"-config", cfg, "-output", "json"}, &out, &errOut)
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
	_ = run([]string{"-config", cfg}, &out, &errOut)

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
	exitCode := run([]string{"-config", cfg}, &out, &errOut)
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

func TestRun_WritesReportFile_NoTasks(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "download.yaml")
	reportPath := filepath.Join(dir, "out", "report.json")
	if err := os.WriteFile(cfg, []byte("downloads: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	var errOut strings.Builder
	exitCode := run([]string{"-config", cfg, "-report-file", reportPath}, &out, &errOut)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, errOut.String())
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
	exitCode := run([]string{"-config", cfg, "-report-file", reportPath}, &out, &errOut)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
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
	start := time.Now()
	exitCode := run([]string{"-config", cfg}, &out, &errOut)
	elapsed := time.Since(start)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr=%s", exitCode, errOut.String())
	}
	if elapsed >= 750*time.Millisecond {
		t.Fatalf("expected parallel task execution to finish in under 750ms, took %s", elapsed)
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
	exitCode := run([]string{"-config", cfg}, &out, &errOut)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(errOut.String(), "[error] warn_only") {
		t.Fatalf("expected promoted warning to be reported as error, got %q", errOut.String())
	}
	if !strings.Contains(out.String(), "warnings=0") {
		t.Fatalf("expected warnings to be promoted and removed from warning count, got %q", out.String())
	}
	if !strings.Contains(out.String(), "errors=") {
		t.Fatalf("expected errors count in summary, got %q", out.String())
	}
}
