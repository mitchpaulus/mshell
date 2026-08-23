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

func TestParseAssertiveDestructuringAliases(t *testing.T) {
	for _, operator := range []string{"=>", "unpack"} {
		input := "[1 2] " + operator + " [first second]"
		parser := NewMShellParser(NewLexer(input, nil))
		file, err := parser.ParseFile()
		if err != nil {
			t.Fatalf("%s: unexpected parse error: %v", operator, err)
		}
		if len(file.Items) != 2 {
			t.Fatalf("%s: got %d items, want 2", operator, len(file.Items))
		}
		block, ok := file.Items[1].(*MShellParseMatchBlock)
		if !ok {
			t.Fatalf("%s: second item is %T, want *MShellParseMatchBlock", operator, file.Items[1])
		}
		if !block.Assertive || len(block.Arms) != 1 || !block.Arms[0].Consume || len(block.Arms[0].Body) != 0 {
			t.Fatalf("%s: unexpected assertive match AST: %#v", operator, block)
		}
		if _, ok := block.Arms[0].Pattern[0].(*MShellParseList); !ok {
			t.Fatalf("%s: pattern is %T, want list pattern", operator, block.Arms[0].Pattern[0])
		}
	}
}

func TestParseAssertiveDestructuringForms(t *testing.T) {
	inputs := []string{
		"{'name': 'Ada'} => {'name': name}",
		"'value' just unpack just value",
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
		{"['copy'] match ['copy'] : end", "List destructuring patterns may contain only binding names"},
		{"[1] => [_]", "must bind at least one variable"},
		{"[1] => [first ...middle ...last]", "one named spread binding"},
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
