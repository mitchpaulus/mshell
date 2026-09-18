package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// GetHistoryDir returns the platform history/storage directory path without a trailing separator.
func GetHistoryDir() (string, error) {
	// Check LOCALAPPDATA
	localAppData, exists := os.LookupEnv("LOCALAPPDATA")

	if exists {
		// Check that the directory exists
		if stat, err := os.Stat(localAppData); err == nil && stat.IsDir() {
			// Create dir 'msh' if it doesn't exist
			dir := filepath.Join(localAppData, "msh")
			err := ensureHistoryDir(dir)
			if err != nil {
				return "", err
			}

			return dir, nil
		}
	}

	return "", os.ErrNotExist

}

// Windows privacy comes from the ACL inherited from LOCALAPPDATA, not Unix modes.
func ensureHistoryDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("unexpected history directory type: %s", dir)
	}
	return nil
}

func openHistoryFile(path string, flags int) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("unexpected history file type: %s", path)
	}
	return os.OpenFile(path, flags, 0600)
}
