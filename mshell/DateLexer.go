package main

import (
	"errors"
	"fmt"
	"time"
)

type DateTokenType uint8

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

// DateToken is a slice of the input plus its classification.
// Value holds the numeric value for the DATEINT* types so it is never re-parsed.
// Lexeme is a substring of the original input, so making a token does not allocate.
type DateToken struct {
	Lexeme string
	Value  int32
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

// DateLexer scans the input bytes directly. Dates are ASCII; any non-ASCII byte is a separator.
type DateLexer struct {
	start   int
	current int
	input   string
}

func NewDateLexer(input string) *DateLexer {
	return &DateLexer{0, 0, input}
}

func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isLetter(c byte) bool { return (c|0x20) >= 'a' && (c|0x20) <= 'z' }

func (l *DateLexer) atEnd() bool {
	return l.current >= len(l.input)
}

func (l *DateLexer) Length() int {
	return l.current - l.start
}

func (l *DateLexer) curLexeme() string {
	return l.input[l.start:l.current]
}

func (l *DateLexer) makeToken(tokenType DateTokenType) DateToken {
	return DateToken{Lexeme: l.curLexeme(), Type: tokenType}
}

func (l *DateLexer) advance() byte {
	c := l.input[l.current]
	l.current++
	return c
}

func (l *DateLexer) peek() byte {
	if l.atEnd() {
		return 0
	}
	return l.input[l.current]
}

func (l *DateLexer) charFromStart(index int) byte {
	if l.start+index < len(l.input) {
		return l.input[l.start+index]
	}
	return 0
}

// eqFold compares an ASCII lexeme against a lowercase word without allocating.
func eqFold(s string, lower string) bool {
	if len(s) != len(lower) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i]|0x20 != lower[i] {
			return false
		}
	}
	return true
}

// MonthDowType classifies an alphabetic run as a month, a day of the week, or a separator.
func (l *DateLexer) MonthDowType() DateTokenType {
	lex := l.curLexeme()

	// Every word we know is 3 to 9 letters.
	if len(lex) < 3 || len(lex) > 9 {
		return DATESEP
	}

	switch lex[0] | 0x20 {
	case 'j':
		switch l.charFromStart(1) | 0x20 {
		case 'u':
			if eqFold(lex, "jun") || eqFold(lex, "june") {
				return DATEJUN
			} else if eqFold(lex, "jul") || eqFold(lex, "july") {
				return DATEJUL
			}
		case 'a':
			if eqFold(lex, "jan") || eqFold(lex, "january") {
				return DATEJAN
			}
		}
	case 'f':
		if eqFold(lex, "feb") || eqFold(lex, "february") {
			return DATEFEB
		} else if eqFold(lex, "fri") || eqFold(lex, "friday") {
			return DATEDOW
		}
	case 'm':
		switch l.charFromStart(1) | 0x20 {
		case 'a':
			if eqFold(lex, "mar") || eqFold(lex, "march") {
				return DATEMAR
			} else if eqFold(lex, "may") {
				return DATEMAY
			}
		case 'o':
			if eqFold(lex, "mon") || eqFold(lex, "monday") {
				return DATEDOW
			}
		}
	case 'a':
		switch l.charFromStart(1) | 0x20 {
		case 'p':
			if eqFold(lex, "apr") || eqFold(lex, "april") {
				return DATEAPR
			}
		case 'u':
			if eqFold(lex, "aug") || eqFold(lex, "august") {
				return DATEAUG
			}
		}
	case 's':
		switch l.charFromStart(1) | 0x20 {
		case 'a':
			if eqFold(lex, "sat") || eqFold(lex, "saturday") {
				return DATEDOW
			}
		case 'e':
			if eqFold(lex, "sep") || eqFold(lex, "september") {
				return DATESEPT
			}
		case 'u':
			if eqFold(lex, "sun") || eqFold(lex, "sunday") {
				return DATEDOW
			}
		}
	case 'o':
		if eqFold(lex, "oct") || eqFold(lex, "october") {
			return DATEOCT
		}
	case 'n':
		if eqFold(lex, "nov") || eqFold(lex, "november") {
			return DATENOV
		}
	case 'd':
		if eqFold(lex, "dec") || eqFold(lex, "december") {
			return DATEDEC
		}
	case 't':
		if eqFold(lex, "tue") || eqFold(lex, "tuesday") || eqFold(lex, "thu") || eqFold(lex, "thursday") {
			return DATEDOW
		}
	case 'w':
		if eqFold(lex, "wed") || eqFold(lex, "wednesday") {
			return DATEDOW
		}
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

	if isDigit(c) {
		value := int32(c - '0')
		for isDigit(l.peek()) {
			// Only 1, 2 and 4 digit runs are used. Stop accumulating so a long digit run
			// (fractional seconds) cannot overflow before it is classed as a separator.
			if l.Length() < 9 {
				value = value*10 + int32(l.peek()-'0')
			}
			l.advance()
		}
		var tokenType DateTokenType
		switch l.Length() {
		case 4:
			tokenType = DATEINT4
		case 2:
			tokenType = DATEINT2
		case 1:
			tokenType = DATEINT1
		default: // TODO: Handle 8 and 6 digit numbers
			return l.makeToken(DATESEP)
		}
		return DateToken{Lexeme: l.curLexeme(), Value: value, Type: tokenType}
	} else if c == 'a' || c == 'A' {

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

		for isLetter(l.peek()) {
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
		switch c | 0x20 {
		case 'j', 'f', 'm', 's', 'o', 'n', 'd', 't', 'w':
			for isLetter(l.peek()) {
				l.advance()
			}
			return l.makeToken(l.MonthDowType())
		}
		// TODO: Implement the rest of the lexer
		return l.makeToken(DATESEP)
	}
}

func (l *DateLexer) consumeAlpha() DateToken {
	for isLetter(l.peek()) {
		l.advance()
	}
	return l.makeToken(DATESEP)
}

// DateOrder is the numeric date order learned during an evaluation.
// It starts unset and is set by each unambiguous date that is not a leading 4 digit year.
type DateOrder uint8

const (
	ORDER_UNSET DateOrder = iota
	ORDER_YMD
	ORDER_MDY
	ORDER_DMY
)

var (
	errDateTooShort   = errors.New("Expected three date components")
	errDateNoReading  = errors.New("No valid reading of the date in any order")
	errDateAmbiguous  = errors.New("Ambiguous date with no prior unambiguous date to establish the order")
	errDateNotInOrder = errors.New("Date is not valid in the established order")
	errDateTrailing   = errors.New("Unexpected trailing token after the date/time")
	errTimeExpected   = errors.New("Expected time")
	errTimeComponent  = errors.New("Expected 1 or 2 digit time component")
	errTimeOutOfRange = errors.New("Time component out of range")
)

// maxDateTokens is the size of the on-stack token buffer. A full date and time with
// AM/PM is 8 tokens after separators are dropped; longer inputs spill to the heap.
const maxDateTokens = 12

func ParseDateTime(dateTimeStr string, order *DateOrder) (time.Time, error) {
	var buf [maxDateTokens]DateToken
	tokens := buf[:0]

	l := DateLexer{0, 0, dateTimeStr}
	for {
		token := l.scanToken()
		if token.Type == DATEEOF {
			break
		}
		// Separators and day of week names carry no information for the parser.
		if token.Type != DATESEP && token.Type != DATEDOW {
			tokens = append(tokens, token)
		}
	}

	return ParseDateTimeTokens(tokens, order)
}

type DateParser struct {
	Tokens  []DateToken
	Current int
	Order   *DateOrder
}

func (p *DateParser) CurrentToken() DateToken {
	if p.Current >= len(p.Tokens) {
		return DateToken{Type: DATEEOF}
	}
	return p.Tokens[p.Current]
}

func (p *DateParser) AtEnd() bool {
	return p.Current >= len(p.Tokens)
}

// daysInMonth is indexed by month (1-12) for a non leap year.
var daysInMonth = [13]int32{0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}

func isLeap(year int32) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

// Interpret a token as a year. 4 digit is taken literally, 2 digit is 2000+. 1 digit is not a year.
func (token DateToken) asYear() (int32, bool) {
	switch token.Type {
	case DATEINT4:
		return token.Value, true
	case DATEINT2:
		return token.Value + 2000, true
	}
	return 0, false
}

func (token DateToken) asMonth() (int32, bool) {
	if token.IsMonth() {
		return int32(token.Type), true
	}
	if token.Type == DATEINT1 || token.Type == DATEINT2 {
		return token.Value, token.Value >= 1 && token.Value <= 12
	}
	return 0, false
}

func (token DateToken) asDay() (int32, bool) {
	if token.Type == DATEINT1 || token.Type == DATEINT2 {
		return token.Value, token.Value >= 1 && token.Value <= 31
	}
	return 0, false
}

type dateReading struct {
	year, month, day int32
}

// Try to read tokens y, m, d as a full date, checking that the day exists in that month.
func tryReading(y DateToken, m DateToken, d DateToken) (dateReading, bool) {
	year, okY := y.asYear()
	month, okM := m.asMonth()
	day, okD := d.asDay()
	if !(okY && okM && okD) {
		return dateReading{}, false
	}
	limit := daysInMonth[month]
	if month == 2 && isLeap(year) {
		limit = 29
	}
	if day > limit {
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
func (p *DateParser) ParseDate() (dateReading, error) {
	if p.Current+3 > len(p.Tokens) {
		return dateReading{}, errDateTooShort
	}
	t0, t1, t2 := p.Tokens[p.Current], p.Tokens[p.Current+1], p.Tokens[p.Current+2]

	// Indexed by DateOrder. ORDER_UNSET is never valid.
	var readings [4]dateReading
	var valid [4]bool
	readings[ORDER_YMD], valid[ORDER_YMD] = tryReading(t0, t1, t2)
	readings[ORDER_MDY], valid[ORDER_MDY] = tryReading(t2, t0, t1)
	readings[ORDER_DMY], valid[ORDER_DMY] = tryReading(t2, t1, t0)

	// Find the first valid reading and whether any other valid reading differs from it.
	first := ORDER_UNSET
	distinct := false
	for o := ORDER_YMD; o <= ORDER_DMY; o++ {
		if !valid[o] {
			continue
		}
		if first == ORDER_UNSET {
			first = o
		} else if readings[o] != readings[first] {
			distinct = true
		}
	}

	var chosen dateReading
	if first == ORDER_UNSET {
		return dateReading{}, errDateNoReading
	} else if !distinct {
		chosen = readings[first]
		if p.Order != nil && t0.Type != DATEINT4 {
			// Record the order as evidence; the most recent unambiguous date wins.
			// Prefer the non-YMD reading when two orders produce the same date,
			// since that is the one that says something about m/d.
			if valid[ORDER_MDY] {
				*p.Order = ORDER_MDY
			} else if valid[ORDER_DMY] {
				*p.Order = ORDER_DMY
			} else {
				*p.Order = ORDER_YMD
			}
		}
	} else if p.Order != nil && *p.Order != ORDER_UNSET {
		if !valid[*p.Order] {
			return dateReading{}, errDateNotInOrder
		}
		chosen = readings[*p.Order]
	} else {
		return dateReading{}, errDateAmbiguous
	}

	p.Current += 3
	return chosen, nil
}

// parseTimeComponent reads a 1 or 2 digit integer.
func (p *DateParser) parseTimeComponent() (int32, error) {
	t := p.CurrentToken()
	if t.Type == DATEINT2 || t.Type == DATEINT1 {
		p.Current++
		return t.Value, nil
	}
	return 0, errTimeComponent
}

// applyMeridiem consumes an AM or PM token if present and adjusts the hour.
func (p *DateParser) applyMeridiem(hour int32) int32 {
	switch p.CurrentToken().Type {
	case DATEAM:
		if hour == 12 {
			hour = 0
		}
		p.Current++
	case DATEPM:
		if hour < 12 {
			hour += 12
		}
		p.Current++
	}
	return hour
}

func (p *DateParser) ParseTime() (hour int32, minute int32, second int32, err error) {
	t := p.CurrentToken()
	if !(t.Type == DATEINT1 || t.Type == DATEINT2) {
		return 0, 0, 0, errTimeExpected
	}
	hour, _ = p.parseTimeComponent()

	if p.AtEnd() {
		return hour, 0, 0, nil
	}
	minute, err = p.parseTimeComponent()
	if err != nil {
		// Matches prior behavior: a non numeric token after the hour ends the time.
		return hour, 0, 0, nil
	}

	if p.AtEnd() {
		return hour, minute, 0, nil
	}

	t = p.CurrentToken()
	if t.Type == DATEAM || t.Type == DATEPM {
		hour = p.applyMeridiem(hour)
	} else {
		second, err = p.parseTimeComponent()
		if err != nil {
			return 0, 0, 0, err
		}
		hour = p.applyMeridiem(hour)
	}

	return hour, minute, second, nil
}

func ParseDateTimeTokens(tokens []DateToken, order *DateOrder) (time.Time, error) {
	parser := DateParser{Tokens: tokens, Current: 0, Order: order}

	date, err := parser.ParseDate()
	if err != nil {
		return time.Time{}, err
	}

	var hour, minute, second int32
	if !parser.AtEnd() {
		hour, minute, second, err = parser.ParseTime()
		if err != nil {
			return time.Time{}, err
		}
	}

	if !parser.AtEnd() {
		return time.Time{}, errDateTrailing
	}

	// The date components were validated by tryReading. Validate the time here so
	// time.Date never has to normalize anything and the result is exactly what was written.
	if hour > 23 || minute > 59 || second > 59 {
		return time.Time{}, errTimeOutOfRange
	}

	return time.Date(int(date.year), time.Month(date.month), int(date.day), int(hour), int(minute), int(second), 0, time.UTC), nil
}
