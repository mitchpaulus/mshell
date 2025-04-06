package main

import (
	"testing"
	"os"
)

func TestHistory(t *testing.T) {
	path := "test.mshell_history"
	WriteToHistory(os.Getenv("HOME"), "echo hello", path)
}
