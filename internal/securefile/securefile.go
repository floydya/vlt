package securefile

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const temporaryNameAttempts = 100

type Directory struct {
	root *os.Root
}

func OpenDirectory(path string, create bool) (*Directory, error) {
	if create {
		if err := createPrivateDirectories(path); err != nil {
			return nil, filesystemError("create application directory", err)
		}
	}

	before, err := os.Lstat(path)
	if err != nil {
		return nil, filesystemError("inspect application directory", err)
	}
	if err := validateDirectory(before); err != nil {
		return nil, err
	}

	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, filesystemError("open application directory", err)
	}
	closeOnError := func(err error) (*Directory, error) {
		_ = root.Close()
		return nil, err
	}

	opened, err := root.Stat(".")
	if err != nil {
		return closeOnError(filesystemError("inspect opened application directory", err))
	}
	if err := validateDirectory(opened); err != nil {
		return closeOnError(err)
	}
	if !os.SameFile(before, opened) {
		return closeOnError(errors.New("application directory changed while opening"))
	}

	current, err := os.Lstat(path)
	if err != nil {
		return closeOnError(filesystemError("reinspect application directory", err))
	}
	if err := validateDirectory(current); err != nil {
		return closeOnError(err)
	}
	if !os.SameFile(opened, current) {
		return closeOnError(errors.New("application directory changed while opening"))
	}
	return &Directory{root: root}, nil
}

func (d *Directory) OpenRegular(name string) (*os.File, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	before, err := d.root.Lstat(name)
	if err != nil {
		return nil, filesystemError("inspect metadata file", err)
	}
	if err := validateFile(before); err != nil {
		return nil, err
	}

	file, err := d.root.OpenFile(name, os.O_RDONLY, 0)
	if err != nil {
		return nil, filesystemError("open metadata file", err)
	}
	closeOnError := func(err error) (*os.File, error) {
		_ = file.Close()
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		return closeOnError(filesystemError("inspect opened metadata file", err))
	}
	if err := validateFile(opened); err != nil {
		return closeOnError(err)
	}
	if !os.SameFile(before, opened) {
		return closeOnError(errors.New("metadata file changed while opening"))
	}

	current, err := d.root.Lstat(name)
	if err != nil {
		return closeOnError(filesystemError("reinspect metadata file", err))
	}
	if err := validateFile(current); err != nil {
		return closeOnError(err)
	}
	if !os.SameFile(opened, current) {
		return closeOnError(errors.New("metadata file changed while opening"))
	}
	return file, nil
}

func (d *Directory) ValidateTarget(name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	info, err := d.root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return filesystemError("inspect metadata file", err)
	}
	return validateFile(info)
}

func (d *Directory) CreateTemp(prefix string) (*os.File, string, error) {
	if err := validateName(prefix); err != nil {
		return nil, "", err
	}
	for range temporaryNameAttempts {
		var random [12]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, "", errors.New("generate temporary metadata name failed")
		}
		name := "." + prefix + "-" + hex.EncodeToString(random[:]) + ".tmp"
		file, err := d.root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", filesystemError("create temporary metadata file", err)
		}
		if err := file.Chmod(0o600); err != nil {
			_ = file.Close()
			_ = d.root.Remove(name)
			return nil, "", filesystemError("set temporary metadata permissions", err)
		}
		info, statErr := file.Stat()
		if statErr != nil {
			_ = file.Close()
			_ = d.root.Remove(name)
			return nil, "", filesystemError("inspect temporary metadata file", statErr)
		}
		if err := validateFile(info); err != nil {
			_ = file.Close()
			_ = d.root.Remove(name)
			return nil, "", err
		}
		return file, name, nil
	}
	return nil, "", errors.New("create temporary metadata file: unique name unavailable")
}

func (d *Directory) Replace(temporaryName, targetName string) error {
	if err := validateName(temporaryName); err != nil {
		return err
	}
	if err := validateName(targetName); err != nil {
		return err
	}
	temporary, err := d.root.Lstat(temporaryName)
	if err != nil {
		return filesystemError("inspect temporary metadata file", err)
	}
	if err := validateFile(temporary); err != nil {
		return err
	}
	if err := d.ValidateTarget(targetName); err != nil {
		return err
	}
	if err := d.root.Rename(temporaryName, targetName); err != nil {
		return filesystemError("replace metadata file", err)
	}
	replaced, err := d.root.Lstat(targetName)
	if err != nil {
		return filesystemError("inspect replaced metadata file", err)
	}
	if err := validateFile(replaced); err != nil {
		return err
	}
	if !os.SameFile(temporary, replaced) {
		return errors.New("metadata file changed during replacement")
	}
	return nil
}

func (d *Directory) Remove(name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	if err := d.root.Remove(name); err != nil {
		return filesystemError("remove temporary metadata file", err)
	}
	return nil
}

func (d *Directory) Sync() error {
	return syncDirectory(d.root)
}

func (d *Directory) Close() error {
	return d.root.Close()
}

func validateDirectory(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("application directory must not be a symbolic link")
	}
	if !info.IsDir() {
		return errors.New("application directory must be a directory")
	}
	return validatePrivate(info, "application directory")
}

func validateFile(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("metadata file must not be a symbolic link")
	}
	if !info.Mode().IsRegular() {
		return errors.New("metadata file must be a regular file")
	}
	return validatePrivate(info, "metadata file")
}

func validateName(name string) error {
	if name == "" || name == "." || filepath.Base(name) != name {
		return errors.New("metadata file name is invalid")
	}
	return nil
}

func createPrivateDirectories(path string) error {
	_, err := os.Lstat(path)
	if err == nil {
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(path)
	if parent != path {
		if err := createPrivateDirectories(parent); err != nil {
			return err
		}
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil
		}
		return err
	}
	return os.Chmod(path, 0o700)
}

func filesystemError(operation string, err error) error {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%s: %w", operation, fs.ErrNotExist)
	case errors.Is(err, fs.ErrExist):
		return fmt.Errorf("%s: %w", operation, fs.ErrExist)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("%s: %w", operation, fs.ErrPermission)
	default:
		return errors.New(operation + " failed")
	}
}
