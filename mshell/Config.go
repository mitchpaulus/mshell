package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const mshellConfigEnvVar = "MSH_CONFIG"

var loadedConfigDict *MShellParseDict
var loadedConfigPath string

// LoadConfig resolves, reads, and parses the config dictionary.
// Cases:
// - --config PATH: returns parsed dict + path, or error if missing/unreadable/invalid.
// - MSH_CONFIG set: returns parsed dict + path, or error if missing/unreadable/invalid.
// - Default path exists: returns parsed dict + path, or error if unreadable/invalid.
// - Default path missing: returns (nil, "", nil).
func LoadConfig(configFlagPath string) (*MShellParseDict, string, error) {
	configPath, explicit, err := resolveConfigPath(configFlagPath)
	if err != nil {
		return nil, "", err
	}
	if configPath == "" {
		return nil, "", nil
	}

	contents, err := os.ReadFile(configPath)
	if err != nil {
		if !explicit && os.IsNotExist(err) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("config file %s: %w", configPath, err)
	}

	dict, err := parseConfigDict(configPath, string(contents))
	if err != nil {
		return nil, "", err
	}

	return dict, configPath, nil
}

// resolveConfigPath returns the resolved path and whether it was explicitly set.
func resolveConfigPath(configFlagPath string) (string, bool, error) {
	if configFlagPath != "" {
		return configFlagPath, true, nil
	}

	if envValue, ok := os.LookupEnv(mshellConfigEnvVar); ok && envValue != "" {
		return envValue, true, nil
	}

	defaultPath, err := defaultConfigPath()
	if err != nil {
		return "", false, err
	}

	return defaultPath, false, nil
}

// defaultConfigPath returns the XDG or ~/.config default config path.
func defaultConfigPath() (string, error) {
	if xdgConfigHome, ok := os.LookupEnv("XDG_CONFIG_HOME"); ok && xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, "mshell", "config.msh"), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(homeDir, ".config", "mshell", "config.msh"), nil
}

// parseConfigDict parses a single dictionary literal and rejects extra items.
func parseConfigDict(path string, input string) (*MShellParseDict, error) {
	lexer := NewLexer(input, &TokenFile{Path: path})
	parser := NewMShellParser(lexer)
	file, err := parser.ParseFile()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	if len(file.Definitions) != 0 {
		return nil, fmt.Errorf("%s: config file must not contain definitions", path)
	}
	if len(file.Items) != 1 {
		return nil, fmt.Errorf("%s: config file must contain a single dictionary literal", path)
	}

	dict, ok := file.Items[0].(*MShellParseDict)
	if !ok {
		return nil, fmt.Errorf("%s: config file must contain a dictionary literal", path)
	}

	return dict, nil
}
