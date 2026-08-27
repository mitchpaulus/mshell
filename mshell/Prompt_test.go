package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReadPromptLine(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "newline terminated", input: "hello\n", want: "hello"},
		{name: "crlf terminated", input: "hello\r\n", want: "hello"},
		{name: "eof without newline", input: "hello", want: "hello"},
		{name: "empty eof", input: "", want: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readPromptLine(strings.NewReader(tc.input))
			if (err != nil) != tc.wantErr {
				t.Fatalf("error mismatch: got %v wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("line mismatch: got %q want %q", got, tc.want)
			}
		})
	}
}

func TestReadPromptFromTTYNoTTY(t *testing.T) {
	originalOpenPromptTTY := openPromptTTYFunc
	openPromptTTYFunc = func() (*promptTTYIO, error) {
		return nil, errors.New("no tty")
	}
	t.Cleanup(func() {
		openPromptTTYFunc = originalOpenPromptTTY
	})

	_, err := readPromptFromTTY("Enter value: ")
	if err == nil {
		t.Fatalf("expected readPromptFromTTY to fail when no tty is available")
	}
}

func TestReadPromptFromTTYWritesPromptAndReadsLine(t *testing.T) {
	inputReader, inputWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create input pipe: %v", err)
	}

	outputReader, outputWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create output pipe: %v", err)
	}

	_, err = inputWriter.WriteString("typed response\n")
	if err != nil {
		t.Fatalf("failed to write input data: %v", err)
	}
	_ = inputWriter.Close()

	originalOpenPromptTTY := openPromptTTYFunc
	openPromptTTYFunc = func() (*promptTTYIO, error) {
		return &promptTTYIO{
			input:  inputReader,
			output: outputWriter,
		}, nil
	}
	t.Cleanup(func() {
		openPromptTTYFunc = originalOpenPromptTTY
		_ = inputReader.Close()
		_ = outputReader.Close()
	})

	line, err := readPromptFromTTY("Enter value: ")
	if err != nil {
		t.Fatalf("readPromptFromTTY returned unexpected error: %v", err)
	}
	if line != "typed response" {
		t.Fatalf("line mismatch: got %q want %q", line, "typed response")
	}

	promptBytes, err := io.ReadAll(outputReader)
	if err != nil {
		t.Fatalf("failed reading prompt output: %v", err)
	}
	if string(promptBytes) != "Enter value: " {
		t.Fatalf("prompt mismatch: got %q want %q", string(promptBytes), "Enter value: ")
	}
}

func TestStreamIsTerminalReturnsFalseForAbstractStreams(t *testing.T) {
	if streamIsTerminal(&bytes.Buffer{}, os.Stdout) {
		t.Fatal("an in-memory byte stream must not be reported as a terminal")
	}
}

func TestStreamIsTerminalReturnsFalseForPipe(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	defer reader.Close()
	defer writer.Close()

	if streamIsTerminal(reader, os.Stdin) {
		t.Fatal("a pipe reader must not be reported as a terminal")
	}
	if streamIsTerminal(writer, os.Stdout) {
		t.Fatal("a pipe writer must not be reported as a terminal")
	}
}

func openTestTerminal(t *testing.T) *os.File {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("/dev/ptmx test is Linux-specific")
	}

	terminal, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("failed to open /dev/ptmx: %v", err)
	}
	t.Cleanup(func() {
		_ = terminal.Close()
	})
	return terminal
}

func TestStreamIsTerminalReturnsTrueForPTY(t *testing.T) {
	terminal := openTestTerminal(t)
	if !streamIsTerminal(terminal, os.Stdout) {
		t.Fatal("a PTY must be reported as a terminal")
	}
}

func TestStreamIsTerminalFollowsSymlinkTarget(t *testing.T) {
	terminal := openTestTerminal(t)
	tempDir := t.TempDir()

	terminalLink := filepath.Join(tempDir, "terminal-link")
	if err := os.Symlink(terminal.Name(), terminalLink); err != nil {
		t.Fatalf("failed to create terminal symlink: %v", err)
	}
	terminalFile, err := os.OpenFile(terminalLink, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("failed to open terminal symlink: %v", err)
	}
	defer terminalFile.Close()

	if !streamIsTerminal(terminalFile, os.Stdout) {
		t.Fatal("a symlink to a PTY must be reported as a terminal")
	}

	regularPath := filepath.Join(tempDir, "regular-file")
	if err := os.WriteFile(regularPath, nil, 0600); err != nil {
		t.Fatalf("failed to create regular file: %v", err)
	}
	regularLink := filepath.Join(tempDir, "regular-link")
	if err := os.Symlink(regularPath, regularLink); err != nil {
		t.Fatalf("failed to create regular-file symlink: %v", err)
	}
	regularFile, err := os.OpenFile(regularLink, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("failed to open regular-file symlink: %v", err)
	}
	defer regularFile.Close()

	if streamIsTerminal(regularFile, os.Stdout) {
		t.Fatal("a symlink to a regular file must not be reported as a terminal")
	}
}
