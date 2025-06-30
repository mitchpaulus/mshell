package main

import (
	"os"
	"path/filepath"
)

func GetHistoryDir() (string, error) {
	// Check LOCALAPPDATA
	localAppData, exists := os.LookupEnv("LOCALAPPDATA")

	if exists {
		// Check that the directory exists
		if stat, err := os.Stat(localAppData); err == nil && stat.IsDir() {
			// Create dir 'mshell' if it doesn't exist
			dir := filepath.Join(localAppData, "mshell")
			err := os.MkdirAll(dir, 0755)
			if err != nil {
				return "", err
			}

			return dir, nil
		}
	}

	return "", os.ErrNotExist

}
