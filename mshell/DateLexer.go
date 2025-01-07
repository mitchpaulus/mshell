package main

import (
	"unicode"
	// "strings"
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
	start   int
	current int
	input   []rune
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

func (l *DateLexer) Length() int {
	return l.current - l.start
}

func (l *DateLexer) peekNext() rune {
	if l.current+1 >= len(l.input) {
		return 0
	}
	return l.input[l.current+1]
}

func (l *DateLexer) checkMonthDowType(start int, rest string, tokenType DateTokenType) DateTokenType {
	lengthMatch := l.current-l.start == start+len(rest)
	restMatch := string(l.input[l.start+start:l.current]) == rest
	if lengthMatch && restMatch {
		return tokenType
	}

	return DATESEP
}

func (l *DateLexer) MonthDowType() DateTokenType {
	c := l.input[l.start]
	length := l.Length()
	lexemeLowered := l.curLexeme()

	switch c {
	case 'j', 'J':
		peek := l.peek()
		switch peek {
		case 'u', 'U':
			if length == 3 && lexemeLowered == "jun" {
				return DATEMONTH
			} else if length == 4 && lexemeLowered == "june" {
				return DATEMONTH
			} else if length == 3 && lexemeLowered == "jul" {
				return DATEMONTH
			} else if length == 4 && lexemeLowered == "july" {
				return DATEMONTH
			} else {
				return DATESEP
			}
		case 'a', 'A':
			if length == 7 && lexemeLowered == "january" {
				return DATEMONTH
			} else if length == 3 && lexemeLowered == "jan" {
				return DATEMONTH
			} else {
				return DATESEP
			}
		default:
			return DATESEP
		}
	case 'f', 'F':
		if length == 8 && lexemeLowered == "february" {
			return DATEMONTH
		} else if length == 3 && lexemeLowered == "feb" {
			return DATEMONTH
		} else if length == 3 && lexemeLowered == "fri" {
			return DATEDOW
		} else if length == 6 && lexemeLowered == "friday" {
			return DATEDOW
		} else {
			return DATESEP
		}
	case 'm', 'M':
		peek := l.peek()
		switch peek {
		case 'a', 'A':
			if length == 3 && lexemeLowered == "mar" {
				return DATEMONTH
			} else if length == 5 && lexemeLowered == "march" {
				return DATEMONTH
			} else if length == 3 && lexemeLowered == "may" {
				return DATEMONTH
			} else {
				return DATESEP
			}
		case 'o', 'O':
			if length == 3 && lexemeLowered == "mon" {
				return DATEDOW
			} else if length == 6 && lexemeLowered == "monday" {
				return DATEDOW
			} else {
				return DATESEP
			}
		default:
			return DATESEP
		}
	case 'a', 'A':
		peek := l.peek()
		switch peek {
		case 'p', 'P':
			if length == 3 && lexemeLowered == "apr" {
				return DATEMONTH
			} else if length == 5 && lexemeLowered == "april" {
				return DATEMONTH
			} else {
				return DATESEP
			}
		case 'u', 'U':
			if length == 3 && lexemeLowered == "aug" {
				return DATEMONTH
			} else if length == 6 && lexemeLowered == "august" {
				return DATEMONTH
			} else {
				return DATESEP
			}
		}
	case 's', 'S':
		peek := l.peek()
		switch peek {
		case 'a', 'A':
			if length == 3 && lexemeLowered == "sat" {
				return DATEDOW
			} else if length == 8 && lexemeLowered == "saturday" {
				return DATEDOW
			} else {
				return DATESEP
			}
		case 'e', 'E':
			if length == 3 && lexemeLowered == "sep" {
				return DATEMONTH
			} else if length == 9 && lexemeLowered == "september" {
				return DATEMONTH
			} else {
				return DATESEP
			}
		case 'u', 'U':
			if length == 3 && lexemeLowered == "sun" {
				return DATEDOW
			} else if length == 6 && lexemeLowered == "sunday" {
				return DATEDOW
			} else {
				return DATESEP
			}
		}
	case 'o', 'O':
		if length == 3 && lexemeLowered == "oct" {
			return DATEMONTH
		} else if length == 7 && lexemeLowered == "october" {
			return DATEMONTH
		} else {
			return DATESEP
		}
	case 'n', 'N':
		if length == 3 && lexemeLowered == "nov" {
			return DATEMONTH
		} else if length == 8 && lexemeLowered == "november" {
			return DATEMONTH
		} else {
			return DATESEP
		}
	case 'd', 'D':
		if length == 3 && lexemeLowered == "dec" {
			return DATEMONTH
		} else if length == 8 && lexemeLowered == "december" {
			return DATEMONTH
		} else {
			return DATESEP
		}
	case 't', 'T':
		if length == 3 && lexemeLowered == "tue" {
			return DATEDOW
		} else if length == 7 && lexemeLowered == "tuesday" {
			return DATEDOW
		} else if length == 3 && lexemeLowered == "thu" {
			return DATEDOW
		} else if length == 8 && lexemeLowered == "thursday" {
			return DATEDOW
		} else {
			return DATESEP
		}
	case 'w', 'W':
		if length == 3 && lexemeLowered == "wed" {
			return DATEDOW
		} else if length == 9 && lexemeLowered == "wednesday" {
			return DATEDOW
		} else {
			return DATESEP
		}
	default:
		return DATESEP
	}

	return DATESEP
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
	} else if c == 'j' || c == 'J' || c == 'f' || c == 'F' || c == 'm' || c == 'M' || c == 'a' || c == 'A' || c == 's' || c == 'S' || c == 'o' || c == 'O' || c == 'n' || c == 'N' || c == 'd' || c == 'D' || c == 't' || c == 'T' || c == 'w' || c == 'W' {
		for unicode.IsLetter(l.peek()) {
			l.advance()
		}
		return l.makeToken(l.MonthDowType())
	} else {
		// TODO: Implement the rest of the lexer
		return l.makeToken(DATEEOF)
	}
}
