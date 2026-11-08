package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
)

// The reference the GUI frontend's Rust lexer is compared against
// (gui/msh-editor, ai/gui-terminal.typ "Keeping it in sync"). Hidden options
// of --lex:
//
//	--lex-editor  lex as the line editor does: unfinished strings are tokens,
//	              and whitespace and comments are emitted
//	--lex-cases   stdin is many inputs, each a u32 little-endian length and
//	              the bytes; each one's tokens follow a line "=== case N"

// lexDump prints the tokens of input as --lex does: one per line,
// line:column:TYPE lexeme, up to and including the first error.
func lexDump(w io.Writer, input string, editor bool) {
	l := NewLexer(input, &TokenFile{"lex"})
	if editor {
		l.allowUnterminatedString = true
		l.emitWhitespace = true
		l.emitComments = true
		l.skipValues = true
	}
	tokens, _ := l.Tokenize()
	for _, t := range tokens {
		fmt.Fprintf(w, "%d:%d:%s %s\n", t.Line, t.Column, t.Type, t.Lexeme)
	}
}

// lexDumpCases runs lexDump over each length-prefixed case in input.
func lexDumpCases(w io.Writer, input []byte, editor bool) error {
	out := bufio.NewWriterSize(w, 1<<20)
	for n := 0; len(input) > 0; n++ {
		if len(input) < 4 {
			return fmt.Errorf("case %d: truncated length", n)
		}
		size := binary.LittleEndian.Uint32(input)
		if uint64(len(input)-4) < uint64(size) {
			return fmt.Errorf("case %d: truncated input", n)
		}
		fmt.Fprintf(out, "=== case %d\n", n)
		lexDump(out, string(input[4:4+size]), editor)
		input = input[4+size:]
	}
	return out.Flush()
}
