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
	DATEINT = iota
	DATEJAN // 1, rest of months aligned, so don't change that.
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
		return l.makeToken(DATESEP)
	}
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

func ParseDateTimeTokens(dateTimeTokens []DateToken) (time.Time, error) {
	nonSepTokens := make([]DateToken, 0, len(dateTimeTokens))
	counts := make([]int, 16)

	dateMonthToken := -1

	for _, token := range dateTimeTokens {
		if token.Type != DATESEP && token.Type != DATEEOF {
			nonSepTokens = append(nonSepTokens, token)
			counts[token.Type] = counts[token.Type] + 1
		}

		if token.Type >= 1 && token.Type <= 12 {
			dateMonthToken = int(token.Type)
		}
	}

	if len(nonSepTokens) == 3 && counts[DATEINT] == 3 {
		// We are dealing with a date.
		if len(nonSepTokens[0].Lexeme) == 4 {
			yearInt, err := strconv.Atoi(nonSepTokens[0].Lexeme)
			// Assume this is a YYYY-MM-DD
			monthInt, err := strconv.Atoi(nonSepTokens[1].Lexeme)
			if err != nil {
				return time.Time{}, err
			}

			if monthInt < 1 || monthInt > 12 {
				return time.Time{}, fmt.Errorf("Month integer found to be '%d'", monthInt)
			}

			dayInt, err := strconv.Atoi(nonSepTokens[2].Lexeme)
			if err != nil {
				return time.Time{}, err
			}

			if dayInt < 0 || dayInt > 31 {
				return time.Time{}, fmt.Errorf("Day integer found to be '%d'", dayInt)
			}

			month := time.Month(monthInt)
			return time.Date(yearInt, month, dayInt, 0, 0, 0, 0, time.UTC), nil
		}
	} else if len(nonSepTokens) == 3 && counts[DATEINT] == 2 && dateMonthToken != -1 {
		if nonSepTokens[0].IsMonth() {
			// Assume 1 is day, and 2 is year
			dayInt, err := nonSepTokens[1].ParseDay()
			if err != nil {
				return time.Time{}, err
			}

			yearInt := nonSepTokens[2].ParseYear()
			return time.Date(yearInt, nonSepTokens[0].Month(), dayInt, 0, 0, 0, 0, time.UTC), nil

		} else if nonSepTokens[1].IsMonth() {
			// Assume 0 is year, 2 in day
			dayInt, err := nonSepTokens[2].ParseDay()
			if err != nil {
				return time.Time{}, err
			}

			yearInt := nonSepTokens[0].ParseYear()
			return time.Date(yearInt, nonSepTokens[1].Month(), dayInt, 0, 0, 0, 0, time.UTC), nil
		}
	}

	return time.Time{}, fmt.Errorf("Could not parse datetime %d %d", len(nonSepTokens), len(dateTimeTokens))
}
