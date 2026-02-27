package main

import (
	"errors"
	"io"
	"os"
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
