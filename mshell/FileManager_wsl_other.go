//go:build !linux

package main

// looksCloudOnly is only needed under WSL. On Windows, cloud only files are
// found from their file attributes.
func looksCloudOnly(path string) bool {
	return false
}
