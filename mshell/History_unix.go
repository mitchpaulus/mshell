//go:build linux || darwin

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// GetHistoryDir returns the platform history/storage directory path without a trailing separator.
func GetHistoryDir() (string, error) {
	// Check XDG_DATA_HOME environment variable
	var dir string
	xdgDataHome, exists := os.LookupEnv("XDG_DATA_HOME")
	if exists {
		// Check that the directory exists
		if stat, err := os.Stat(xdgDataHome); err == nil && stat.IsDir() {
			dir = filepath.Join(xdgDataHome, "msh")
			if err := ensureHistoryDir(dir); err != nil {
				return "", err
			}
			return dir, nil
		}
	}

	// Use ~/.local/share/msh/ and create it if it doesn't exist. If HOME doesn't exist, return an error.
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	} else {
		dir = filepath.Join(homeDir, ".local", "share", "msh")
		if err := ensureHistoryDir(dir); err != nil {
			return "", err
		}
	}

	// Return the full path to the history file
	return dir, nil
}

// Validate and chmod the descriptor, never a symlink target or foreign file.
func privateHistoryObject(file *os.File, directory bool) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	stat := &unix.Stat_t{}
	if err := unix.Fstat(int(file.Fd()), stat); err != nil {
		return err
	}
	if stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("history storage %s is not owned by the current user", file.Name())
	}
	if (directory && !info.IsDir()) || (!directory && (!info.Mode().IsRegular() || stat.Nlink != 1)) {
		return fmt.Errorf("unexpected history storage type or hard link: %s", file.Name())
	}
	allowed := os.FileMode(0600)
	if directory {
		allowed = 0700
	}
	// Preserve stricter owner permissions while removing group/other access.
	mode := info.Mode().Perm() & allowed
	if info.Mode().Perm() != mode || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		if err := file.Chmod(mode); err != nil {
			return fmt.Errorf("repair history permissions for %s: %w", file.Name(), err)
		}
	}
	return nil
}

func openHistoryDir(dir string) (*os.File, error) {
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open history directory", Path: dir, Err: err}
	}
	file := os.NewFile(uintptr(fd), dir)
	if err := privateHistoryObject(file, true); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func ensureHistoryDir(dir string) error {
	// Parents may be shared XDG directories. Never repair their permissions.
	if err := os.MkdirAll(filepath.Dir(dir), 0755); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	file, err := openHistoryDir(dir)
	if err != nil {
		return err
	}
	return file.Close()
}

func openHistoryFile(path string, flags int) (*os.File, error) {
	dir, err := openHistoryDir(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	// O_NONBLOCK avoids hanging on a FIFO before the type check.
	fd, err := unix.Openat(int(dir.Fd()), filepath.Base(path), flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, &os.PathError{Op: "open history file", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	if err := privateHistoryObject(file, false); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
