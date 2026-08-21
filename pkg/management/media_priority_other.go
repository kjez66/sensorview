//go:build !linux

package management

import (
	"os"
	"os/exec"
)

func configureMediaCommand(_ *exec.Cmd) {}

func lowerMediaPriority(_ *os.Process) {}
