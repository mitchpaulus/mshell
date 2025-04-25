package main
import (
	"os"
	"strings"
	"sort"
)

type PathBinManager struct {
	currPath []string
	index int
	binaryPaths map[string]string
	pathExts []string
}

func (pbm *PathBinManager) Lookup(binName string) (string, bool) {

	// Check if the binary name is already in the map
	if path, exists := pbm.binaryPaths[strings.ToUpper(binName)]; exists {
		return path, true
	}

	// Loop through extensions and check if the binary name with each extension exists
	for _, ext := range pbm.pathExts {
		if path, exists := pbm.binaryPaths[strings.ToUpper(binName+ext)]; exists {
			return path, true
		}
	}

	return "", false
}

func (pbm *PathBinManager) IsExecutableFile(path string) bool {
	fileInfo, err := os.Stat(path)

	if err != nil {
		return false
	}

	// Check if the file is executable, based on the file extensions
	for _, ext := range pbm.pathExts {
		if strings.HasSuffix(strings.ToUpper(fileInfo.Name()), ext) {
			return true
		}
	}

	return false
}




func (pbm *PathBinManager) ExecuteArgs(execPath string) ([]string, error) {
	// Check extension
	if strings.HasSuffix(strings.ToUpper(execPath), ".EXE") {
		return []string{execPath}, nil
	}

	if strings.HasSuffix(strings.ToUpper(execPath), ".CMD") {
		return []string{execPath}, nil
	}

	if strings.HasSuffix(strings.ToUpper(execPath), ".BAT") {
		return []string{execPath}, nil
	}

	if strings.HasSuffix(strings.ToUpper(execPath), ".COM") {
		return []string{execPath}, nil
	}

	if strings.HasSuffix(strings.ToUpper(execPath), ".MSH") {
		// Find the msh.exe executable path
		mshPath, exists := pbm.Lookup("MSH.EXE")
		if !exists {
			return nil, os.ErrNotExist
		}

		return []string{ mshPath, execPath }, nil
	}

	return nil, os.ErrNotExist
}

func NewPathBinManager() IPathBinManager {
	// Get the current path from the environment
	currPath, exists := os.LookupEnv("PATH")
	var currPathSlice []string
	if !exists {
		currPathSlice = make([]string, 0)
	} else {
		currPathSlice = strings.Split(currPath, ";")
	}

	pathExts, exists := os.LookupEnv("PATHEXT")
	var pathExtsSlice []string
	if !exists {
		pathExtsSlice = []string{".EXE", ".CMD", ".BAT", ".COM"}
	} else {
		rawExts := strings.Split(pathExts, ";")
		pathExtsSlice = make([]string, 0, len(rawExts))
		for _, ext := range rawExts {
			// Check that the extension starts with a dot and has a minimum length of 2
			if len(ext) < 2 || ext[0] != '.' {
				continue
			}
			// Convert to uppercase and add to the slice
			pathExtsSlice = append(pathExtsSlice, strings.ToUpper(ext))
		}
	}

	binaryPaths := make(map[string]string)

	for _, path := range currPathSlice {
		// Look for executables in the current path
		files, err := os.ReadDir(path)
		if err != nil {
			continue
		}

		for _, file := range files {
			if file.IsDir() {
				continue
			}
			fileName := file.Name()
			// Check if the file has a valid extension
			for _, ext := range pathExtsSlice {
				if strings.HasSuffix(strings.ToUpper(fileName), ext) {
					// Add the file to the binary paths map
					binaryPaths[strings.ToUpper(fileName)] = path + "\\" + fileName
					break
				}
			}
		}
	}

	return &PathBinManager{
		currPath: currPathSlice,
		index: 0,
		binaryPaths: binaryPaths,
		pathExts: pathExtsSlice,
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
