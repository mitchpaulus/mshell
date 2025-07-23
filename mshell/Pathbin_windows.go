package main
import (
	"os"
	"strings"
	"sort"
	"os/exec"
	"path/filepath"
	"syscall"
)

type PathBinManager struct {
	currPath []string
	index int
	binaryPaths map[string]WinBinaryPath // Key here is uppercase binary name
	pathExts []string
}

type WinBinaryPath struct {
	OriginalFileName string
	FullPath string
}

func (pbm *PathBinManager) Matches(search string) ([]string) {
	var matches []string
	for binName, winBinaryPath := range pbm.binaryPaths {
		if strings.HasPrefix(binName, strings.ToUpper(search)) {
			matches = append(matches, winBinaryPath.OriginalFileName)
		}
	}
	sort.Strings(matches)
	return matches
}

func (pbm *PathBinManager) Lookup(binName string) (string, bool) {

	// Check if the binary name is already in the map
	if path, exists := pbm.binaryPaths[strings.ToUpper(binName)]; exists {
		return path.FullPath, true
	}

	// Loop through extensions and check if the binary name with each extension exists
	for _, ext := range pbm.pathExts {
		if path, exists := pbm.binaryPaths[strings.ToUpper(binName+ext)]; exists {
			return path.FullPath, true
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
		return []string{"CMD.EXE", "/C", execPath}, nil
	}

	if strings.HasSuffix(strings.ToUpper(execPath), ".BAT") {
		return []string{"CMD.EXE", "/C", execPath}, nil
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

	binaryPaths := make(map[string]WinBinaryPath)

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

			// Check if already exists
			if _, exists := binaryPaths[strings.ToUpper(fileName)]; exists {
				continue
			}

			// Check if the file has a valid extension
			for _, ext := range pathExtsSlice {
				if strings.HasSuffix(strings.ToUpper(fileName), ext) {
					// Add the file to the binary paths map
					binaryPaths[strings.ToUpper(fileName)] = WinBinaryPath{ FullPath: path + "\\" + fileName, OriginalFileName: fileName }
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
		sb.WriteString(pbm.binaryPaths[key].FullPath)
		sb.WriteString("\n")
	}

	return sb.String()
}

func EscapeArgForCmd(arg string) string {
	// For simplicity, just wrap anything that isn't alphanumeric in quotes.
	needsQuotes := false
	for _, r := range arg {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			needsQuotes = true
			break
		}
	}

	if needsQuotes {
		return "\"" + strings.ReplaceAll(arg, "\"", "\"\"") + "\""
	}

	return arg
}

func EscapeForCmd(allArgs []string) string {
	if len(allArgs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(EscapeArgForCmd(allArgs[0]))
	for _, arg := range allArgs[1:] {
		sb.WriteString(" ")
		sb.WriteString(EscapeArgForCmd(arg))
	}
	return sb.String()
}

func (pbm *PathBinManager) SetupCommand(allArgs []string) (*exec.Cmd) {
	// Get basename of the command
	if len(allArgs) == 0 {
		return nil
	}

	execName := filepath.Base(allArgs[0])
	if strings.ToUpper(execName) == "CMD.EXE" {
		// If the command is CMD.EXE, we need to escape the arguments properly
		cmdStr := EscapeForCmd(allArgs)

		cmd := exec.Command("CMD.EXE")
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CmdLine: cmdStr,
		}
		return cmd
	} else {
		return exec.Command(allArgs[0], allArgs[1:]...)
	}
}
