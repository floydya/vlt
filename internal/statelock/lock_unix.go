//go:build darwin || linux

package statelock

import (
	"context"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func acquire(ctx context.Context, file *os.File) error {
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EINTR) {
			return errors.New("acquire application state lock failed")
		}
		if err := waitForRetry(ctx); err != nil {
			return err
		}
	}
}

func release(file *os.File) error {
	if err := unix.Flock(int(file.Fd()), unix.LOCK_UN); err != nil {
		return errors.New("release application state lock failed")
	}
	return nil
}
