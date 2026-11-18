//go:build !linux

package main

// looksCloudOnly is only needed under WSL. On Windows, cloud only files are
// found from their file attributes.
func looksCloudOnly(path string) bool {
	return false
}

// isWSL reports whether msh is running under WSL. Tests replace it.
var isWSL = func() bool {
	return false
}
