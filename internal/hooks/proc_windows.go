//go:build windows

package hooks

import "os/exec"

// killProcessGroupOnCancel keeps the default behavior on Windows: the hook
// process is killed on timeout and cmd.WaitDelay stops waiting for children.
func killProcessGroupOnCancel(cmd *exec.Cmd) {}
