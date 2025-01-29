package main

import (
	"unicode"
	"fmt"
	"time"
	"strconv"
	"strings"
)

type DateTokenType int

const (
	DATEJAN = iota + 1 // 1, rest of months aligned, so don't change that.
	DATEFEB
	DATEMAR
	DATEAPR
	DATEMAY
	DATEJUN
	DATEJUL
	DATEAUG
	DATESEPT
	DATEOCT
	DATENOV
	DATEDEC
	DATEDOW
	DATESEP
	DATEAM
	DATEPM
	DATEINT4
	DATEINT2
	DATEINT1
	DATEEOF
)

type DateToken struct {
	Start  int
	Lexeme string
	Type   DateTokenType
}

func (token DateToken) IsMonth() bool {
	return token.Type >= 1 && token.Type <= 12
}

func (token DateToken) Month() time.Month {
	return time.Month(token.Type)
}

func (token DateToken) ParseDay() (int, error) {
	dayInt, err := strconv.Atoi(token.Lexeme)
	if err != nil {
		return -1, err
	}

	if dayInt < 0 || dayInt > 31 {
		return -1, fmt.Errorf("Day integer found to be '%d'", dayInt)
	}
	return dayInt, nil
}

func (token DateToken) ParseYear() (int) {
	yearInt, _ := strconv.Atoi(token.Lexeme)
	if len(token.Lexeme) == 2 {
		yearInt += 2000
	}
	return yearInt
}

func (token DateToken) String() string {
	return fmt.Sprintf("'%s' %v", token.Lexeme, token.Type)
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

func (l *DateLexer) charFromStart(index int) rune {
	if l.start + index < len(l.input) {
		return l.input[l.start + index]
	} else {
		return 0
	}
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
	lexemeLowered := strings.ToLower(l.curLexeme())

	switch c {
	case 'j', 'J':
		peek := l.charFromStart(1)
		switch peek {
		case 'u', 'U':
			if length == 3 && lexemeLowered == "jun" {
				return DATEJUN
			} else if length == 4 && lexemeLowered == "june" {
				return DATEJUN
			} else if length == 3 && lexemeLowered == "jul" {
				return DATEJUL
			} else if length == 4 && lexemeLowered == "july" {
				return DATEJUL
			} else {
				return DATESEP
			}
		case 'a', 'A':
			if length == 7 && lexemeLowered == "january" {
				return DATEJAN
			} else if length == 3 && lexemeLowered == "jan" {
				return DATEJAN
			} else {
				return DATESEP
			}
		default:
			return DATESEP
		}

	case 'f', 'F':
		if length == 8 && lexemeLowered == "february" {
			return DATEFEB
		} else if length == 3 && lexemeLowered == "feb" {
			return DATEFEB
		} else if length == 3 && lexemeLowered == "fri" {
			return DATEDOW
		} else if length == 6 && lexemeLowered == "friday" {
			return DATEDOW
		} else {
			return DATESEP
		}
	case 'm', 'M':
		peek := l.charFromStart(1)
		switch peek {
		case 'a', 'A':
			if length == 3 && lexemeLowered == "mar" {
				return DATEMAR
			} else if length == 5 && lexemeLowered == "march" {
				return DATEMAR
			} else if length == 3 && lexemeLowered == "may" {
				return DATEMAY
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
		peek := l.charFromStart(1)
		switch peek {
		case 'p', 'P':
			if length == 3 && lexemeLowered == "apr" {
				return DATEAPR
			} else if length == 5 && lexemeLowered == "april" {
				return DATEAPR
			} else {
				return DATESEP
			}
		case 'u', 'U':
			if length == 3 && lexemeLowered == "aug" {
				return DATEAUG
			} else if length == 6 && lexemeLowered == "august" {
				return DATEAUG
			} else {
				return DATESEP
			}
		}
	case 's', 'S':
		peek := l.charFromStart(1)
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
				return DATESEPT
			} else if length == 9 && lexemeLowered == "september" {
				return DATESEPT
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
			return DATEOCT
		} else if length == 7 && lexemeLowered == "october" {
			return DATEOCT
		} else {
			return DATESEP
		}
	case 'n', 'N':
		if length == 3 && lexemeLowered == "nov" {
			return DATENOV
		} else if length == 8 && lexemeLowered == "november" {
			return DATENOV
		} else {
			return DATESEP
		}
	case 'd', 'D':
		if length == 3 && lexemeLowered == "dec" {
			return DATEDEC
		} else if length == 8 && lexemeLowered == "december" {
			return DATEDEC
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
	// https://en.wikipedia.org/wiki/List_of_time_zone_abbreviations
	l.start = l.current

	if l.atEnd() {
		return l.makeToken(DATEEOF)
	}

	c := l.advance()

	if unicode.IsDigit(c) {
		for unicode.IsDigit(l.peek()) {
			l.advance()
		}
		if l.Length() == 4 {
			return l.makeToken(DATEINT4)
		} else if l.Length() == 2 {
			return l.makeToken(DATEINT2)
		} else if l.Length() == 1 {
			return l.makeToken(DATEINT1)
		} else { // TODO: Handle 8 and 6 digit numbers
			return l.makeToken(DATESEP)
		}
	} else if c == 'a' || c == 'A' {
		l.advance()
		if l.peek() == 'm' || l.peek() == 'M' {
			l.advance()

			// Check for AMST or AMT for Amazon summer time or Armenia time
			if l.peek() == 's' || l.peek() == 'S' || l.peek() == 'T' {
				return l.consumeAlpha()
			}

			if l.peek() == '.' {
				l.advance()
			}
			return l.makeToken(DATEAM)
		} else if l.peek() == '.' {
			l.advance()
			if l.peek() == 'm' || l.peek() == 'M' {
				l.advance()
				if l.peek() == '.' {
					l.advance()
				}
				return l.makeToken(DATEAM)
			}
		}

		for unicode.IsLetter(l.peek()) {
			l.advance()
		}
		return l.makeToken(l.MonthDowType())
	} else if c == 'j' || c == 'J' || c == 'f' || c == 'F' || c == 'm' || c == 'M' || c == 's' || c == 'S' || c == 'o' || c == 'O' || c == 'n' || c == 'N' || c == 'd' || c == 'D' || c == 't' || c == 'T' || c == 'w' || c == 'W' {
		for unicode.IsLetter(l.peek()) {
			l.advance()
		}
		return l.makeToken(l.MonthDowType())
	} else if c == 'p' || c == 'P' {
		if l.peek() == 'm' || l.peek() == 'M' {
			l.advance()
			// Checking for PMDT or PMST or Saint Pierre and Miquelon Daylight/Standard Time
			if l.peek() == 's' || l.peek() == 'S' || l.peek() == 'D' || l.peek() == 'd' {
				return l.consumeAlpha()
			}
			return l.makeToken(DATEPM)
		} else if l.peek() == '.' {
			l.advance()
			if l.peek() == 'm' || l.peek() == 'M' {
				l.advance()
				if l.peek() == '.' {
					l.advance()
				}
				return l.makeToken(DATEPM)
			}
		}
		return l.consumeAlpha()
	} else {
		// TODO: Implement the rest of the lexer
		return l.makeToken(DATESEP)
	}
}

func (l *DateLexer) consumeAlpha() DateToken {
	for unicode.IsLetter(l.peek()) {
		l.advance()
	}
	return l.makeToken(DATESEP)
}

func ParseDateTime(dateTimeStr string) (time.Time, error) {
	l := NewDateLexer(dateTimeStr)
	tokens := make([]DateToken, 0)

	for {
		token := l.scanToken()
		if token.Type == DATEEOF {
			break
		}

		tokens = append(tokens, token)
	}

	time, err := ParseDateTimeTokens(tokens)
	return time, err
}

type DateParser struct {
	Tokens []DateToken
	Current int
	Error error
}

func (p *DateParser) CurrentToken() DateToken {
	if p.Current >= len(p.Tokens) {
		return DateToken{Type: DATEEOF, Lexeme: "", Start: -1}
	}
	return p.Tokens[p.Current]
}

func (p *DateParser) Peek() DateToken {
	if p.Current + 1 >= len(p.Tokens) {
		return DateToken{Type: DATEEOF, Lexeme: "", Start: -1}
	}
	return p.Tokens[p.Current + 1]
}

func (p *DateParser) Peek2() DateToken {
	if p.Current + 2 >= len(p.Tokens) {
		return DateToken{Type: DATEEOF, Lexeme: "", Start: -1}
	}
	return p.Tokens[p.Current + 2]
}

func (p *DateParser) ParseDate() (year int, month int, day int, err error) {
	if p.CurrentToken().Type == DATEINT4 {
		year, _ = p.ParseYear()

		month, err = p.ParseMonth()
		if err != nil {
			return 0, 0, 0, err
		}

		day, err = p.ParseDay()
		if err != nil {
			return 0, 0, 0, err
		}

		return year, month, day, nil
	} else if p.CurrentToken().IsMonth() {
		month, _ = p.ParseMonth()

		day, err = p.ParseDay()
		if err != nil {
			return 0, 0, 0, err
		}

		year, err = p.ParseYear()
		if err != nil {
			return 0, 0, 0, err
		}

		return year, month, day, nil
	} else if p.CurrentToken().Type == DATEINT2 {
		// Here we get some ambiguity.
		if p.Peek().IsMonth() {
			day, _ = p.ParseDay()
			month, err = p.ParseMonth()
			if err != nil {
				return 0, 0, 0, err
			}
			year, err = p.ParseYear()
			if err != nil {
				return 0, 0, 0, err
			}
			return year, month, day, nil
		} else if p.Peek2().Type == DATEINT4 {
			// month, day, year
			month, err = p.ParseMonth()
			if err != nil {
				return 0, 0, 0, err
			}

			day, err = p.ParseDay()
			if err != nil {
				return 0, 0, 0, err
			}

			year, err = p.ParseYear()
			if err != nil {
				return 0, 0, 0, err
			}
			return year, month, day, nil
		} else {
			// year, month, day
			year, err = p.ParseYear()
			if err != nil {
				return 0, 0, 0, err
			}

			month, err = p.ParseMonth()
			if err != nil {
				return 0, 0, 0, err
			}

			day, err = p.ParseDay()
			if err != nil {
				return 0, 0, 0, err
			}
			return year, month, day, nil
		}
	} else if p.CurrentToken().Type == DATEINT1 {
		day, _ = p.ParseDay()
		month, err = p.ParseMonth()
		if err != nil {
			return 0, 0, 0, err
		}
		year, err = p.ParseYear()
		if err != nil {
			return 0, 0, 0, err
		}
		return year, month, day, nil
	} else {
		err = fmt.Errorf("Expected 4 digit year or month name")
		return 0, 0, 0, err
	}
}

func (p *DateParser) ParseYear() (int, error) {
	if p.CurrentToken().Type == DATEINT4 {
		year, err := strconv.Atoi(p.CurrentToken().Lexeme)
		if err != nil {
			return 0, err
		}
		p.Current++
		return year, nil
	} else if p.CurrentToken().Type == DATEINT2 {
		year, err := strconv.Atoi(p.CurrentToken().Lexeme)
		if err != nil {
			return 0, err
		}
		p.Current++
		return year + 2000, nil
	} else {
		return 0, fmt.Errorf("Expected 4 or 2 digit year, received '%s' (%d)", p.CurrentToken().Lexeme, p.CurrentToken().Type)
	}
}

func (p *DateParser) ParseMonth() (int, error) {
	if p.CurrentToken().IsMonth() {
		month := int(p.CurrentToken().Type)
		p.Current++
		return month, nil
	} else if p.CurrentToken().Type == DATEINT2 {
		month, err := strconv.Atoi(p.CurrentToken().Lexeme)
		if err != nil {
			return 0, err
		}
		p.Current++
		return month, nil
	} else if  p.CurrentToken().Type == DATEINT1 {
		month, _ := strconv.Atoi(p.CurrentToken().Lexeme)
		p.Current++
		return month, nil
	} else {
		return 0, fmt.Errorf("Expected 2 digit month or month name")
	}
}

func (p *DateParser) ParseDay() (int, error) {
	if p.CurrentToken().Type == DATEINT2 {
		day, err := strconv.Atoi(p.CurrentToken().Lexeme)
		if err != nil {
			return 0, err
		}
		p.Current++
		return day, nil
	} else if p.CurrentToken().Type == DATEINT1 {
		day, _ := strconv.Atoi(p.CurrentToken().Lexeme)
		p.Current++
		return day, nil
	} else {
		return 0, fmt.Errorf("Expected 2 or 1 digit day")
	}
}


func ParseDateTimeTokens(dateTimeTokens []DateToken) (time.Time, error) {
	nonSepTokens := make([]DateToken, 0, len(dateTimeTokens))

	for _, token := range dateTimeTokens {
		if token.Type != DATESEP && token.Type != DATEDOW {
			nonSepTokens = append(nonSepTokens, token)
		}
	}

	parser := DateParser{Tokens: nonSepTokens, Current: 0}

	year, month, day, err := parser.ParseDate()
	if err != nil {
		return time.Time{}, err
	}

	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC), nil
}
