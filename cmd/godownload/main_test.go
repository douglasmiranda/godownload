package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestRun_SortsTasksDeterministically(t *testing.T) {
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
	_ = run([]string{"-config", cfg}, &out, &errOut)

	stdout := out.String()
	aIdx := strings.Index(stdout, "task a_task")
	zIdx := strings.Index(stdout, "task z_task")
	if aIdx == -1 || zIdx == -1 {
		t.Fatalf("expected both task lines in stdout, got %q", stdout)
	}
	if aIdx > zIdx {
		t.Fatalf("expected a_task line before z_task line, got %q", stdout)
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
