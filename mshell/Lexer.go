package main

import (
	"fmt"
	"os"
	"unicode"
	"encoding/json"
)

type TokenType int

const (
	EOF TokenType = iota
	ERROR
	LEFT_SQUARE_BRACKET
	RIGHT_SQUARE_BRACKET
	LEFT_PAREN
	RIGHT_PAREN
	EXECUTE
	PIPE
	QUESTION
	POSITIONAL
	STRING
	SINGLEQUOTESTRING
	MINUS
	PLUS
	EQUALS
	INTERPRET
	IF
	LOOP
	READ
	STR
	BREAK
	NOT
	AND
	OR
	GREATERTHANOREQUAL
	LESSTHANOREQUAL
	LESSTHAN
	GREATERTHAN
	TRUE
	FALSE
	VARRETRIEVE
	VARSTORE
	INTEGER
	FLOAT
	LITERAL
	INDEXER
	ENDINDEXER
	STARTINDEXER
	SLICEINDEXER
	STDOUTLINES
	STDOUTSTRIPPED
	STDOUTCOMPLETE
	EXPORT
	TILDEEXPANSION
	STOP_ON_ERROR
	DEF
	END
	STDERRREDIRECT
	TYPEINT
	TYPEFLOAT
	// TYPESTRING, using str token instead
	TYPEBOOL
	DOUBLEDASH
	AMPERSAND
)

func (t TokenType) String() string {
	switch t {
	case EOF:
		return "EOF"
	case ERROR:
		return "ERROR"
	case LEFT_SQUARE_BRACKET:
		return "LEFT_SQUARE_BRACKET"
	case RIGHT_SQUARE_BRACKET:
		return "RIGHT_SQUARE_BRACKET"
	case LEFT_PAREN:
		return "LEFT_PAREN"
	case RIGHT_PAREN:
		return "RIGHT_PAREN"
	case EXECUTE:
		return "EXECUTE"
	case PIPE:
		return "PIPE"
	case QUESTION:
		return "QUESTION"
	case POSITIONAL:
		return "POSITIONAL"
	case STRING:
		return "STRING"
	case SINGLEQUOTESTRING:
		return "SINGLEQUOTESTRING"
	case MINUS:
		return "MINUS"
	case PLUS:
		return "PLUS"
	case EQUALS:
		return "EQUALS"
	case INTERPRET:
		return "INTERPRET"
	case IF:
		return "IF"
	case LOOP:
		return "LOOP"
	case READ:
		return "READ"
	case STR:
		return "STR"
	case BREAK:
		return "BREAK"
	case NOT:
		return "NOT"
	case AND:
		return "AND"
	case OR:
		return "OR"
	case GREATERTHANOREQUAL:
		return "GREATERTHANOREQUAL"
	case LESSTHANOREQUAL:
		return "LESSTHANOREQUAL"
	case LESSTHAN:
		return "LESSTHAN"
	case GREATERTHAN:
		return "GREATERTHAN"
	case TRUE:
		return "TRUE"
	case FALSE:
		return "FALSE"
	case VARRETRIEVE:
		return "VARRETRIEVE"
	case VARSTORE:
		return "VARSTORE"
	case INTEGER:
		return "INTEGER"
	case FLOAT:
		return "FLOAT"
	case LITERAL:
		return "LITERAL"
	case INDEXER:
		return "INDEXER"
	case ENDINDEXER:
		return "ENDINDEXER"
	case STARTINDEXER:
		return "STARTINDEXER"
	case SLICEINDEXER:
		return "SLICEINDEXER"
	case STDOUTLINES:
		return "STDOUTLINES"
	case STDOUTSTRIPPED:
		return "STDOUTSTRIPPED"
	case STDOUTCOMPLETE:
		return "STDOUTCOMPLETE"
	case EXPORT:
		return "EXPORT"
	case TILDEEXPANSION:
		return "TILDEEXPANSION"
	case STOP_ON_ERROR:
		return "STOP_ON_ERROR"
	case DEF:
		return "DEF"
	case END:
		return "END"
	case STDERRREDIRECT:
		return "STDERRREDIRECT"
	case TYPEINT:
		return "TYPEINT"
	case TYPEFLOAT:
		return "TYPEFLOAT"
	case TYPEBOOL:
		return "TYPEBOOL"
	case DOUBLEDASH:
		return "DOUBLEDASH"
	case AMPERSAND:
		return "AMPERSAND"
	default:
		return "UNKNOWN"
	}
}

type Token struct {
	// One-based line number.
	Line   int
	Column int
	Start  int
	Lexeme string
	Type   TokenType
}

func (t Token) ToJson() string {
	escaped, _ := json.Marshal(t.Lexeme)
	return fmt.Sprintf("{\"line\": %d, \"column\": %d, \"start\": %d, \"lexeme\": %s, \"type\": \"%s\"}", t.Line, t.Column, t.Start, string(escaped), t.Type)
}

func (t Token) DebugString() string {
	return fmt.Sprintf("'%s'", t.Lexeme)
}

type Lexer struct {
	start   int
	current int
	col     int
	line    int
	input   []rune
}

func (l *Lexer) DebugStr() {
	fmt.Fprintf(os.Stderr, "start: %d, current: %d, col: %d, line: %d, cur lexeme: %s\n", l.start, l.current, l.col, l.line, l.curLexeme())
}

func NewLexer(input string) *Lexer {
	return &Lexer{
		input: []rune(input),
		line:  1,
	}
}

func (l *Lexer) atEnd() bool {
	return l.current >= len(l.input)
}

func (l *Lexer) curLen() int {
	return l.current - l.start
}

func (l *Lexer) curLexeme() string {
	return string(l.input[l.start:l.current])
}

func (l *Lexer) makeToken(tokenType TokenType) Token {
	lexeme := l.curLexeme()

	return Token{
		Line:   l.line,
		Column: l.col,
		Start:  l.start,
		Lexeme: lexeme,
		Type:   tokenType,
	}
}

func (l *Lexer) advance() rune {
	c := l.input[l.current]
	l.current++
	l.col++
	return c
}

func (l *Lexer) peek() rune {
	if l.atEnd() {
		return 0
	}
	return l.input[l.current]
}

func (l *Lexer) peekNext() rune {
	if l.current+1 >= len(l.input) {
		return 0
	}
	return l.input[l.current+1]
}

var notAllowedLiteralChars = map[rune]bool{
	'[': true,
	']': true,
	'(': true,
	')': true,
	'<': true,
	'>': true,
	';': true,
	'?': true,
	'!': true,
	'@': true,
	'=': true,
	'&': true,
	'|': true,
}

func isAllowedLiteral(r rune) bool {
	if unicode.IsSpace(r) {
		return false
	}
	_, ok := notAllowedLiteralChars[r]
	return !ok
}

func (l *Lexer) parseLiteralOrKeyword() Token {
	for {
		if l.atEnd() {
			break
		}
		c := l.peek()
		if isAllowedLiteral(c) {
			l.advance()
		} else {
			break
		}
	}

	tokenType := l.literalOrKeywordType()
	return l.makeToken(tokenType)
}

func (l *Lexer) literalOrKeywordType() TokenType {
	switch l.input[l.start] {
	case '-':
		if l.curLen() > 1 {
			return l.checkKeyword(1, "-", DOUBLEDASH)
		}
	case '+':
		return l.checkKeyword(1, "", PLUS)
	case 'a':
		return l.checkKeyword(1, "nd", AND)
	case 'b':
		if l.curLen() > 1 {
			c := l.input[l.start+1]
			switch c {
			case 'r':
				return l.checkKeyword(2, "eak", BREAK)
			case 'o':
				return l.checkKeyword(2, "ol", TYPEBOOL)
			}
		}
	case 'd':
		return l.checkKeyword(1, "ef", DEF)
	case 'e':
		if l.curLen() > 1 {
			c := l.input[l.start+1]
			switch c {
			case 'n':
				return l.checkKeyword(2, "d", END)
			case 'x':
				return l.checkKeyword(2, "port", EXPORT)
			}
		}
	case 'f':
		if l.curLen() > 1 {
			c := l.input[l.start+1]
			switch c {
			case 'a':
				return l.checkKeyword(2, "lse", FALSE)
			case 'l':
				return l.checkKeyword(2, "oat", TYPEFLOAT)
			}
		}
	case 'i':
		if l.curLen() > 1 {
			c := l.input[l.start+1]
			if c == 'f' {
				return l.checkKeyword(2, "", IF)
			} else if c == 'n' {
				return l.checkKeyword(2, "t", TYPEINT)
			}
		}
	case 'l':
		return l.checkKeyword(1, "oop", LOOP)
	case 'n':
		return l.checkKeyword(1, "ot", NOT)
	case 'o':
		if l.curLen() == 1 {
			return STDOUTLINES
		}

		c := l.input[l.start+1]
		switch c {
		case 'c':
			return l.checkKeyword(2, "", STDOUTCOMPLETE)
		case 'r':
			return l.checkKeyword(2, "", OR)
		case 's':
			return l.checkKeyword(2, "", STDOUTSTRIPPED)
		}
	case 'r':
		return l.checkKeyword(1, "ead", READ)
	case 's':
		if l.curLen() > 1 {
			c := l.input[l.start+1]
			switch c {
			case 'o':
				return l.checkKeyword(2, "e", STOP_ON_ERROR)
			case 't':
				return l.checkKeyword(2, "r", STR)
			}
		}
	case 't':
		return l.checkKeyword(1, "rue", TRUE)
	case 'x':
		if l.curLen() == 1 {
			return INTERPRET
		}
	}

	if l.peek() == '!' {
		l.advance()
		return VARSTORE
	}

	return LITERAL
}

func (l *Lexer) checkKeyword(start int, rest string, tokenType TokenType) TokenType {
	lengthMatch := l.current-l.start == start+len(rest)
	restMatch := string(l.input[l.start+start:l.current]) == rest
	if lengthMatch && restMatch {
		return tokenType
	}

	if l.peek() == '!' {
		l.advance()
		return VARSTORE
	}
	return LITERAL
}

func (l *Lexer) scanToken() Token {
	l.eatWhitespace()
	l.start = l.current
	if l.atEnd() {
		return l.makeToken(EOF)
	}

	c := l.advance()

	if c == '"' {
		return l.parseString()
	}

	if unicode.IsDigit(c) {
		return l.parseNumberOrStartIndexer()
	}

	switch c {
	case '\'':
		return l.parseSingleQuoteString()
	case '[':
		return l.makeToken(LEFT_SQUARE_BRACKET)
	case ']':
		return l.makeToken(RIGHT_SQUARE_BRACKET)
	case '(':
		return l.makeToken(LEFT_PAREN)
	case ')':
		return l.makeToken(RIGHT_PAREN)
	case ';':
		return l.makeToken(EXECUTE)
	case '|':
		return l.makeToken(PIPE)
	case '?':
		return l.makeToken(QUESTION)
	case '$':
		if unicode.IsDigit(l.peek()) {
			return l.parsePositional()
		}
		return l.parseLiteralOrKeyword()
	case '=':
		return l.makeToken(EQUALS)
	case '&':
		return l.makeToken(AMPERSAND)
	case '<':
		if l.peek() == '=' {
			l.advance()
			return l.makeToken(LESSTHANOREQUAL)
		} else {
			return l.makeToken(LESSTHAN)
		}
	case '>':
		if l.peek() == '=' {
			l.advance()
			return l.makeToken(GREATERTHANOREQUAL)
		} else {
			return l.makeToken(GREATERTHAN)
		}
	case ':':
		return l.parseIndexerOrLiteral()
	case '-':
		if unicode.IsDigit(l.peek()) {
			// Consume the hyphen and parse the number
			l.advance()
			return l.parseNumberOrStartIndexer()
		} else if unicode.IsSpace(l.peek()) {
			return l.makeToken(MINUS)
		} else {
			return l.parseLiteralOrKeyword()
		}
	case '@':
		for {
			if l.atEnd() {
				break
			}
			c := l.peek()
			if isAllowedLiteral(c) {
				l.advance()
			} else {
				break
			}
		}
		// TODO: if empty at end, need better error.
		return l.makeToken(VARRETRIEVE)
	default:
		// return l.parseLiteralOrNumber()
		return l.parseLiteralOrKeyword()
	}
}

func (l *Lexer) parseSingleQuoteString() Token {
	// When this is called, we've already consumed a single quote.
	for {
		if l.atEnd() {
			fmt.Fprintf(os.Stderr, "%d:%d: Unterminated string.\n", l.line, l.col)
			return l.makeToken(ERROR)
		}

		c := l.advance()
		if c == '\'' {
			break
		}
	}

	return l.makeToken(SINGLEQUOTESTRING)
}

func (l *Lexer) consumeLiteral() Token {
	for {
		if l.atEnd() {
			break
		}
		c := l.peek()
		if isAllowedLiteral(c) {
			l.advance()
		} else {
			break
		}
	}

	if l.peek() == '!' {
		l.advance()
		return l.makeToken(VARSTORE)
	}

	return l.makeToken(LITERAL)
}

func (l *Lexer) parseNumberOrStartIndexer() Token {
	// Read all the digits
	for {
		if l.atEnd() {
			break
		}
		if !unicode.IsDigit(l.peek()) {
			break
		}
		l.advance()
	}

	peek := l.peek()
	if peek == ':' {
		l.advance()

		c := l.peek()
		if c == '-' {
			if unicode.IsDigit(l.peekNext()) {
				l.advance() // Consume the hyphen
				for {
					if !unicode.IsDigit(l.peek()) {
						break
					}
					l.advance()
				}
				return l.makeToken(SLICEINDEXER)
			} else {
				return l.makeToken(STARTINDEXER)
			}
		} else if unicode.IsDigit(c) {
			// Read all the digits
			for {
				if l.atEnd() {
					break
				}
				if !unicode.IsDigit(l.peek()) {
					break
				}
				l.advance()
			}
			return l.makeToken(SLICEINDEXER)
		} else {
			return l.makeToken(STARTINDEXER)
		}
	} else if peek == '>' {
		l.advance()
		return l.makeToken(STDERRREDIRECT)
	} else if peek == '.' {
		l.advance()

		for {
			if l.atEnd() {
				break
			}
			if !unicode.IsDigit(l.peek()) {
				break
			}
			l.advance()
		}

		return l.makeToken(FLOAT)
	}

	if !isAllowedLiteral(peek) {
		return l.makeToken(INTEGER)
	} else {
		return l.consumeLiteral()
	}
}

func (l *Lexer) parseIndexerOrLiteral() Token {
	c := l.advance()

	// Return literal if at end
	if l.atEnd() {
		return l.makeToken(LITERAL)
	}

	if unicode.IsDigit(c) || c == '-' {
		// Read all the digits
		for {
			if l.atEnd() {
				break
			}
			if !unicode.IsDigit(l.peek()) {
				break
			}
			c = l.advance()
		}
	} else {
		return l.consumeLiteral()
	}

	if l.peek() == ':' {
		l.advance()
		return l.makeToken(INDEXER)
	} else {
		return l.makeToken(ENDINDEXER)
	}
}

func (l *Lexer) parsePositional() Token {
	for {
		if l.atEnd() {
			break
		}
		if !unicode.IsDigit(l.peek()) {
			break
		}
		l.advance()
	}
	return l.makeToken(POSITIONAL)
}

func (l *Lexer) parseString() Token {
	// When this is called, we've already consumed a single double quote.
	inEscape := false
	for {
		if l.atEnd() {
			fmt.Fprintf(os.Stderr, "%d:%d: Unterminated string.\n", l.line, l.col)
			return l.makeToken(ERROR)
		}
		c := l.advance()
		if inEscape {
			if c != 'n' && c != 't' && c != 'r' && c != '\\' && c != '"' {
				fmt.Fprintf(os.Stderr, "%d:%d: Invalid escape character '%c'.\n", l.line, l.col, c)
				return l.makeToken(ERROR)
			}
			inEscape = false
		} else {
			if c == '"' {
				break
			}
			if c == '\\' {
				inEscape = true
			}
		}
	}
	return l.makeToken(STRING)
}

func (l *Lexer) Tokenize() []Token {
	var tokens []Token
	for {
		t := l.scanToken()
		tokens = append(tokens, t)
		if t.Type == ERROR || t.Type == EOF {
			break
		}
	}
	return tokens
}

func (l *Lexer) eatWhitespace() {
	for {
		if l.atEnd() {
			return
		}
		c := l.peek()
		switch c {
		case ' ', '\t', '\r', '\v', '\f':
			l.advance()
		case '#':
			for !l.atEnd() && l.peek() != '\n' {
				l.advance()
			}
		case '\n':
			l.line++
			l.col = 0
			l.advance()
		default:
			return
		}
	}
}
