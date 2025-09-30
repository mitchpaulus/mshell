package main

import (
	"testing"
)

func TestUnterminatedString(t *testing.T) {
	input := `"Hello, world!`
	l := NewLexer(input, nil)
	l.allowUnterminatedString = true

	tokens, _ := l.Tokenize()

	if len(tokens) != 2 {
		t.Logf("Expected 1 token, got %d", len(tokens))
		for i, token := range tokens {
			t.Logf("Token %d: Type=%s, Value='%s'", i, token.Type, token.Lexeme)
		}
		t.Fail()
	}

	if tokens[0].Type != UNFINISHEDSTRING {
		t.Errorf("Expected token type UNFINISHEDSTRING, got %s", tokens[0].Type)
	}
}

func TestUnterminatedSingleQuoteString(t *testing.T) {
	input := `'Hello, world!`
	l := NewLexer(input, nil)
	l.allowUnterminatedString = true

	tokens, _ := l.Tokenize()

	if len(tokens) != 2 {
		t.Logf("Expected 1 token, got %d", len(tokens))
		for i, token := range tokens {
			t.Logf("Token %d: Type=%s, Value='%s'", i, token.Type, token.Lexeme)
		}
		t.Fail()
	}

	if tokens[0].Type != UNFINISHEDSINGLEQUOTESTRING {
		t.Errorf("Expected token type UNFINISHEDSINGLEQUOTESTRING, got %s", tokens[0].Type)
	}
}
