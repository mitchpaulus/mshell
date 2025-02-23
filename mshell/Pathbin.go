package main
import (
	"os"
	"strings"
)

type PathBinManager struct {
	currPath []string
	index int
	binaryPaths map[string]string
}

func NewPathBinManager() *PathBinManager {
	// Get the current path from the environment
	currPath, exists := os.LookupEnv("PATH")
	var currPathSlice []string
	if !exists {
		currPathSlice = make([]string, 0)
	} else {
		currPathSlice = strings.Split(currPath, ":")
	}

	return &PathBinManager{
		currPath: currPathSlice,
		index: 0,
		binaryPaths: make(map[string]string),
	}
}

func (pbm *PathBinManager) Lookup(binName string) (string, bool) {
	path, exists := pbm.binaryPaths[binName]
	if exists {
		return path, true
	}

	// Else, check the current index vs. the length of the path items. Start searching.
	if pbm.index >= len(pbm.currPath) {
		return "", false
	}

	for i := pbm.index; i < len(pbm.currPath); i++ {
		pathItem := pbm.currPath[i]
		// Stat the directory, look for executables
		dir, err := os.Open(pathItem)
		if err != nil {
			continue
		}

		files, err := dir.Readdir(0)
		if err != nil {
			continue
		}

		// First add all the executables to the binaryPaths map
		// If already exists, skip
		for _, file := range files {
			if file.Mode() & 0111 != 0 {
				_, exists := pbm.binaryPaths[file.Name()]
				if !exists {
					pbm.binaryPaths[file.Name()] = pathItem
				}
			}
		}

		// Now check if the binary exists
		path, exists := pbm.binaryPaths[binName]
		if exists {
			pbm.index = i + 1
			return path, true
		}
	}

	return "", false
}
