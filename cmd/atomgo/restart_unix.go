//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detachCmd 让子进程脱离父进程组/会话,父进程退出(自重启)后子进程仍能独立存活。
func detachCmd(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}
