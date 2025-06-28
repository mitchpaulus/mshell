package main

import (
	"os"
)

func GetHistoryDir() (string, error) {
	// Check LOCALAPPDATA
	var dir string
	localAppData, exists := os.LookupEnv("LOCALAPPDATA")

	if exists {
		// Check that the directory exists
		if stat, err := os.Stat(localAppData); err == nil && stat.IsDir() {
			return dir, nil
		}
	}

	return "", os.ErrNotExist

}
