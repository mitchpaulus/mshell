package main

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const binMapFileName = "msh_bins"

type BinMapEntry struct {
	Name string
	Path string
}

func BinMapPath() (string, error) {
	historyDir, err := GetHistoryDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(historyDir, binMapFileName), nil
}

func parseBinMapLine(line string) (BinMapEntry, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return BinMapEntry{}, false
	}
	spaceIdx := strings.IndexByte(line, ' ')
	if spaceIdx < 0 {
		return BinMapEntry{}, false
	}
	name := strings.TrimSpace(line[:spaceIdx])
	path := strings.TrimSpace(line[spaceIdx+1:])
	if name == "" || path == "" {
		return BinMapEntry{}, false
	}
	return BinMapEntry{Name: name, Path: path}, true
}

func loadBinMap() (map[string]string, error) {
	entries, err := loadBinMapEntries()
	if err != nil {
		return nil, err
	}
	binMap := make(map[string]string, len(entries))
	for _, entry := range entries {
		binMap[entry.Name] = entry.Path
	}
	return binMap, nil
}

func loadBinMapEntries() ([]BinMapEntry, error) {
	path, err := BinMapPath()
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []BinMapEntry{}, nil
		}
		return nil, err
	}
	defer file.Close()

	entries := make([]BinMapEntry, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		entry, ok := parseBinMapLine(scanner.Text())
		if ok {
			entries = append(entries, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func readBinMapLines() (string, []string, error) {
	path, err := BinMapPath()
	if err != nil {
		return "", nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return path, []string{}, nil
		}
		return "", nil, err
	}
	defer file.Close()

	lines := make([]string, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return "", nil, err
	}
	return path, lines, nil
}

func ensureBinMapFile() (string, error) {
	path, err := BinMapPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_CREATE, 0644)
	if err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return path, nil
}

func binNameMatches(a string, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
