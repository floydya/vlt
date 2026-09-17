//go:build windows

package vaultexec

func signalExitCode(error) (int, bool) {
	return 0, false
}
