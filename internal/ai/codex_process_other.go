//go:build !unix

package ai

import "os/exec"

func setCodexProcessGroup(cmd *exec.Cmd) {}

func killCodexProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
