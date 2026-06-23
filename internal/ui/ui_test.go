package ui

import (
	"strings"
	"testing"
)

func TestPrinter_LegacyOutputNoColor(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder
	p := New(&out, &errOut, Options{Interactive: false})

	p.PrintLegacyTaskLine("my_task", 2, 1, 0, 0)
	p.PrintLegacyWarning("my_task", "already exists")
	p.PrintLegacyError("my_task", "download failed")
	p.PrintLegacySummary(1, 2, 1, 0, 0)

	stdout := out.String()
	if !strings.Contains(stdout, "task my_task: saved=2 skipped=1 warnings=0 errors=0") {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if !strings.Contains(stdout, "summary: tasks=1 saved=2 skipped=1 warnings=0 errors=0") {
		t.Fatalf("unexpected summary: %q", stdout)
	}

	stderr := errOut.String()
	if !strings.Contains(stderr, "[warning] my_task: already exists") {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if !strings.Contains(stderr, "[error] my_task: download failed") {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
}

func TestPrinter_ResultsTableNoColor(t *testing.T) {
	var out strings.Builder
	var errOut strings.Builder
	p := New(&out, &errOut, Options{Interactive: false})

	rows := []TaskRow{
		{Name: "good", Saved: 2, Skipped: 0, Warnings: 0, Errors: 0},
		{Name: "bad", Saved: 0, Skipped: 0, Warnings: 0, Errors: 1, Failed: true},
	}
	details := []TaskDetails{
		{Name: "bad", Warnings: []string{"already exists"}, Errors: []string{"domain not allowed"}},
	}

	p.PrintResultsTable(rows, details, 2, 2, 0, 1, 1, true, "bad")

	got := out.String()
	for _, want := range []string{
		"TASK",
		"good",
		"bad",
		"ok",
		"failed",
		"Summary: 2 tasks | 2 saved | 0 skipped | 1 warnings | 1 errors",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "[warning]") || strings.Contains(got, "[error]") || strings.Contains(got, "stop_all_tasks") {
		t.Fatalf("expected diagnostics to stay out of stdout, got:\n%s", got)
	}
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("expected no ANSI codes, got: %q", got)
	}

	stderr := errOut.String()
	for _, want := range []string{
		"[warning] bad: already exists",
		"[error] bad: domain not allowed",
		"stop_all_tasks triggered by task bad; aborting remaining tasks",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("expected stderr to contain %q, got:\n%s", want, stderr)
		}
	}
}
