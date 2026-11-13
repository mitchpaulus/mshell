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

func TestParseAssertiveDestructuring(t *testing.T) {
	input := "[1 2] => [first second]"
	parser := NewMShellParser(NewLexer(input, nil))
	file, err := parser.ParseFile()
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(file.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(file.Items))
	}
	block, ok := file.Items[1].(*MShellParseMatchBlock)
	if !ok {
		t.Fatalf("second item is %T, want *MShellParseMatchBlock", file.Items[1])
	}
	if !block.Assertive || len(block.Arms) != 1 || !block.Arms[0].Consume || len(block.Arms[0].Body) != 0 {
		t.Fatalf("unexpected assertive match AST: %#v", block)
	}
	if _, ok := block.Arms[0].Pattern[0].(*MShellParseList); !ok {
		t.Fatalf("pattern is %T, want list pattern", block.Arms[0].Pattern[0])
	}
}

func TestParseAssertiveDestructuringForms(t *testing.T) {
	inputs := []string{
		"{'name': 'Ada'} => {'name': name}",
		"'value' just => just value",
		"[1 2 3] => [first ...middle last]",
	}
	for _, input := range inputs {
		parser := NewMShellParser(NewLexer(input, nil))
		if _, err := parser.ParseFile(); err != nil {
			t.Errorf("%q: unexpected parse error: %v", input, err)
		}
	}
}

func TestParseRejectsInvalidStructuralBindingPatterns(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"[1] => [_]", "must bind at least one variable"},
		{"[1] => ['a' 1 [_ ..._]]", "must bind at least one variable"},
		{"[1] => [first ...middle ...last]", "one named spread binding"},
		{"[[1]] => [[first ...middle ...last]]", "one named spread binding"},
		{"['-h'] match [-h] : end", "To match the string, quote it: '-h'"},
		{"['a'] match [$\"a\"] : end", "A format string cannot be a match pattern"},
		{"[(1)] match [(1)] : end", "cannot be part of a pattern"},
		{"{'a': 1} => {'a': first second}", "must be one item"},
		{"{'a': {'b': 1}} => {'a': {'b': ...rest}}", "Dictionary destructuring does not support spread patterns"},
		{"{'a': 1} => {'a': ..._}", "Dictionary destructuring does not support spread patterns"},
		{"{'a': 1} match {'a': ...rest} : end", "Dictionary destructuring does not support spread patterns"},
		{"none => just _", "must bind at least one variable"},
		{"42 => int", "Expected a list, dictionary, or 'just <name>' pattern"},
	}
	for _, tc := range cases {
		parser := NewMShellParser(NewLexer(tc.input, nil))
		_, err := parser.ParseFile()
		if err == nil {
			t.Errorf("%q: expected parse error", tc.input)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got error %q, want it to contain %q", tc.input, err, tc.want)
		}
	}
}

func TestParseAcceptsLiteralAndNestedPatterns(t *testing.T) {
	cases := []string{
		"['GET' '/'] match ['GET' path] : , ['POST' path ...rest] : , [...rest 'end'] : , _ : , end",
		"[1] match [1 2.5 true false `p` 'r' \"s\" none null -3] : , _ : , end",
		"[[1 2]] => [[a b] ...rest]",
		"{'to': [1 2]} => {'to': [x1 y1], 'kind': 'move'}",
		"[{'a': [1]}] => [{'a': [one]}]",
		// The parser cannot tell an enum member from a name, so the same
		// word twice is left to the type checker and the runtime.
		"[1 2] => [value value]",
	}
	for _, input := range cases {
		parser := NewMShellParser(NewLexer(input, nil))
		if _, err := parser.ParseFile(); err != nil {
			t.Errorf("%q: unexpected parse error: %v", input, err)
		}
	}
}

func TestStructuralBindingPatternItemLimit(t *testing.T) {
	atLimit := "[1] => [" + strings.Repeat("_ ", maxStructuralPatternItems-1) + "value]"
	parser := NewMShellParser(NewLexer(atLimit, nil))
	if _, err := parser.ParseFile(); err != nil {
		t.Fatalf("pattern at item limit should parse: %v", err)
	}

	overLimit := "[1] => [" + strings.Repeat("_ ", maxStructuralPatternItems) + "value]"
	parser = NewMShellParser(NewLexer(overLimit, nil))
	_, err := parser.ParseFile()
	if err == nil || !strings.Contains(err.Error(), "at most 256 positions") {
		t.Fatalf("pattern over item limit: got %v", err)
	}
}

func TestAssertiveMatchInvariantPanicsOnMalformedAST(t *testing.T) {
	block := &MShellParseMatchBlock{
		Assertive: true,
		Arms: []MShellParseMatchArm{{
			Consume: false,
			Pattern: []MShellParseItem{&Token{Type: LITERAL, Lexeme: "value"}},
		}},
	}
	defer func() {
		if recover() == nil {
			t.Fatal("malformed assertive match AST did not panic")
		}
	}()
	block.assertAssertiveInvariant()
}
