package main

import (
	"slices"
	"strings"
	"testing"
	"unicode"
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

// TestElseStar checks that only "else" followed by '*' is the else* keyword,
// not every four-letter word starting with "el".
func TestElseStar(t *testing.T) {
	cases := []struct {
		input string
		want  []TokenType
	}{
		{"else*", []TokenType{ELSESTAR, EOF}},
		{"else", []TokenType{ELSE, EOF}},
		{"elxx*", []TokenType{LITERAL, ASTERISK, EOF}},
		{"elsé*", []TokenType{LITERAL, ASTERISK, EOF}},
		{"elsex*", []TokenType{LITERAL, ASTERISK, EOF}},
	}
	for _, tc := range cases {
		toks, err := NewLexer(tc.input, nil).Tokenize()
		if err != nil {
			t.Errorf("%q: %v", tc.input, err)
			continue
		}
		var got []TokenType
		for _, tok := range toks {
			got = append(got, tok.Type)
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%q: got %v, want %v", tc.input, got, tc.want)
		}
	}
}

// TestInvalidUTF8 checks that a byte that is not part of a valid UTF-8
// character is an error where it is, wherever it is.
func TestInvalidUTF8(t *testing.T) {
	cases := []struct {
		input string
		want  string // the error, or "" for none
	}{
		{"1 2 +", ""},
		{"'é' \uFFFD", ""},
		{"ab\xffcd", "1:3: Invalid UTF-8 encoding."},
		{"1\n  \"x\xe2\x82\"", "2:5: Invalid UTF-8 encoding."},
		{"1 # é\xc3", "1:6: Invalid UTF-8 encoding."},
		{"\xff", "1:1: Invalid UTF-8 encoding."},
		{"'a\nb\xf0\x9f\x98'", "2:2: Invalid UTF-8 encoding."},
		{"`p\xed\xa0\x80`", "1:3: Invalid UTF-8 encoding."},
	}
	for _, tc := range cases {
		_, err := NewLexer(tc.input, nil).Tokenize()
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%q: unexpected error %v", tc.input, err)
		case tc.want != "" && (err == nil || !strings.HasPrefix(err.Error(), tc.want)):
			t.Errorf("%q: got error %v, want %q", tc.input, err, tc.want)
		}
	}
}

func TestFatArrowToken(t *testing.T) {
	l := NewLexer("=> = >=", nil)
	toks, _ := l.Tokenize()
	want := []TokenType{FATARROW, EQUALS, GREATERTHANOREQUAL, EOF}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d", len(toks), len(want))
	}
	for i, tokenType := range want {
		if toks[i].Type != tokenType {
			t.Errorf("token %d: got %s, want %s", i, toks[i].Type, tokenType)
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

// A float ends at any token-ending character, not just whitespace, while a
// trailing literal char (e.g. `1.pdf`) makes the whole token a literal.
func TestFloatVsLiteral(t *testing.T) {
	cases := []struct {
		input      string
		want       TokenType
		wantLexeme string
	}{
		{"1.5", FLOAT, "1.5"},
		{"7.", FLOAT, "7."},
		{"1.5 x", FLOAT, "1.5"},
		{"1.5)", FLOAT, "1.5"},
		{"2.5]", FLOAT, "2.5"},
		{"3.5;", FLOAT, "3.5"},
		{"4.5,", FLOAT, "4.5"},
		{"5.5}", FLOAT, "5.5"},
		{"6.5!", FLOAT, "6.5"},
		{"1.pdf", LITERAL, "1.pdf"},
		{"1.5x", LITERAL, "1.5x"},
		{"1.5.6", LITERAL, "1.5.6"},
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
		if toks[0].Lexeme != tc.wantLexeme {
			t.Errorf("%q: got lexeme %q, want %q", tc.input, toks[0].Lexeme, tc.wantLexeme)
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

func TestFormatStringTokens(t *testing.T) {
	input := `$"a {1 {"k": 2} :k? } b {$"c {@x}"} d \{\}" $"plain"`
	tokens, err := NewLexer(input, nil).Tokenize()
	if err != nil {
		t.Fatalf("lex error: %v", err)
	}
	want := []struct {
		typ   TokenType
		value string
	}{
		{FORMATSTRINGSTART, "a "},
		{INTEGER, ""},
		{LEFT_CURLY, ""},
		{STRING, ""},
		{COLON, ""},
		{INTEGER, ""},
		{RIGHT_CURLY, ""},
		{COLON, ""},
		{LITERAL, ""},
		{QUESTION, ""},
		{FORMATSTRINGMID, " b "},
		{FORMATSTRINGSTART, "c "},
		{VARRETRIEVE, ""},
		{FORMATSTRINGEND, ""},
		{FORMATSTRINGEND, " d {}"},
		{FORMATSTRING, "plain"},
		{EOF, ""},
	}
	if len(tokens) != len(want) {
		for i, tok := range tokens {
			t.Logf("Token %d: %s", i, tok)
		}
		t.Fatalf("got %d tokens, want %d", len(tokens), len(want))
	}
	for i, w := range want {
		if tokens[i].Type != w.typ {
			t.Errorf("token %d: got %s, want %s", i, tokens[i].Type, w.typ)
			continue
		}
		switch w.typ {
		case FORMATSTRING, FORMATSTRINGSTART, FORMATSTRINGMID, FORMATSTRINGEND:
			if got := tokens[i].Value.(MShellString).Content; got != w.value {
				t.Errorf("token %d: got text %q, want %q", i, got, w.value)
			}
		}
	}
}

// peekToken must leave the format string state as it found it, including
// when the peeked token opens or closes an interpolation.
func TestFormatStringPeekRestoresState(t *testing.T) {
	input := `$"a {x} b {{"k": 1}} c"`
	plain, err := NewLexer(input, nil).Tokenize()
	if err != nil {
		t.Fatalf("lex error: %v", err)
	}

	l := NewLexer(input, nil)
	for i := range plain {
		peeked := l.peekToken()
		got := l.scanToken()
		if peeked.Type != got.Type || got.Type != plain[i].Type {
			t.Fatalf("token %d: peeked %s, scanned %s, want %s", i, peeked.Type, got.Type, plain[i].Type)
		}
	}
}

func TestUnterminatedFormatString(t *testing.T) {
	for _, input := range []string{`$"abc`, `$"a {@x} bc`} {
		l := NewLexer(input, nil)
		l.allowUnterminatedString = true
		tokens, err := l.Tokenize()
		if err != nil {
			t.Fatalf("%q: lex error: %v", input, err)
		}
		if last := tokens[len(tokens)-2]; last.Type != UNFINISHEDFORMATSTRING {
			t.Errorf("%q: last token is %s, want UNFINISHEDFORMATSTRING", input, last.Type)
		}

		if _, err := NewLexer(input, nil).Tokenize(); err == nil {
			t.Errorf("%q: expected an error without allowUnterminatedString", input)
		}
	}
}

func TestRuneLiterals(t *testing.T) {
	cases := []struct {
		input string
		want  rune   // the value, for a RUNE token
		err   string // a part of the message, for an ERROR token
	}{
		{"r'a'", 'a', ""},
		{"r' '", ' ', ""},
		{`r'\'`, '\\', ""},
		{"r'😀'", 0x1F600, ""},
		{"r'é'", 0xE9, ""},
		{"U+001B", 0x1B, ""},
		{"U+1f600", 0x1F600, ""},
		{"U+10FFFF", 0x10FFFF, ""},
		{"r'asdf'", 0, "r is not a raw-string prefix"},
		{"r'é'", 0, "Use U+00E9, or write the runes separately: r'e' U+0301."},
		{"r'👨‍👩‍👧'", 0, "holds 5 code points"},
		{"r''", 0, "Empty rune literal"},
		{"r'''", 0, "write the apostrophe as U+0027"},
		{"r'a", 0, "Unterminated rune literal"},
		{"r'a\n'", 0, "closing quote on the same line"},
		{"r'\t'", 0, "write it as U+0009"},
		{"U+41", 0, "4 to 6 hex digits"},
		{"U+0000041", 0, "4 to 6 hex digits"},
		{"U+00GG", 0, "expected 4 to 6 hex digits"},
		{"U+0041x", 0, "expected 4 to 6 hex digits"},
		{"U+110000", 0, "above U+10FFFF"},
		{"U+D83D", 0, "is a surrogate"},
	}
	for _, tc := range cases {
		toks, _ := NewLexer(tc.input, nil).Tokenize()
		tok := toks[0]
		if tc.err != "" {
			if tok.Type != ERROR || !strings.Contains(tok.Lexeme, tc.err) {
				t.Errorf("%q: got %s %q, want an error containing %q", tc.input, tok.Type, tok.Lexeme, tc.err)
			}
			continue
		}
		v, ok := tok.Value.(MShellRune)
		if tok.Type != RUNE || !ok || v.Value != tc.want {
			t.Errorf("%q: got %s %q (%v), want rune %U", tc.input, tok.Type, tok.Lexeme, tok.Value, tc.want)
		}
	}

	// Words that only look like the start of a rune stay what they were.
	for _, input := range []string{"xr'a'", "u+0041", "U+", "Ux", "r"} {
		toks, _ := NewLexer(input, nil).Tokenize()
		if toks[0].Type != LITERAL {
			t.Errorf("%q: got %s, want LITERAL", input, toks[0].Type)
		}
	}
}

func TestUnterminatedRune(t *testing.T) {
	l := NewLexer("r'a", nil)
	l.allowUnterminatedString = true
	toks, _ := l.Tokenize()
	if toks[0].Type != UNFINISHEDRUNE {
		t.Errorf("got %s, want UNFINISHEDRUNE", toks[0].Type)
	}
}

// Every rune prints in a form that lexes back to the same rune.
func TestRuneSourceRoundTrip(t *testing.T) {
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue
		}
		src := runeSource(r)
		toks, _ := NewLexer(src, nil).Tokenize()
		v, ok := toks[0].Value.(MShellRune)
		if len(toks) != 2 || toks[0].Type != RUNE || !ok || v.Value != r {
			t.Fatalf("%U prints as %q, which lexes to %s %q", r, src, toks[0].Type, toks[0].Lexeme)
		}
	}
}
