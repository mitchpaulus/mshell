//go:build !linux

package main

// looksCloudOnly is only needed under WSL. On Windows, cloud only files are
// found from their file attributes.
func looksCloudOnly(path string) bool {
	return false
}

// isWSL reports whether msh is running under WSL. It is a variable holding a
// function, not a plain function, so that a test can set it to a function
// returning the answer the test needs: true to test what is done under WSL on
// a machine without WSL, and false so a test run under WSL does not start
// real Windows programs.
var isWSL = func() bool {
	return false
}
