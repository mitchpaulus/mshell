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
		wantOk  bool
		wantErr bool
	}{
		{name: "newline terminated", input: "hello\n", want: "hello", wantOk: true},
		{name: "crlf terminated", input: "hello\r\n", want: "hello", wantOk: true},
		{name: "eof without newline", input: "hello", want: "hello", wantOk: true},
		{name: "empty eof", input: "", want: "", wantOk: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, err := readPromptLine(strings.NewReader(tc.input))
			if (err != nil) != tc.wantErr {
				t.Fatalf("error mismatch: got %v wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("line mismatch: got %q want %q", got, tc.want)
			}
			if ok != tc.wantOk {
				t.Fatalf("ok mismatch: got %v want %v", ok, tc.wantOk)
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

	line, ok, err := readPromptFromTTY("Enter value: ")
	if err != nil {
		t.Fatalf("readPromptFromTTY returned unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false when no tty is available")
	}
	if line != "" {
		t.Fatalf("expected empty line when no tty is available, got %q", line)
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

	line, ok, err := readPromptFromTTY("Enter value: ")
	if err != nil {
		t.Fatalf("readPromptFromTTY returned unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
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
