package hooks

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunner_RunSuccessUsesWorkDirEnvAndShell(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "hook.out")

	res := Runner{
		WorkDir: dir,
		Env: map[string]string{
			"SHELL":   "/bin/sh",
			"MESSAGE": "hello",
			"STATUS":  "from-task-env",
		},
	}.Run(context.Background(), "on_success", `printf '%s:%s:%s' "$MESSAGE" "$STATUS" "$PWD" > hook.out`, 0, map[string]string{"STATUS": "from-extra-env"})

	if res.Err != nil {
		t.Fatalf("unexpected hook error: %v output=%q", res.Err, res.Output)
	}
	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("expected hook output file: %v", err)
	}
	want := "hello:from-extra-env:" + dir
	if got := string(b); got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestRunner_RunIgnoresProcessShell(t *testing.T) {
	t.Setenv("SHELL", filepath.Join(t.TempDir(), "missing-shell"))

	res := Runner{}.Run(context.Background(), "on_success", "echo ok", 0, nil)
	if res.Err != nil {
		t.Fatalf("expected the default shell to run the hook, got %v output=%q", res.Err, res.Output)
	}
	if res.Output != "ok" {
		t.Fatalf("expected output %q, got %q", "ok", res.Output)
	}
}

func TestRunner_RunNonZeroCapturesOutput(t *testing.T) {
	res := Runner{Shell: "/bin/sh"}.Run(context.Background(), "on_failure", "echo nope; exit 7", 0, nil)
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

func TestRunner_RunTimeoutKillsChildProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process groups are not used on Windows")
	}
	// The child sleep keeps the output pipe open. Without killing the whole
	// process group, Run would wait for it to finish.
	start := time.Now()
	res := Runner{Shell: "/bin/sh"}.Run(context.Background(), "on_complete", "sleep 30 & sleep 30", 1, nil)
	elapsed := time.Since(start)

	if !res.TimedOut || res.Err == nil {
		t.Fatalf("expected timed out result: %+v", res)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("expected hook to stop shortly after the timeout, took %s", elapsed)
	}
}

func TestShellCommand(t *testing.T) {
	cases := []struct {
		shell    string
		wantArgs []string
	}{
		{"/bin/bash", []string{"-c", "echo"}},
		{`C:\Windows\System32\cmd.exe`, []string{"/C", "echo"}},
		{"pwsh", []string{"-NoProfile", "-Command", "echo"}},
	}
	for _, tc := range cases {
		program, args := shellCommand(tc.shell, "echo")
		if program != tc.shell || strings.Join(args, " ") != strings.Join(tc.wantArgs, " ") {
			t.Errorf("shellCommand(%q) = %q %q, want %q", tc.shell, program, args, tc.wantArgs)
		}
	}
}
