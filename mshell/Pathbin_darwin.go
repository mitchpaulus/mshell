package main

import (
	"os"
	"os/signal"
	"strings"
	"sort"
	"os/exec"
	"fmt"
	"syscall"
	"golang.org/x/term"
	"golang.org/x/sys/unix"
)

const nullDevice = "/dev/null"

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
	pbm := PathBinManager{}
	pbm.Update()
	return &pbm
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
		innerList.Items[1] = &MShellString {pbm.binaryPaths[key]}
		l.Items[i] = innerList
	}

	return l
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

func (pbm *PathBinManager) Update() {
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

	binMap, err := loadBinMap()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading bin map: %s\n", err)
	} else {
		for name, path := range binMap {
			binaryPaths[name] = path
		}
	}

	pbm.currPath = currPathSlice
	pbm.binaryPaths = binaryPaths

}


func (pbm *PathBinManager) SetupCommand(allArgs []string) (*exec.Cmd) {
	cmd := exec.Command(allArgs[0], allArgs[1:]...)
	// Put subprocess in its own process group so CTRL-C only affects it, not the shell
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	return cmd
}

// IgnoreSignalsForJobControl ignores SIGTTOU and SIGTTIN which would stop the shell
// when it manipulates the foreground process group. Returns a function to restore signals.
func IgnoreSignalsForJobControl() func() {
	signal.Ignore(syscall.SIGTTOU, syscall.SIGTTIN)
	return func() {
		signal.Reset(syscall.SIGTTOU, syscall.SIGTTIN)
	}
}

// SetForegroundProcessGroup makes the given process group the foreground process group
// of the terminal. Returns the previous foreground process group ID so it can be restored.
// IMPORTANT: Call IgnoreSignalsForJobControl() before this to avoid SIGTTOU stopping the shell.
func SetForegroundProcessGroup(ttyFd int, pgid int) (int, error) {
	// Get current foreground process group
	oldPgid, err := unix.IoctlGetInt(ttyFd, unix.TIOCGPGRP)
	if err != nil {
		return 0, err
	}

	// Set new foreground process group
	err = unix.IoctlSetPointerInt(ttyFd, unix.TIOCSPGRP, pgid)
	if err != nil {
		return oldPgid, err
	}

	return oldPgid, nil
}

// RestoreForegroundProcessGroup restores the shell's process group as foreground
// IMPORTANT: Call IgnoreSignalsForJobControl() before this to avoid SIGTTOU stopping the shell.
func RestoreForegroundProcessGroup(ttyFd int) error {
	return unix.IoctlSetPointerInt(ttyFd, unix.TIOCSPGRP, syscall.Getpgrp())
}

// IsTerminal returns true if the file descriptor is connected to a terminal
func IsTerminal(fd int) bool {
	return term.IsTerminal(fd)
}

func IsPathSeparator(c uint8) bool {
	return c == '/'
}

func (s *TermState) UpdateSize() {
	var err error
	s.numCols, s.numRows, err = term.GetSize(s.stdInFd)
	if err != nil {
		fmt.Fprintf(s.f, "Error getting terminal size for FD %d: %s\n", s.stdInFd, err)
	}
}
