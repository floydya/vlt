//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package vaultexec

import (
	"errors"
	"os/exec"
	"syscall"
)

func signalExitCode(err error) (int, bool) {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 0, false
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return 0, false
	}
	return 128 + int(status.Signal()), true
}
