//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package securefile

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type fakeFileInfo struct {
	mode os.FileMode
	uid  uint32
}

func (f fakeFileInfo) Name() string       { return "metadata" }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeFileInfo) Sys() any           { return &syscall.Stat_t{Uid: f.uid} }

func TestValidatePrivateRejectsUnexpectedOwnership(t *testing.T) {
	currentUID := uint32(os.Geteuid())
	if err := validatePrivate(fakeFileInfo{mode: 0o600, uid: currentUID}, "metadata file"); err != nil {
		t.Fatalf("validatePrivate(current owner) error = %v", err)
	}
	err := validatePrivate(fakeFileInfo{mode: 0o600, uid: currentUID + 1}, "metadata file")
	if err == nil || !strings.Contains(err.Error(), "current user") {
		t.Fatalf("validatePrivate(other owner) error = %v, want ownership rejection", err)
	}
}

func TestCreateUsesExactPrivatePermissionsWithRestrictiveUmask(t *testing.T) {
	root := t.TempDir()
	previousUmask := syscall.Umask(0o777)
	defer syscall.Umask(previousUmask)

	directoryPath := filepath.Join(root, "vlt")
	directory, err := OpenDirectory(directoryPath, true)
	if err != nil {
		t.Fatalf("OpenDirectory() error = %v", err)
	}
	defer func() { _ = directory.Close() }()
	directoryInfo, err := os.Stat(directoryPath)
	if err != nil {
		t.Fatalf("Stat(directory) error = %v", err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("directory permissions = %04o, want 0700", got)
	}

	file, name, err := directory.CreateTemp("profiles.json")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	defer func() { _ = directory.Remove(name) }()
	defer func() { _ = file.Close() }()
	fileInfo, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat(file) error = %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("file permissions = %04o, want 0600", got)
	}
}

func TestOpenRegularRejectsFIFO(t *testing.T) {
	directoryPath := filepath.Join(t.TempDir(), "vlt")
	if err := os.Mkdir(directoryPath, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	path := filepath.Join(directoryPath, "profiles.json")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("Mkfifo() error = %v", err)
	}
	directory, err := OpenDirectory(directoryPath, false)
	if err != nil {
		t.Fatalf("OpenDirectory() error = %v", err)
	}
	defer func() { _ = directory.Close() }()

	if _, err := directory.OpenRegular("profiles.json"); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("OpenRegular() error = %v, want FIFO rejection", err)
	}
}

func TestOpenDirectoryErrorsDoNotExposePath(t *testing.T) {
	const canary = "TOKEN-CANARY-DO-NOT-LEAK"
	path := filepath.Join(t.TempDir(), canary)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.Chmod(path, 0o750); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	_, err := OpenDirectory(path, false)
	if err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("OpenDirectory() error = %v, want permissions rejection", err)
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatalf("OpenDirectory() error exposed path: %q", err)
	}
}

func TestDirectoryHandleSurvivesApplicationPathReplacement(t *testing.T) {
	root := t.TempDir()
	applicationPath := filepath.Join(root, "vlt")
	if err := os.Mkdir(applicationPath, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	directory, err := OpenDirectory(applicationPath, false)
	if err != nil {
		t.Fatalf("OpenDirectory() error = %v", err)
	}
	defer func() { _ = directory.Close() }()

	movedPath := filepath.Join(root, "moved")
	if err := os.Rename(applicationPath, movedPath); err != nil {
		t.Fatalf("Rename(application directory) error = %v", err)
	}
	if err := os.Mkdir(applicationPath, 0o700); err != nil {
		t.Fatalf("Mkdir(replacement directory) error = %v", err)
	}

	temporary, temporaryName, err := directory.CreateTemp("profiles.json")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	if _, err := temporary.Write([]byte("held-directory")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := temporary.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := directory.Replace(temporaryName, "profiles.json"); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(movedPath, "profiles.json"))
	if err != nil {
		t.Fatalf("ReadFile(moved directory) error = %v", err)
	}
	if !bytes.Equal(contents, []byte("held-directory")) {
		t.Fatalf("moved directory contents = %q", contents)
	}
	if _, err := os.Stat(filepath.Join(applicationPath, "profiles.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("replacement directory Stat() error = %v, want missing metadata", err)
	}
}

func TestReplaceRejectsTargetChangedToSymlink(t *testing.T) {
	root := t.TempDir()
	applicationPath := filepath.Join(root, "vlt")
	if err := os.Mkdir(applicationPath, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	directory, err := OpenDirectory(applicationPath, false)
	if err != nil {
		t.Fatalf("OpenDirectory() error = %v", err)
	}
	defer func() { _ = directory.Close() }()

	temporary, temporaryName, err := directory.CreateTemp("profiles.json")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	if _, err := temporary.Write([]byte("replacement")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := temporary.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	defer func() { _ = directory.Remove(temporaryName) }()

	outsidePath := filepath.Join(root, "outside.json")
	original := []byte("outside")
	if err := os.WriteFile(outsidePath, original, 0o600); err != nil {
		t.Fatalf("WriteFile(outside) error = %v", err)
	}
	if err := os.Symlink(outsidePath, filepath.Join(applicationPath, "profiles.json")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	err = directory.Replace(temporaryName, "profiles.json")
	if err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Replace() error = %v, want symbolic link rejection", err)
	}
	contents, readErr := os.ReadFile(outsidePath)
	if readErr != nil {
		t.Fatalf("ReadFile(outside) error = %v", readErr)
	}
	if !bytes.Equal(contents, original) {
		t.Fatalf("outside contents changed: %q", contents)
	}
}
