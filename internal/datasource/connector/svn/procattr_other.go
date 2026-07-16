//go:build !unix

package svn

import "os/exec"

// setProcAttr is a no-op on non-unix platforms; process-group kill is
// unavailable, but svn does not spawn grandchildren outside svn+ssh.
func setProcAttr(cmd *exec.Cmd) {}

// killProcessGroup falls back to killing only the direct child.
func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
