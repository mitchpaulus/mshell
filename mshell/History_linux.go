package main

import (
	"os"
)

func GetHistoryDir() (string, error) {
	// Check XDG_DATA_HOME environment variable
	var dir string
	xdgDataHome, exists := os.LookupEnv("XDG_DATA_HOME")
	if exists {
		// Check that the directory exists
		if stat, err := os.Stat(xdgDataHome); err == nil && stat.IsDir() {
			dir = xdgDataHome
			return dir, nil
		}
	}

	// Use ~/.local/share/msh/ and create it if it doesn't exist. If HOME doesn't exist, return an error.
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	} else {
		dir = homeDir + "/.local/share/msh/"
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", err
		}
	}

	// Return the full path to the history file
	return dir, nil
}
