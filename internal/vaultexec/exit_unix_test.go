//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package vaultexec

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestExitCodeUsesSignalConvention(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	command := exec.CommandContext(context.Background(), path, "-test.run=TestVaultExecSignalHelper")
	command.Env = append(os.Environ(), "GO_WANT_VAULTEXEC_SIGNAL_HELPER=1")
	err = command.Run()
	if err == nil {
		t.Fatal("signal helper error = nil, want signal termination")
	}
	if got, want := ExitCode(err), 128+int(syscall.SIGTERM); got != want {
		t.Errorf("ExitCode(error) = %d, want %d", got, want)
	}
}

func TestVaultExecSignalHelper(t *testing.T) {
	if os.Getenv("GO_WANT_VAULTEXEC_SIGNAL_HELPER") != "1" {
		return
	}
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		os.Exit(254)
	}
	select {}
}
