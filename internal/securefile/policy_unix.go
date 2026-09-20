//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package securefile

import (
	"errors"
	"os"
	"syscall"
)

func validatePrivate(info os.FileInfo, subject string) error {
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New(subject + " permissions must not allow group or other access")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New(subject + " ownership could not be verified")
	}
	if int(stat.Uid) != os.Geteuid() {
		return errors.New(subject + " must be owned by the current user")
	}
	return nil
}

func syncDirectory(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return filesystemError("open application directory for sync", err)
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return filesystemError("sync application directory", err)
	}
	if err := directory.Close(); err != nil {
		return filesystemError("close application directory", err)
	}
	return nil
}
