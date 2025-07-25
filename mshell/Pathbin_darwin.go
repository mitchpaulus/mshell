package main

import (
	"os"
	"strings"
	"sort"
	"os/exec"
	"fmt"
	"golang.org/x/term"
)

type PathBinManager struct {
	currPath []string
	index int
	binaryPaths map[string]string
}

func (pbm *PathBinManager) Matches(search string) []string {
	var matches []string

	// Iterate over the binaryPaths map and find matches
	for binName := range pbm.binaryPaths {
		// Case insensitive search
		if strings.HasPrefix(strings.ToLower(binName), strings.ToLower(search)) {
			matches = append(matches, binName)
		}
	}

	// Sort the matches
	sort.Strings(matches)

	return matches
}


func NewPathBinManager() IPathBinManager {
	// Get the current path from the environment
	currPath, exists := os.LookupEnv("PATH")
	var currPathSlice []string
	if !exists {
		currPathSlice = make([]string, 0)
	} else {
		currPathSlice = strings.Split(currPath, ":")
	}

	var binaryPaths = make(map[string]string)

	for _, pathItem := range currPathSlice {
		// Stat the directory, look for executables
		// dir, err := os.Open(pathItem)
		// if err != nil {
			// continue
		// }
		files, err := os.ReadDir(pathItem)
		if err != nil {
			continue
		}

		// First add all the executables to the binaryPaths map
		// If already exists, skip
		for _, file := range files {
			if file.IsDir() {
				continue
			}

			fileInfo, err := file.Info()
			if err != nil {
				continue
			}

			if fileInfo.Mode() & 0111 != 0 {
				_, exists := binaryPaths[file.Name()]
				if !exists {
					binaryPaths[file.Name()] = pathItem + "/" + file.Name()
				}
			}
		}
	}

	return &PathBinManager{
		currPath: currPathSlice,
		index: 0,
		binaryPaths: binaryPaths,
	}
}

func (pbm *PathBinManager) DebugList() string {
	var sb strings.Builder

	// Write tab separated key -> value pairs, sorted by key
	keys := make([]string, 0, len(pbm.binaryPaths))
	for key := range pbm.binaryPaths {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		sb.WriteString(key)
		sb.WriteString("\t")
		sb.WriteString(pbm.binaryPaths[key])
		sb.WriteString("\n")
	}

	return sb.String()
}

func (pbm *PathBinManager) ExecuteArgs(execPath string) ([]string, error) {
	return []string{execPath}, nil
}

func (pbm *PathBinManager) IsExecutableFile(path string) bool {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return false
	}
	return (fileInfo.Mode() & 0111) != 0
}


func (pbm *PathBinManager) Lookup(binName string) (string, bool) {
	path, exists := pbm.binaryPaths[binName]
	if exists {
		return path, true
	}
	return "", false

	// // Else, check the current index vs. the length of the path items. Start searching.
	// if pbm.index >= len(pbm.currPath) {
		// return "", false
	// }

	// for i := pbm.index; i < len(pbm.currPath); i++ {
		// pathItem := pbm.currPath[i]
		// // Stat the directory, look for executables
		// // dir, err := os.Open(pathItem)
		// // if err != nil {
			// // continue
		// // }

		// files, err := os.ReadDir(pathItem)
		// if err != nil {
			// continue
		// }

		// // First add all the executables to the binaryPaths map
		// // If already exists, skip
		// for _, file := range files {
			// if file.IsDir() {
				// continue
			// }

			// fileInfo, err := file.Info()
			// if err != nil {
				// continue
			// }

			// if fileInfo.Mode() & 0111 != 0 {
				// _, exists := pbm.binaryPaths[file.Name()]
				// if !exists {
					// pbm.binaryPaths[file.Name()] = pathItem
				// }
			// }
		// }

		// // Now check if the binary exists
		// path, exists := pbm.binaryPaths[binName]
		// if exists {
			// pbm.index = i + 1
			// return path, true
		// }
	// }

	// return "", false
}

func (pbm *PathBinManager) SetupCommand(allArgs []string) (*exec.Cmd) {
	// No-op for Linux, as we don't need to set the PATH
	return exec.Command(allArgs[0], allArgs[1:]...)
}

func IsPathSeparator(c uint8) bool {
	return c == '/'
}

func (s *TermState) UpdateSize() {
	var err error
	s.numCols, s.numRows, err = term.GetSize(s.stdInFd)
	if err != nil {
		fmt.Fprintf(s.f, "Error getting terminal size for FD %d: %s\n", s.stdOutFd, err)
	}
}
