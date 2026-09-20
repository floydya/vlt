//go:build windows

package statelock

import (
	"context"
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func acquire(ctx context.Context, file *os.File) error {
	for {
		overlapped := new(windows.Overlapped)
		err := windows.LockFileEx(
			windows.Handle(file.Fd()),
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0,
			1,
			0,
			overlapped,
		)
		if err == nil {
			return nil
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return errors.New("acquire application state lock failed")
		}
		if err := waitForRetry(ctx); err != nil {
			return err
		}
	}
}

func release(file *os.File) error {
	if err := windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, new(windows.Overlapped)); err != nil {
		return errors.New("release application state lock failed")
	}
	return nil
}
