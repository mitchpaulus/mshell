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
		{"tryAs", TRYAS},
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
		{"tryAsX", LITERAL},
		{"tryas", LITERAL},
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

// Base-prefixed integer literals (0o/0x/0b) tokenize as INTEGER, while
// malformed or separator-bearing forms fall back to LITERAL (mshell has no
// digit separators in numeric literals).
func TestBaseIntegerLiterals(t *testing.T) {
	cases := []struct {
		input string
		want  TokenType
	}{
		{"0o644", INTEGER},
		{"0O17", INTEGER},
		{"0x1a4", INTEGER},
		{"0XFF", INTEGER},
		{"0b101", INTEGER},
		{"0B0", INTEGER},
		{"-0o10", INTEGER},
		{"42", INTEGER},
		// Malformed / not octal-hex-bin: stay literals.
		{"0o", LITERAL},      // no digits after prefix
		{"0o8", LITERAL},     // 8 is not an octal digit
		{"0xG", LITERAL},     // G is not a hex digit
		{"0b2", LITERAL},     // 2 is not a binary digit
		{"0o6_44", LITERAL},  // no digit separators
		{"0o644g", LITERAL},  // trailing literal char
		{"10o", LITERAL},     // prefix only valid right after a lone 0
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

func TestParseIntLiteral(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"0o644", 420},
		{"0x1a4", 420},
		{"0b110100100", 420},
		{"-0o10", -8},
		{"0xFF", 255},
		{"42", 42},
		{"-42", -42},
		{"0", 0},
	}
	for _, tc := range cases {
		got, err := parseIntLiteral(tc.input)
		if err != nil {
			t.Errorf("%q: unexpected error %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q: got %d, want %d", tc.input, got, tc.want)
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
