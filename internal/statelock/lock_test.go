package statelock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestFileLockSerializesIndependentInstancesAndHonorsCancellation(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "vlt")
	first := New(directory)
	second := New(directory)
	release := make(chan struct{})
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- first.WithLock(context.Background(), func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	called := false
	err := second.WithLock(ctx, func(context.Context) error {
		called = true
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WithLock() error = %v, want context.DeadlineExceeded", err)
	}
	if called {
		t.Fatal("contending operation ran without acquiring the lock")
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("first WithLock() error = %v", err)
	}
	if err := second.WithLock(context.Background(), func(context.Context) error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("WithLock() after release error = %v", err)
	}
	if !called {
		t.Fatal("operation did not run after lock release")
	}
}

func TestFileLockCreatesPrivateRegularFile(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "vlt")
	if err := New(directory).WithLock(context.Background(), func(context.Context) error { return nil }); err != nil {
		t.Fatalf("WithLock() error = %v", err)
	}
	info, err := os.Lstat(filepath.Join(directory, fileName))
	if err != nil {
		t.Fatalf("Lstat(lock file) error = %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("lock mode = %v, want regular file", info.Mode())
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("lock permissions = %04o, want 0600", info.Mode().Perm())
	}
}

func TestFileLockRejectsUnsafePathWithoutExposingIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses a platform-specific permission policy")
	}
	const canary = "TOKEN-CANARY-DO-NOT-LEAK"
	directory := filepath.Join(t.TempDir(), canary)
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	tests := []struct {
		name  string
		setup func(*testing.T, string)
		want  string
	}{
		{
			name: "permissive file",
			setup: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile(path, nil, 0o640); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
			},
			want: "permissions",
		},
		{
			name: "symbolic link",
			setup: func(t *testing.T, path string) {
				t.Helper()
				target := filepath.Join(filepath.Dir(directory), "outside.lock")
				if err := os.WriteFile(target, nil, 0o600); err != nil {
					t.Fatalf("WriteFile(target) error = %v", err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatalf("Symlink() error = %v", err)
				}
			},
			want: "symbolic link",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(directory, fileName)
			tt.setup(t, path)
			err := New(directory).WithLock(context.Background(), func(context.Context) error { return nil })
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("WithLock() error = %v, want %q", err, tt.want)
			}
			if strings.Contains(err.Error(), canary) {
				t.Fatalf("WithLock() error exposed path: %q", err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatalf("Remove(lock file) error = %v", err)
			}
		})
	}
}
