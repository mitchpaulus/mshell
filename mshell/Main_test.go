package main

import (
	"testing"
	"os"
)

func TestHistory(t *testing.T) {
	path := "test.mshell_history"
	err := WriteToHistory(os.Getenv("HOME"), "echo hello", path)
}
