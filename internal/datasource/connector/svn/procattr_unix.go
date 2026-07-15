//go:build unix

package svn

import (
	"os/exec"
	"syscall"
)

// setProcAttr puts the svn child into its own process group so a timeout can
// kill the whole group — including the ssh grandchild spawned for svn+ssh://.
func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup SIGKILLs the entire process group of cmd. Covers the ssh
// grandchild that exec.CommandContext alone would orphan on timeout.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
