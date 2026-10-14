//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// detachCmd 让子进程脱离父进程的控制台进程组(CREATE_NEW_PROCESS_GROUP),
// 父进程退出(自重启)后子进程仍能独立存活。
func detachCmd(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags = 0x00000200 // CREATE_NEW_PROCESS_GROUP
}
