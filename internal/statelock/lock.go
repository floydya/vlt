package statelock

import (
	"context"
	"errors"
	"fmt"
	"time"

	"vlt/internal/securefile"
)

const fileName = "state.lock"

type FileLock struct {
	directory string
}

func New(directory string) *FileLock {
	return &FileLock{directory: directory}
}

func (l *FileLock) WithLock(ctx context.Context, operation func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("wait for application state lock: %w", err)
	}
	if l == nil || l.directory == "" {
		return errors.New("application state lock is not configured")
	}
	if operation == nil {
		return errors.New("application state mutation is not configured")
	}
	directory, err := securefile.OpenDirectory(l.directory, true)
	if err != nil {
		return fmt.Errorf("open application state lock directory: %w", err)
	}
	file, err := directory.OpenOrCreateRegular(fileName)
	if err != nil {
		_ = directory.Close()
		return fmt.Errorf("open application state lock: %w", err)
	}
	if err := acquire(ctx, file); err != nil {
		_ = file.Close()
		_ = directory.Close()
		return err
	}

	operationErr := operation(ctx)
	var cleanupErrors []error
	if err := release(file); err != nil {
		cleanupErrors = append(cleanupErrors, err)
	}
	if err := file.Close(); err != nil {
		cleanupErrors = append(cleanupErrors, errors.New("close application state lock failed"))
	}
	if err := directory.Close(); err != nil {
		cleanupErrors = append(cleanupErrors, errors.New("close application state lock directory failed"))
	}
	return errors.Join(operationErr, errors.Join(cleanupErrors...))
}

func waitForRetry(ctx context.Context) error {
	timer := time.NewTimer(10 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait for application state lock: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}
