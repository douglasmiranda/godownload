//go:build !windows

package hooks

import (
	"os/exec"
	"syscall"
)

// killProcessGroupOnCancel runs the hook in its own process group and kills
// the whole group on timeout, so commands started by the shell (for example
// "sleep" in "sleep 10; echo") do not outlive the hook.
func killProcessGroupOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
