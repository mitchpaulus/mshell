package main
import (
	"os"
	"strings"
	"sort"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"golang.org/x/sys/windows"
	"fmt"
)

// Windows CTRL-C handling
// When a subprocess is running, we track its process group ID so we can forward CTRL-C to it.
var (
	foregroundPgidMu    sync.Mutex
	foregroundPgid      uint32 // Process group ID of the current foreground process (0 if none)
	ctrlHandlerInstalled bool
)

const nullDevice = "NUL"

type PathBinManager struct {
	currPath []string
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
	pbm := PathBinManager {}
	pbm.Update()
	return &pbm
}

func (pbm *PathBinManager) Update() {
	// Get the current path from the environment
	currPath, exists := os.LookupEnv("PATH")
	var currPathSlice []string
	if !exists {
		currPathSlice = make([]string, 0)
	} else {
		currPathSlice = strings.Split(currPath, ";")
	}


	// We don't really use the PATHEXT as intended by Windows.
	// So going to hardcode in the extensions we handle above.
	// The other reason to not rely on PATHEXT is that will usually require administrator privelages,
	// and there is some part of mshell existenance that is because I needed programming power
	// without having to call my IT department.
	pathExtsSlice := []string{".EXE", ".CMD", ".BAT", ".COM", ".MSH"}

	// pathExts, exists := os.LookupEnv("PATHEXT")
	// var pathExtsSlice []string
	// if !exists {
		// pathExtsSlice = []string{".EXE", ".CMD", ".BAT", ".COM"}
	// } else {
		// rawExts := strings.Split(pathExts, ";")
		// pathExtsSlice = make([]string, 0, len(rawExts))
		// for _, ext := range rawExts {
			// // Check that the extension starts with a dot and has a minimum length of 2
			// if len(ext) < 2 || ext[0] != '.' {
				// continue
			// }
			// // Convert to uppercase and add to the slice
			// pathExtsSlice = append(pathExtsSlice, strings.ToUpper(ext))
		// }
	// }

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

	binMap, err := loadBinMap()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading bin map: %s\n", err)
	} else {
		for name, path := range binMap {
			binaryPaths[strings.ToUpper(name)] = WinBinaryPath{ FullPath: path, OriginalFileName: name }
		}
	}

	pbm.currPath = currPathSlice
	pbm.binaryPaths = binaryPaths
	pbm.pathExts = pathExtsSlice
}

func (pbm *PathBinManager) DebugList() *MShellList {

	keys := make([]string, 0, len(pbm.binaryPaths))
	for key := range pbm.binaryPaths {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	l := NewList(len(keys))

	for i, key := range keys {
		innerList := NewList(2)
		innerList.Items[0] = &MShellString {key}
		innerList.Items[1] = &MShellString { pbm.binaryPaths[key].FullPath }
		l.Items[i] = innerList
	}

	return l
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

func IsPathSeparator(c uint8) bool {
	// Windows uses backslash as path separator
	return c == '\\' || c == '/'
}


func (s *TermState) UpdateSize() {
	stdout := windows.Handle(os.Stdout.Fd())
	var info windows.ConsoleScreenBufferInfo
	var err error
	err = windows.GetConsoleScreenBufferInfo(stdout, &info)
	if err != nil {
		fmt.Fprintf(s.f, "Error getting console screen buffer info for FD %d: %s\n", stdout, err)
	}
	s.numCols = int(info.Window.Right - info.Window.Left + 1)
	s.numRows = int(info.Window.Bottom - info.Window.Top + 1)
}

// Windows API constants
const (
	CTRL_C_EVENT     = 0
	CTRL_BREAK_EVENT = 1
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleCtrlHandler = kernel32.NewProc("SetConsoleCtrlHandler")
)

// consoleCtrlHandler handles console control events (CTRL-C, CTRL-BREAK, etc.)
// When a child process is running (foregroundPgid != 0), the shell ignores CTRL-C
// so only the child is terminated. The child receives CTRL-C directly from the console.
func consoleCtrlHandler(ctrlType uint32) uintptr {
	if ctrlType == CTRL_C_EVENT || ctrlType == CTRL_BREAK_EVENT {
		foregroundPgidMu.Lock()
		hasChild := foregroundPgid != 0
		foregroundPgidMu.Unlock()

		if hasChild {
			// A child process is running. Return TRUE to indicate we handled the event,
			// which prevents the shell from being terminated. The child process will
			// receive the CTRL-C directly from the console and handle it.
			return 1
		}
	}
	// Return FALSE to let the default handler process the event (terminates the shell)
	return 0
}

// installCtrlHandler installs the console control handler if not already installed
func installCtrlHandler() {
	foregroundPgidMu.Lock()
	defer foregroundPgidMu.Unlock()

	if !ctrlHandlerInstalled {
		// SetConsoleCtrlHandler with add=true (1) adds the handler to the list
		procSetConsoleCtrlHandler.Call(syscall.NewCallback(consoleCtrlHandler), 1)
		ctrlHandlerInstalled = true
	}
}

// IgnoreSignalsForJobControl is a no-op on Windows.
// On Windows, we use a console control handler instead.
func IgnoreSignalsForJobControl() func() {
	return func() {}
}

// SetForegroundProcessGroup marks that a child process is running.
// On Windows, this causes the console control handler to ignore CTRL-C for the shell,
// allowing only the child to be terminated.
func SetForegroundProcessGroup(ttyFd int, pgid int) (int, error) {
	installCtrlHandler()

	foregroundPgidMu.Lock()
	oldPgid := foregroundPgid
	foregroundPgid = uint32(pgid)
	foregroundPgidMu.Unlock()

	return int(oldPgid), nil
}

// RestoreForegroundProcessGroup marks that no child process is running.
// CTRL-C will now terminate the shell again.
func RestoreForegroundProcessGroup(ttyFd int) error {
	foregroundPgidMu.Lock()
	foregroundPgid = 0
	foregroundPgidMu.Unlock()
	return nil
}

// IsTerminal returns true if the file descriptor is connected to a terminal
func IsTerminal(fd int) bool {
	// Check if it's a console handle
	handle := windows.Handle(fd)
	var mode uint32
	err := windows.GetConsoleMode(handle, &mode)
	return err == nil
}
