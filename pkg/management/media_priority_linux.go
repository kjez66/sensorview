//go:build linux

package management

import (
	"os"
	"os/exec"
	"syscall"
)

func configureMediaCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func lowerMediaPriority(process *os.Process) {
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, process.Pid, 10)
}
