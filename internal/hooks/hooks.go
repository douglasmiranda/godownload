package hooks

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const maxCapturedOutputBytes = 32 * 1024

type Runner struct {
	Env     map[string]string
	WorkDir string
	Shell   string
}

type Result struct {
	Name     string
	Command  string
	ExitCode int
	Output   string
	TimedOut bool
	Err      error
}

func (r Runner) Run(ctx context.Context, name string, command string, timeoutSeconds int) Result {
	result := Result{Name: name, Command: command, ExitCode: 0}
	shell := strings.TrimSpace(r.Shell)
	if shell == "" && r.Env != nil {
		shell = strings.TrimSpace(r.Env["SHELL"])
	}
	if shell == "" {
		shell = strings.TrimSpace(os.Getenv("SHELL"))
	}
	if shell == "" {
		shell = "/bin/sh"
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if timeoutSeconds > 0 {
		runCtx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	} else {
		runCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	cmd := exec.CommandContext(runCtx, shell, "-c", command)
	cmd.Dir = r.WorkDir
	cmd.Env = mergeEnv(os.Environ(), r.Env)

	var out limitedBuffer
	out.limit = maxCapturedOutputBytes
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	result.Output = strings.TrimSpace(out.String())
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		result.TimedOut = true
		result.Err = fmt.Errorf("hook %s timed out after %ds", name, timeoutSeconds)
		return result
	}
	if err == nil {
		return result
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		result.Err = fmt.Errorf("hook %s exited with code %d", name, result.ExitCode)
		return result
	}
	result.Err = fmt.Errorf("hook %s failed: %w", name, err)
	return result
}

func mergeEnv(base []string, overrides map[string]string) []string {
	env := append([]string{}, base...)
	index := make(map[string]int, len(env)+len(overrides))
	for i, kv := range env {
		k, _, ok := strings.Cut(kv, "=")
		if ok {
			index[k] = i
		}
	}
	for k, v := range overrides {
		kv := k + "=" + v
		if i, ok := index[k]; ok {
			env[i] = kv
			continue
		}
		index[k] = len(env)
		env = append(env, kv)
	}
	return env
}

type limitedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return len(p), nil
	}
	remaining := b.limit - b.buf.Len()
	if remaining > 0 {
		if len(p) <= remaining {
			_, _ = b.buf.Write(p)
		} else {
			_, _ = b.buf.Write(p[:remaining])
			b.truncated = true
		}
	} else {
		b.truncated = true
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	if !b.truncated {
		return b.buf.String()
	}
	return b.buf.String() + "\n[output truncated]"
}
