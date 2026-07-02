package hooks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunner_RunSuccessUsesWorkDirEnvAndShell(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "hook.out")

	res := Runner{
		WorkDir: dir,
		Env: map[string]string{
			"SHELL":   "/bin/sh",
			"MESSAGE": "hello",
		},
	}.Run(context.Background(), "on_success", "printf '%s:%s' \"$MESSAGE\" \"$PWD\" > hook.out", 0)

	if res.Err != nil {
		t.Fatalf("unexpected hook error: %v output=%q", res.Err, res.Output)
	}
	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected hook output file: %v", err)
	}
	got := string(b)
	if !strings.Contains(got, "hello:"+dir) {
		t.Fatalf("expected env and workdir in output, got %q", got)
	}
}

func TestRunner_RunNonZeroCapturesOutput(t *testing.T) {
	res := Runner{Shell: "/bin/sh"}.Run(context.Background(), "on_failure", "echo nope; exit 7", 0)
	if res.Err == nil {
		t.Fatal("expected hook error")
	}
	if res.ExitCode != 7 {
		t.Fatalf("expected exit code 7, got %d", res.ExitCode)
	}
	if !strings.Contains(res.Output, "nope") {
		t.Fatalf("expected captured output, got %q", res.Output)
	}
}

func TestRunner_RunTimeout(t *testing.T) {
	res := Runner{Shell: "/bin/sh"}.Run(context.Background(), "on_complete", "sleep 2", 1)
	if res.Err == nil {
		t.Fatal("expected timeout error")
	}
	if !res.TimedOut {
		t.Fatalf("expected timed out result: %+v", res)
	}
}
