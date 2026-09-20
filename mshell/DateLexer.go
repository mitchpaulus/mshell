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

		if l.peek() == 'm' || l.peek() == 'M' {
			c = l.advance()

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

// DateOrder is the numeric date order learned during an evaluation.
// It starts unset and is set by each unambiguous date that is not a leading 4 digit year.
type DateOrder int

const (
	ORDER_UNSET DateOrder = iota
	ORDER_YMD
	ORDER_MDY
	ORDER_DMY
)

func ParseDateTime(dateTimeStr string, order *DateOrder) (time.Time, error) {
	l := NewDateLexer(dateTimeStr)
	tokens := make([]DateToken, 0)

	for {
		token := l.scanToken()
		if token.Type == DATEEOF {
			break
		}

		tokens = append(tokens, token)
	}

	time, err := ParseDateTimeTokens(tokens, order)
	return time, err
}

type DateParser struct {
	Tokens []DateToken
	Current int
	Error error
	Order *DateOrder
}

func (p *DateParser) CurrentToken() DateToken {
	if p.Current >= len(p.Tokens) {
		return DateToken{Type: DATEEOF, Lexeme: "", Start: -1}
	}
	return p.Tokens[p.Current]
}

func (p *DateParser) AtEnd() bool {
	return p.CurrentToken().Type == DATEEOF
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

// Interpret a token as a year. 4 digit is taken literally, 2 digit is 2000+. 1 digit is not a year.
func (token DateToken) asYear() (int, bool) {
	if token.Type == DATEINT4 {
		y, _ := strconv.Atoi(token.Lexeme)
		return y, true
	} else if token.Type == DATEINT2 {
		y, _ := strconv.Atoi(token.Lexeme)
		return y + 2000, true
	}
	return 0, false
}

func (token DateToken) asMonth() (int, bool) {
	if token.IsMonth() {
		return int(token.Type), true
	} else if token.Type == DATEINT1 || token.Type == DATEINT2 {
		m, _ := strconv.Atoi(token.Lexeme)
		return m, m >= 1 && m <= 12
	}
	return 0, false
}

func (token DateToken) asDay() (int, bool) {
	if token.Type == DATEINT1 || token.Type == DATEINT2 {
		d, _ := strconv.Atoi(token.Lexeme)
		return d, d >= 1 && d <= 31
	}
	return 0, false
}

type dateReading struct {
	year, month, day int
}

// Try to read tokens y, m, d as a full date, checking that the day exists in that month.
func tryReading(y DateToken, m DateToken, d DateToken) (dateReading, bool) {
	year, okY := y.asYear()
	month, okM := m.asMonth()
	day, okD := d.asDay()
	if !(okY && okM && okD) {
		return dateReading{}, false
	}
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if t.Year() != year || int(t.Month()) != month || t.Day() != day {
		return dateReading{}, false
	}
	return dateReading{year, month, day}, true
}

// ParseDate consumes three tokens and tries the three orders in real world use:
// year-month-day, month-day-year, day-month-year.
// If exactly one distinct date is valid, that is the answer, and it sets the order used for
// later ambiguous dates, unless it was forced by a leading 4 digit year (ISO is universal).
// If several distinct dates are valid, the learned order decides. With no learned order the
// date is ambiguous and an error is returned.
func (p *DateParser) ParseDate() (year int, month int, day int, err error) {
	t0, t1, t2 := p.CurrentToken(), p.Peek(), p.Peek2()
	if t0.Type == DATEEOF || t1.Type == DATEEOF || t2.Type == DATEEOF {
		return 0, 0, 0, fmt.Errorf("Expected three date components")
	}

	readings := make(map[DateOrder]dateReading)
	if r, ok := tryReading(t0, t1, t2); ok { readings[ORDER_YMD] = r }
	if r, ok := tryReading(t2, t0, t1); ok { readings[ORDER_MDY] = r }
	if r, ok := tryReading(t2, t1, t0); ok { readings[ORDER_DMY] = r }

	distinct := make(map[dateReading]bool)
	for _, r := range readings { distinct[r] = true }

	var chosen dateReading
	if len(distinct) == 0 {
		return 0, 0, 0, fmt.Errorf("No valid date reading for '%s %s %s'", t0.Lexeme, t1.Lexeme, t2.Lexeme)
	} else if len(distinct) == 1 {
		for r := range distinct { chosen = r }
		if p.Order != nil && t0.Type != DATEINT4 {
			// Record the order as evidence; the most recent unambiguous date wins.
			// Prefer the non-YMD reading when two orders produce the same date,
			// since that is the one that says something about m/d.
			for _, o := range []DateOrder{ORDER_MDY, ORDER_DMY, ORDER_YMD} {
				if _, ok := readings[o]; ok {
					*p.Order = o
					break
				}
			}
		}
	} else if p.Order != nil && *p.Order != ORDER_UNSET {
		r, ok := readings[*p.Order]
		if !ok {
			return 0, 0, 0, fmt.Errorf("Date '%s %s %s' is not valid in the established order", t0.Lexeme, t1.Lexeme, t2.Lexeme)
		}
		chosen = r
	} else {
		return 0, 0, 0, fmt.Errorf("Ambiguous date '%s %s %s' with no prior unambiguous date to establish the order", t0.Lexeme, t1.Lexeme, t2.Lexeme)
	}

	p.Current += 3
	return chosen.year, chosen.month, chosen.day, nil
}

func (p *DateParser) ParseTime() (int, int, int, error) {
	hour, minute, second := 0, 0, 0

	if !(p.CurrentToken().Type == DATEINT1 || p.CurrentToken().Type == DATEINT2) {
		return 0, 0, 0, fmt.Errorf("Expected time")
	}
	hour, err := p.ParseHour()
	if err != nil {
		return 0, 0, 0, err
	}

	if p.CurrentToken().Type == DATEEOF {
		return hour, 0, 0, nil
	}
	minute, err = p.ParseMinute()
	if err != nil {
		return 0, 0, 0, nil
	}

	if p.CurrentToken().Type == DATEEOF {
		return hour, minute, 0, nil
	} else if p.CurrentToken().Type == DATEAM || p.CurrentToken().Type == DATEPM {
		if p.CurrentToken().Type == DATEAM {
			if hour == 12 {
				hour = 0
			}
		} else {
			if hour < 12 {
				hour += 12
			}
		}

		p.Current++
	} else {
		second, err = p.ParseSecond()
		if err != nil {
			return 0, 0, 0, err
		}
	}

	if p.CurrentToken().Type == DATEAM || p.CurrentToken().Type == DATEPM {
		if p.CurrentToken().Type == DATEAM {
			if hour == 12 {
				hour = 0
			}
		} else {
			if hour < 12 {
				hour += 12
			}
		}

		p.Current++
	}

	return hour, minute, second, nil
}

func (p *DateParser) ParseHour() (int, error) {
	if p.CurrentToken().Type == DATEINT2 || p.CurrentToken().Type == DATEINT1 {
		hour, err := strconv.Atoi(p.CurrentToken().Lexeme)
		if err != nil {
			return 0, err
		}
		p.Current++
		return hour, nil
	} else {
		return 0, fmt.Errorf("Expected 2 or 1 digit hour")
	}
}

func (p *DateParser) ParseMinute() (int, error) {
	if p.CurrentToken().Type == DATEINT2 || p.CurrentToken().Type == DATEINT1 {
		minute, err := strconv.Atoi(p.CurrentToken().Lexeme)
		if err != nil {
			return 0, err
		}
		p.Current++
		return minute, nil
	} else {
		return 0, fmt.Errorf("Expected 2 or 1 digit minute")
	}
}

func (p *DateParser) ParseSecond() (int, error) {
	if p.CurrentToken().Type == DATEINT2 || p.CurrentToken().Type == DATEINT1 {
		second, err := strconv.Atoi(p.CurrentToken().Lexeme)
		if err != nil {
			return 0, err
		}
		p.Current++
		return second, nil
	} else {
		return 0, fmt.Errorf("Expected 2 or 1 digit second")
	}
}

func ParseDateTimeTokens(dateTimeTokens []DateToken, order *DateOrder) (time.Time, error) {
	nonSepTokens := make([]DateToken, 0, len(dateTimeTokens))

	for _, token := range dateTimeTokens {
		if token.Type != DATESEP && token.Type != DATEDOW {
			nonSepTokens = append(nonSepTokens, token)
		}
	}

	parser := DateParser{Tokens: nonSepTokens, Current: 0, Order: order}

	year, month, day, err := parser.ParseDate()
	if err != nil {
		return time.Time{}, err
	}

	hour, minute, second := 0, 0, 0
	if !parser.AtEnd() {
		hour, minute, second, err = parser.ParseTime()
		if err != nil {
			return time.Time{}, err
		}
	}

	if !parser.AtEnd() {
		return time.Time{}, fmt.Errorf("Unexpected trailing token '%s'", parser.CurrentToken().Lexeme)
	}

	result := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC)

	// time.Date normalizes out of range values (month 16 becomes April of the next
	// year, Feb 30 becomes Mar 2). Reject anything that did not round trip exactly.
	if result.Year() != year || int(result.Month()) != month || result.Day() != day ||
		result.Hour() != hour || result.Minute() != minute || result.Second() != second {
		return time.Time{}, fmt.Errorf("Date/time component out of range: %d-%d-%d %d:%d:%d", year, month, day, hour, minute, second)
	}

	return result, nil
}
