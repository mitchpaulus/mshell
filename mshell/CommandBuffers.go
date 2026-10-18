package main

import "unsafe"

// The command line is edited and painted on every key, so its text and its
// tokens live in buffers the editor keeps, rather than in new objects per
// key.

// lineArena hands out the strings of the command line from large chunks, so
// an edit allocates once per chunk rather than once per key. A chunk is only
// appended to, never reset or written again, so every string handed out is
// as immutable as any other Go string, and comparing two of them is
// reliable. A string kept for long keeps its whole chunk alive; whatever
// keeps the line past the next few edits (history, the evaluator, caches
// keyed by a word of it) takes a copy.
type lineArena struct {
	chunk []byte // The bytes handed out so far; its capacity is the chunk.
}

const lineArenaChunk = 64 << 10

// concat returns a + b + c, in the arena unless it is long.
func (a *lineArena) concat(x, y, z string) string {
	n := len(x) + len(y) + len(z)
	if n == 0 {
		return ""
	}
	if n > lineArenaChunk/8 {
		// A long line would use up chunks in a few edits.
		return x + y + z
	}
	if cap(a.chunk)-len(a.chunk) < n {
		a.chunk = make([]byte, 0, lineArenaChunk)
	}
	start := len(a.chunk)
	a.chunk = append(a.chunk, x...)
	a.chunk = append(a.chunk, y...)
	a.chunk = append(a.chunk, z...)
	return unsafe.String(&a.chunk[start], n)
}

// lineTokens are the tokens of the command line, lexed once per edit, as
// painting needs them: white space and comments are tokens, so the tokens
// tile the line, and a string may be unterminated. Painting fills it, and
// the abbreviation lookup on the next key reads the same tokens.
type lineTokens struct {
	l      *Lexer
	source SourceText // The line the tokens are of, when valid.
	valid  bool
	tokens []Token
	// errAt is the byte offset where the lexer failed, or -1. The tokens
	// before it are kept; there is no token for the error.
	errAt ByteOffset
	// brackets is findAbbrSite's scratch stack of open brackets.
	brackets []int32
}

// lineTokensKeep is how many tokens' room is kept between edits whatever
// the line's length.
const lineTokensKeep = 4096

// get returns the tokens of line, lexing it when it is not the line the
// tokens are of. The slice is valid until the next call.
func (c *lineTokens) get(line SourceText) []Token {
	if c.valid && c.source == line {
		return c.tokens
	}
	if c.l == nil {
		c.l = NewLexer("", &TokenFile{"REPL"})
		c.l.allowUnterminatedString = true
		c.l.emitWhitespace = true
		c.l.emitComments = true
		c.l.skipValues = true
	}
	// A line has at most one token per byte, and EOF. After a huge line,
	// do not keep its buffer for the short lines that follow.
	if cap(c.tokens) > max(lineTokensKeep, 2*(len(line)+1)) {
		c.tokens = nil
	}
	c.l.resetInput(string(line))
	tokens, err := c.l.TokenizeInto(c.tokens)
	c.errAt = -1
	if err != nil {
		// The last token is the error, and its lexeme is the message.
		tokens = tokens[:len(tokens)-1]
		var offset ByteOffset
		for _, t := range tokens {
			offset += ByteOffset(len(t.Lexeme))
		}
		c.errAt = offset
	}
	c.tokens, c.source, c.valid = tokens, line, true
	return tokens
}
