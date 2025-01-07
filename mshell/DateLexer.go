package main

import (
	"unicode"
)

type DateTokenType int

const (
	DATEINT = iota
	DATEMONTH
	DATEDOW
	DATESEP
	DATEEOF
)


type DateToken struct {
	Start  int
	Lexeme string
	Type   DateTokenType
}


type DateLexer struct {
	start int
	current int
	input  []rune
}

func NewDateLexer(input string) *DateLexer {
	return &DateLexer{0, 0, []rune(input)}
}


func (l *DateLexer) atEnd() bool {
	return l.current >= len(l.input)
}

func (l *DateLexer) curLen() int {
	return l.current - l.start
}

func (l *DateLexer) curLexeme() string {
	return string(l.input[l.start:l.current])
}

func (l *DateLexer) makeToken(tokenType DateTokenType) DateToken {
	lexeme := l.curLexeme()

	return DateToken{
		Start:  l.start,
		Lexeme: lexeme,
		Type:   tokenType,
	}
}

func (l *DateLexer) advance() rune {
	c := l.input[l.current]
	l.current++
	return c
}

func (l *DateLexer) peek() rune {
	if l.atEnd() {
		return 0
	}
	return l.input[l.current]
}

func (l *DateLexer) peekNext() rune {
	if l.current+1 >= len(l.input) {
		return 0
	}
	return l.input[l.current+1]
}

func (l *DateLexer) scanToken() DateToken {

	l.start = l.current

	if l.atEnd() {
		return l.makeToken(DATEEOF)
	}

	c := l.advance()

	if unicode.IsDigit(c) {
		for unicode.IsDigit(l.peek()) {
			l.advance()
		}
		return l.makeToken(DATEINT)
	} else {
		// TODO: Implement the rest of the lexer
		return l.makeToken(DATEEOF)
	}
}
