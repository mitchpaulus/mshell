package main

import (
	"encoding/json"
	"fmt"
	"os"
	"unicode"
)


// See scanToken for main token scanning entry point.

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
	STRING // Normal string like "hello world"
	UNFINISHEDSTRING
	SINGLEQUOTESTRING // Single quoted string like 'hello world'
	UNFINISHEDSINGLEQUOTESTRING
	MINUS
	PLUS
	EQUALS
	INTERPRET
	IF
	IFF // This is just temporary as I work to remove the old if.
	LOOP
	READ
	STR // This is like the command str that convert to string.
	BREAK
	CONTINUE
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
	ENVRETREIVE
	ENVSTORE
	ENVCHECK
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
	PATH
	COMMA
	DATETIME
	FORMATSTRING
	LEFT_CURLY
	RIGHT_CURLY
	COLON
	NOTEQUAL // !=
	BANG // !
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
	case UNFINISHEDSTRING:
		return "UNFINISHEDSTRING"
	case SINGLEQUOTESTRING:
		return "SINGLEQUOTESTRING"
	case UNFINISHEDSINGLEQUOTESTRING:
		return "UNFINISHEDSINGLEQUOTESTRING"
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
	case IFF:
		return "IFF"
	case LOOP:
		return "LOOP"
	case READ:
		return "READ"
	case STR:
		return "STR"
	case BREAK:
		return "BREAK"
	case CONTINUE:
		return "CONTINUE"
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
	case ENVRETREIVE:
		return "ENVRETREIVE"
	case ENVSTORE:
		return "ENVSTORE"
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
	case PATH:
		return "PATH"
	case COMMA:
		return "COMMA"
	case DATETIME:
		return "DATETIME"
	case FORMATSTRING:
		return "FORMATSTRING"
	case LEFT_CURLY:
		return "LEFT_CURLY"
	case RIGHT_CURLY:
		return "RIGHT_CURLY"
	case COLON:
		return "COLON"
	case NOTEQUAL:
		return "NOTEQUAL"
	case BANG:
		return "BANG"
	default:
		return "UNKNOWN"
	}
}

type Token struct {
	Line   int // One-based line number.
	Column int // One-based column number.
	Start  int // Zero-based index into the entire input string
	Lexeme string
	Type   TokenType
}

func (t Token) String() string {
	return fmt.Sprintf("Token{line: %d, column: %d, start: %d, lexeme: '%s', type: %s}", t.Line, t.Column, t.Start, t.Lexeme, t.Type)
}

func (t Token) ToJson() string {
	escaped, _ := json.Marshal(t.Lexeme)
	return fmt.Sprintf("{\"line\": %d, \"column\": %d, \"start\": %d, \"lexeme\": %s, \"type\": \"%s\"}", t.Line, t.Column, t.Start, string(escaped), t.Type)
}

func (t Token) DebugString() string {
	return fmt.Sprintf("'%s'", t.Lexeme)
}

func (t Token) GetStartToken() Token {
	return t
}

func (t Token) GetEndToken() Token {
	return t
}

type Lexer struct {
	start   int
	current int
	col     int // Zero-based column number.
	line    int // One-based line number.
	input   []rune
	allowUnterminatedString bool
}

func (l *Lexer) DebugStr() {
	fmt.Fprintf(os.Stderr, "start: %d, current: %d, col: %d, line: %d, cur lexeme: %s\n", l.start, l.current, l.col, l.line, l.curLexeme())
}

func NewLexer(input string) *Lexer {
	return &Lexer{
		input: []rune(input),
		line:  1,
		start: 0,
		current: 0,
		col: 0,
		allowUnterminatedString: false,
	}
}

func (l *Lexer) resetInput(input string) {
	l.input = []rune(input)
	l.line = 1
	l.col = 0
	l.start = 0
	l.current = 0
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
		Column: l.col - l.curLen() + 1,
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
	'{': true,
	'}': true,
	'<': true,
	'>': true,
	':': true,
	';': true,
	'?': true,
	'!': true,
	'@': true,
	',': true,
	// '=': true, Removed because it's often used in CLI options like --option=value
	'&': true,
	'|': true,
	'"': true, // Double quote, used for strings.
	'\'': true, // Single quote, used for single quoted strings.
	0:  true, // Null, used for 'peek' at end of file.
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
	case 'c':
		return l.checkKeyword(1, "ontinue", CONTINUE)
	case 'd':
		return l.checkKeyword(1, "ef", DEF)
	case 'e':
		return l.checkKeyword(1, "nd", END)
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
				if l.curLen() > 2 {
					c = l.input[l.start+2]
					if c == 'f' {
						return l.checkKeyword(3, "", IFF)
					}
				}
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

	if c == '`' {
		return l.parsePath()
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
	case '{':
		return l.makeToken(LEFT_CURLY)
	case '}':
		return l.makeToken(RIGHT_CURLY)
	case ';':
		return l.makeToken(EXECUTE)
	case '|':
		return l.makeToken(PIPE)
	case '?':
		return l.makeToken(QUESTION)
	case '$':
		if unicode.IsDigit(l.peek()) {
			return l.parsePositional()
		} else if l.peek() == '"' { // This must be before the 'isAllowedLiteral' check.
			l.advance()
			err := l.consumeString()
			if err != nil {
				if l.allowUnterminatedString {
					_, ok := err.(ConsumeStringErrorUnterminated)
					if ok {
						return l.makeToken(UNFINISHEDSTRING)
					} else {
						fmt.Fprintf(os.Stderr, "%s", err)
						return l.makeToken(ERROR)
					}
				} else {
					fmt.Fprintf(os.Stderr, "%s", err)
					return l.makeToken(ERROR)
				}
			} else {
				return l.makeToken(FORMATSTRING)
			}
		} else if isAllowedLiteral(l.peek()) {
			return l.parseEnvVar()
		} else {
			return l.parseLiteralOrKeyword()
		}
	case '=':
		return l.makeToken(EQUALS)
	case ',':
		return l.makeToken(COMMA)
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
		return l.parseIndexerOrColon()
	case '-':
		if unicode.IsDigit(l.peek()) {
			// Consume the hyphen and parse the number
			l.advance()
			return l.parseNumberOrStartIndexer()
		} else if unicode.IsSpace(l.peek()) || !isAllowedLiteral(l.peek()) {
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
	case '!':
		if l.peek() == '=' {
			l.advance()
			return l.makeToken(NOTEQUAL)
		} else {
			return l.makeToken(BANG)
		}
	default:
		// return l.parseLiteralOrNumber()
		return l.parseLiteralOrKeyword()
	}
}

func (l *Lexer) parseSingleQuoteString() Token {
	// When this is called, we've already consumed a single quote.
	for {
		if l.atEnd() {
			if l.allowUnterminatedString {
				return l.makeToken(UNFINISHEDSINGLEQUOTESTRING)
			} else {
				fmt.Fprintf(os.Stderr, "%d:%d: Unterminated string.\n", l.line, l.col)
				return l.makeToken(ERROR)
			}
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

func (l *Lexer) parseEnvVar() Token {
	for {
		if isAllowedLiteral(l.peek()) {
			l.advance()
		} else {
			break
		}
	}

	c := l.peek()

	if c == '!' {
		l.advance()
		return l.makeToken(ENVSTORE)
	} else if c == '?' {
		l.advance()
		return l.makeToken(ENVCHECK)
	} else {
		return l.makeToken(ENVRETREIVE)
	}
}

func (l *Lexer) parseNumberOrStartIndexer() Token {
	// Read all the digits
	for unicode.IsDigit(l.peek()) {
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
		for unicode.IsDigit(l.peek()) {
			l.advance()
		}
		return l.makeToken(FLOAT)
	} else if l.curLen() == 4 && peek == '-' {
		l.advance()
		// Month
		for i := 0; i < 2; i++ {
			if !unicode.IsDigit(l.peek()) {
				return l.consumeLiteral()
			}
			l.advance()
		}
		if l.peek() != '-' {
			return l.consumeLiteral()
		}
		l.advance()

		// Day
		for i := 0; i < 2; i++ {
			if !unicode.IsDigit(l.peek()) {
				return l.consumeLiteral()
			}
			l.advance()
		}

		if l.peek() != 'T' {
			return l.makeToken(DATETIME)
		} else {
			l.advance()
		}

		// Hour
		for i := 0; i < 2; i++ {
			if !unicode.IsDigit(l.peek()) {
				return l.consumeLiteral()
			}
			l.advance()
		}

		if l.peek() != ':' {
			return l.makeToken(DATETIME)
		}
		l.advance()

		// Minute
		for i := 0; i < 2; i++ {
			if !unicode.IsDigit(l.peek()) {
				return l.consumeLiteral()
			}
			l.advance()
		}

		// Second
		if l.peek() != ':' {
			return l.makeToken(DATETIME)
		}
		l.advance()

		for i := 0; i < 2; i++ {
			if !unicode.IsDigit(l.peek()) {
				return l.consumeLiteral()
			}
			l.advance()
		}

		return l.makeToken(DATETIME)
	}

	if !isAllowedLiteral(peek) {
		return l.makeToken(INTEGER)
	} else {
		return l.consumeLiteral()
	}
}

func (l *Lexer) parseIndexerOrColon() Token {
	c := l.peek()

	// Return literal if at end
	if c == 0 {
		return l.makeToken(COLON)
	}

	if unicode.IsDigit(c) || c == '-' {
		// Read all the digits
		l.advance()
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
		return l.makeToken(COLON)
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

type ConsumeStringError interface {
	Error() string
}

type ConsumeStringErrorUnterminated struct {
	ErrorString string
}

func (e ConsumeStringErrorUnterminated) Error() string {
	return e.ErrorString
}

type ConsumeStringErrorInvalidEscape struct {
	ErrorString string
}

func (e ConsumeStringErrorInvalidEscape) Error() string {
	return e.ErrorString
}

func (l *Lexer) consumeString() ConsumeStringError {
	// When this is called, we've already consumed a single double quote.
	inEscape := false
	for {
		if l.atEnd() {
			return ConsumeStringErrorUnterminated{ErrorString: fmt.Sprintf("%d:%d: Unterminated string.\n", l.line, l.col)}
		}
		c := l.advance()
		if inEscape {
			if c != 'e' && c != 'n' && c != 't' && c != 'r' && c != '\\' && c != '"' {
				return fmt.Errorf("%d:%d: Invalid escape character within string, '%c'. Expected 'e', 'n', 't', 'r', '\\', or '\"'.\n", l.line, l.col, c)
				// return l.makeToken(ERROR)
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

	return nil
}

func (l *Lexer) parseString() Token {
	err := l.consumeString()
	if err != nil {

		if l.allowUnterminatedString {
			_, ok := err.(ConsumeStringErrorUnterminated)
			if ok {
				return l.makeToken(UNFINISHEDSTRING)
			} else {
				fmt.Fprintf(os.Stderr, "%s", err)
				return l.makeToken(ERROR)
			}
		} else {
			fmt.Fprintf(os.Stderr, "%s", err)
			return l.makeToken(ERROR)
		}
	}

	return l.makeToken(STRING)
}

func (l *Lexer) parsePath() Token {
	inEscape := false
	for {
		if l.atEnd() {
			fmt.Fprintf(os.Stderr, "%d:%d: Unterminated path.\n", l.line, l.col)
			return l.makeToken(ERROR)
		}
		c := l.advance()
		if inEscape {
			if c != 'e' && c != 'n' && c != 't' && c != 'r' && c != '\\' && c != '`' {
				fmt.Fprintf(os.Stderr, "%d:%d: Invalid escape character within path, '%c'. Expected 'n', 't', 'r', '\\', or '`'.\n", l.line, l.col, c)
				return l.makeToken(ERROR)
			}
			inEscape = false
		} else {
			if c == '`' {
				break
			}
			if c == '\\' {
				inEscape = true
			}
		}
	}
	return l.makeToken(PATH)
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
