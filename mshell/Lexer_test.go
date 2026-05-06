package main

import (
	"testing"
)

// Phase 10 prep: type-checker keyword reservations.
func TestTypeCheckerKeywords(t *testing.T) {
	cases := []struct {
		input string
		want  TokenType
	}{
		{"as", AS},
		{"type", TYPE},
		{"try", TRY},
		{"fail", FAIL_KEYWORD},
		{"pure", PURE},
		// Make sure neighbors still tokenize as before.
		{"true", TRUE},
		{"false", FALSE},
		{"float", TYPEFLOAT},
		// And that variant prefixes don't get swallowed.
		{"types", LITERAL},
		{"asx", LITERAL},
		{"trying", LITERAL},
		{"failed", LITERAL},
		{"purest", LITERAL},
	}
	for _, tc := range cases {
		l := NewLexer(tc.input, nil)
		toks, _ := l.Tokenize()
		if len(toks) < 1 {
			t.Errorf("%q: no tokens produced", tc.input)
			continue
		}
		if toks[0].Type != tc.want {
			t.Errorf("%q: got %s, want %s", tc.input, toks[0].Type, tc.want)
		}
	}
}

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
