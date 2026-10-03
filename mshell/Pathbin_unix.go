//go:build linux || darwin

package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
)

// PathBinManager finds the executables on PATH.
//
// Running a command needs only its own name, so Lookup looks in each PATH
// directory for that one name, and remembers the answer. The full list of
// executables, which completion and binPaths need, is read only when one of
// them asks for it. Reading every PATH directory and every file in it on
// each start took most of the time of a short script.
type PathBinManager struct {
	mu       sync.Mutex
	currPath []string
	// binMap holds the entries of msh_bins.txt, which win over PATH; nil
	// until first needed.
	binMap map[string]string
	// found caches Lookup's answers, an empty path for a name not found.
	found map[string]string
	// binaryPaths maps every executable's name to its full path, once
	// scanned is set.
	binaryPaths map[string]string
	scanned     bool
}

func NewPathBinManager() IPathBinManager {
	pbm := PathBinManager{}
	pbm.Update()
	return &pbm
}

// Update reads PATH again and forgets every answer: the next lookup reads
// the directories, and msh_bins.txt, again.
func (pbm *PathBinManager) Update() {
	pbm.mu.Lock()
	defer pbm.mu.Unlock()
	currPath, exists := os.LookupEnv("PATH")
	if exists {
		pbm.currPath = strings.Split(currPath, ":")
	} else {
		pbm.currPath = nil
	}
	pbm.binMap, pbm.found, pbm.binaryPaths, pbm.scanned = nil, nil, nil, false
}

// loadBinMapLocked reads msh_bins.txt the first time it is needed.
func (pbm *PathBinManager) loadBinMapLocked() {
	if pbm.binMap != nil {
		return
	}
	binMap, err := loadBinMap()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading bin map: %s\n", err)
		binMap = map[string]string{}
	}
	pbm.binMap = binMap
}

// executableAt reports whether path names an executable on PATH: not a
// directory, with an execute bit, judged without following a symlink, as
// the directory scan judges an entry.
func executableAt(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && !info.IsDir() && info.Mode()&0111 != 0
}

func (pbm *PathBinManager) Lookup(binName string) (string, bool) {
	pbm.mu.Lock()
	defer pbm.mu.Unlock()
	if binName == "" || strings.ContainsRune(binName, '/') {
		return "", false
	}
	pbm.loadBinMapLocked()
	if path, ok := pbm.binMap[binName]; ok {
		return path, true
	}
	if pbm.scanned {
		path, ok := pbm.binaryPaths[binName]
		return path, ok
	}
	if path, ok := pbm.found[binName]; ok {
		return path, path != ""
	}
	path := ""
	for _, dir := range pbm.currPath {
		if candidate := dir + "/" + binName; executableAt(candidate) {
			path = candidate
			break
		}
	}
	if pbm.found == nil {
		pbm.found = map[string]string{}
	}
	pbm.found[binName] = path
	return path, path != ""
}

// scanLocked reads every PATH directory into binaryPaths, once.
func (pbm *PathBinManager) scanLocked() {
	if pbm.scanned {
		return
	}
	pbm.loadBinMapLocked()
	binaryPaths := make(map[string]string)
	for _, pathItem := range pbm.currPath {
		files, err := os.ReadDir(pathItem)
		if err != nil {
			continue
		}
		// The first directory with a name wins.
		for _, file := range files {
			if file.IsDir() {
				continue
			}
			fileInfo, err := file.Info()
			if err != nil {
				continue
			}
			if fileInfo.Mode()&0111 != 0 {
				if _, exists := binaryPaths[file.Name()]; !exists {
					binaryPaths[file.Name()] = pathItem + "/" + file.Name()
				}
			}
		}
	}
	for name, path := range pbm.binMap {
		binaryPaths[name] = path
	}
	pbm.binaryPaths, pbm.scanned = binaryPaths, true
}

func (pbm *PathBinManager) Matches(search string) []string {
	pbm.mu.Lock()
	defer pbm.mu.Unlock()
	pbm.scanLocked()
	var matches []string
	for binName := range pbm.binaryPaths {
		// Do case-insensitive search prefix match
		if strings.HasPrefix(strings.ToLower(binName), strings.ToLower(search)) {
			matches = append(matches, binName)
		}
	}
	sort.Strings(matches)
	return matches
}

func (pbm *PathBinManager) DebugList() *MShellList {
	pbm.mu.Lock()
	defer pbm.mu.Unlock()
	pbm.scanLocked()
	keys := make([]string, 0, len(pbm.binaryPaths))
	for key := range pbm.binaryPaths {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	l := NewList(len(keys))
	for i, key := range keys {
		innerList := NewList(2)
		innerList.Items[0] = &MShellString{key}
		innerList.Items[1] = &MShellString{pbm.binaryPaths[key]}
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
