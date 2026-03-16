package main

import (
	"strings"
	"testing"
)

func TestParseFile_LexerErrorStopsParsing(t *testing.T) {
	input := "`design\\"
	lexer := NewLexer(input, nil)
	parser := NewMShellParser(lexer)

	_, err := parser.ParseFile()
	if err == nil {
		t.Fatal("Expected parse error from lexer, got nil")
	}
	if !strings.Contains(err.Error(), "Unterminated path") {
		t.Fatalf("Expected unterminated path error, got: %s", err.Error())
	}
}
