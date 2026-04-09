package main

import (
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
			err := os.MkdirAll(dir, 0755)
			if err != nil {
				return "", err
			}

			return dir, nil
		}
	}

	return "", os.ErrNotExist

}
