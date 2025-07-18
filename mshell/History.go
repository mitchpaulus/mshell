package main

import (
	"os"
	"github.com/cespare/xxhash"
	"encoding/binary"
	"bufio"
)

// Returns slice of HistoryItem data, last item is the most recent
func ReadHistory(historyDir string) ([]HistoryItem, error) {
	historyFile := historyDir + "/msh_history"
	history, err := ReadHistoryFile(historyFile)
	if err != nil {
		return nil, err
	}

	commandHashFile := historyDir + "/msh_commands"
	commandHashes, err := ReadHashFile(commandHashFile)
	if err != nil {
		return nil, err
	}

	directoryHashFile := historyDir + "/msh_dirs"
	directoryHashes, err := ReadHashFile(directoryHashFile)
	if err != nil {
		return nil, err
	}

	items := make([]HistoryItem, len(history))
	for i, item := range history {
		command, found := commandHashes[item.CommandxxHash]
		if !found {
			continue // Skip if command hash not found
		}
		directory, found := directoryHashes[item.DirectoryxxHash]
		if !found {
			continue // Skip if directory hash not found
		}
		historyItem := HistoryItem{
			UnixTimeUtc: item.UnixTimeUtc,
			Command:     command,
			Directory:   directory,
		}
		items[i] = historyItem
	}
	return items, nil
}

func SearchHistory(current string, historyData []HistoryItem) string {
	// Loop through backwards, looking for first item with prefix
	for i := len(historyData) - 1; i >= 0; i-- {
		if len(historyData[i].Command) >= len(current) && historyData[i].Command[:len(current)] == current {
			return historyData[i].Command
		}
	}
	return ""
}

type HistoryFileItem struct {
	UnixTimeUtc int64
	CommandxxHash uint64  // 64-bit xxHash of the command
	DirectoryxxHash uint64 // 64-bit xxHash of the directory
}

func ReadHistoryFile(historyFile string) ([]HistoryFileItem, error) {
	var history []HistoryFileItem

	// Open the history file
	file, err := os.Open(historyFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Check that the file is divisible by 24 bits, otherwise it's corrupted
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if stat.Size()%24 != 0 {
		return nil, os.ErrInvalid
	}

	// Read entire file into memory
	data := make([]byte, stat.Size())
	_, err = file.Read(data)
	if err != nil {
		return nil, err
	}

	// Parse the file into HistoryFileItem structs
	for i := 0; i < len(data); i += 24 {
		item := HistoryFileItem{
			UnixTimeUtc:   int64(binary.BigEndian.Uint64(data[i : i+8])),
			CommandxxHash: binary.BigEndian.Uint64(data[i+8 : i+16]),
			DirectoryxxHash: binary.BigEndian.Uint64(data[i+16 : i+24]),
		}
		history = append(history, item)
	}
	return history, nil
}

func ReadHashFile(hashFile string) (map[uint64]string, error) {
	hashes := make(map[uint64]string)

	// Open the command hash file
	file, err := os.Open(hashFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Read the file line by line
	scanner := bufio.NewScanner(file)
	var command string
	for scanner.Scan() {
		command = scanner.Text()
		if len(command) > 0 {
			// Calculate the xxHash of the command
			hash := xxhash.Sum64String(command)
			hashes[hash] = command
		}
	}

	return hashes, nil
}
