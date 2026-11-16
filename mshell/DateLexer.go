package main

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
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
	DATEINTN // A run of digits of any other length.
	DATEEOF
)

// DateToken is a slice of the input plus its classification.
// Value holds the numeric value for the DATEINT* types so it is never re-parsed.
// For a number of more than nine digits it is the value of the first nine.
// Lexeme is a substring of the original input, so making a token does not allocate.
// Prev is the last punctuation character between the previous number, month or AM/PM
// and this token, or 0 if there was none. Whitespace and letters are not punctuation, so
// Prev is '-' for the 05 in "32 -05", and 0 for the 15 in "17T15". Every Unicode minus
// sign is '-' and every Unicode plus sign is '+'.
type DateToken struct {
	Lexeme string
	Value  int32
	Type   DateTokenType
	Prev   byte
}

// IsInt reports whether the token is a run of digits of any length.
func (token DateToken) IsInt() bool {
	return token.Type >= DATEINT4 && token.Type <= DATEINTN
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

// DateLexer scans the input bytes directly. Dates are ASCII; a non-ASCII character is
// a minus or plus sign, or it is skipped.
type DateLexer struct {
	start   int
	current int
	input   string
	// prev is the last punctuation character since the last token.
	prev byte
}

func NewDateLexer(input string) *DateLexer {
	return &DateLexer{input: input}
}

// unicodeSign gives '-' or '+' for the non-ASCII characters used as a minus or plus sign,
// and 0 for every other character.
func unicodeSign(r rune) byte {
	switch r {
	case '\u2212', // minus sign, the one ISO 8601 prefers
		'\u2010', // hyphen
		'\u2011', // non-breaking hyphen
		'\u2012', // figure dash
		'\u2013', // en dash
		'\uFE63', // small hyphen-minus
		'\uFF0D': // fullwidth hyphen-minus
		return '-'
	case '\uFE62', // small plus sign
		'\uFF0B': // fullwidth plus sign
		return '+'
	}
	return 0
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

// makeToken gives a token for the text scanned so far, with the last punctuation
// character that came before it.
func (l *DateLexer) makeToken(tokenType DateTokenType) DateToken {
	token := DateToken{Lexeme: l.curLexeme(), Type: tokenType, Prev: l.prev}
	l.prev = 0
	return token
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

// scanToken gives the next number, month or AM/PM, or a DATEEOF token at the end of the
// input. Whitespace, punctuation, day of week names and every other word are skipped here
// and never become tokens, since the parser has no use for them.
func (l *DateLexer) scanToken() DateToken {
	// https://en.wikipedia.org/wiki/List_of_time_zone_abbreviations
next:
	l.start = l.current

	if l.atEnd() {
		return l.makeToken(DATEEOF)
	}

	c := l.advance()

	if isDigit(c) {
		value := int32(c - '0')
		// The input, the start and the position are held in local variables for this loop,
		// so each digit costs no read or write of the lexer's fields.
		input, start, i := l.input, l.start, l.current
		for i < len(input) && isDigit(input[i]) {
			// Keep the value of the first nine digits. That is every digit of a 1, 2 or 4
			// digit number, and a fraction of a second down to the nanosecond. A tenth
			// digit could overflow.
			if i-start < 9 {
				value = value*10 + int32(input[i]-'0')
			}
			i++
		}
		l.current = i
		var tokenType DateTokenType
		switch l.Length() {
		case 4:
			tokenType = DATEINT4
		case 2:
			tokenType = DATEINT2
		case 1:
			tokenType = DATEINT1
		default: // TODO: Handle 8 and 6 digit numbers
			tokenType = DATEINTN
		}
		token := l.makeToken(tokenType)
		token.Value = value
		return token
	} else if c == 'a' || c == 'A' {

		if l.peek() == 'm' || l.peek() == 'M' {
			l.advance()

			// Check for AMST or AMT for Amazon summer time or Armenia time
			if l.peek() == 's' || l.peek() == 'S' || l.peek() == 'T' {
				l.consumeAlpha()
				goto next
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
		if tokenType := l.MonthDowType(); tokenType != DATESEP {
			if tokenType != DATEDOW {
				return l.makeToken(tokenType)
			}
			// Punctuation before a day of week name is forgotten, as it is before a token.
			l.prev = 0
		}
		goto next
	} else if c == 'p' || c == 'P' {
		if l.peek() == 'm' || l.peek() == 'M' {
			l.advance()
			// Checking for PMDT or PMST or Saint Pierre and Miquelon Daylight/Standard Time
			if l.peek() == 's' || l.peek() == 'S' || l.peek() == 'D' || l.peek() == 'd' {
				l.consumeAlpha()
				goto next
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
		l.consumeAlpha()
		goto next
	} else {
		switch c | 0x20 {
		case 'j', 'f', 'm', 's', 'o', 'n', 'd', 't', 'w':
			for isLetter(l.peek()) {
				l.advance()
			}
			if tokenType := l.MonthDowType(); tokenType != DATESEP {
				if tokenType != DATEDOW {
					return l.makeToken(tokenType)
				}
				// Punctuation before a day of week name is forgotten, as it is before a token.
				l.prev = 0
			}
			goto next
		}
		// TODO: Implement the rest of the lexer
		if c >= utf8.RuneSelf {
			// Take the whole character. Only a minus or plus sign is remembered.
			r, size := utf8.DecodeRuneInString(l.input[l.start:])
			l.current = l.start + size
			if sign := unicodeSign(r); sign != 0 {
				l.prev = sign
			}
		} else if !isLetter(c) && c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			// Punctuation. Whitespace and letters are skipped without being remembered.
			l.prev = c
		}
		goto next
	}
}

// consumeAlpha skips the rest of a word.
func (l *DateLexer) consumeAlpha() {
	for isLetter(l.peek()) {
		l.advance()
	}
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

// maxDateTokens is the most tokens the parser reads: three for the date, then the hour,
// minute, second, fraction of a second and AM/PM. Every token after these is ignored, so
// the lexer stops at this many. A long input with a date at the front costs the same as
// the date alone, and the tokens never need memory from the heap.
// TestTokensPastMaxAreIgnored checks that later tokens never change the result.
const maxDateTokens = 8

func ParseDateTime(dateTimeStr string, order *DateOrder) (time.Time, error) {
	var tokens [maxDateTokens]DateToken
	count := 0

	l := DateLexer{input: dateTimeStr}
	for count < maxDateTokens {
		token := l.scanToken()
		if token.Type == DATEEOF {
			break
		}
		tokens[count] = token
		count++
	}

	return ParseDateTimeTokens(tokens[:count], order)
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

// nanosecondsPerFractionUnit is indexed by the number of digits in a fraction of a second.
// It is what one unit of the last digit is worth: a 1 digit fraction counts tenths of a
// second, and a 9 digit fraction counts nanoseconds.
var nanosecondsPerFractionUnit = [10]int32{0, 100000000, 10000000, 1000000, 100000, 10000, 1000, 100, 10, 1}

// fractionNanoseconds reads a number as the fraction of a second: 5 is half a second and
// 1463 is 146,300,000 nanoseconds. Digits past the ninth are dropped, which the lexer has
// already done in token.Value.
func fractionNanoseconds(token DateToken) int32 {
	return token.Value * nanosecondsPerFractionUnit[min(len(token.Lexeme), 9)]
}

// startsOffset reports whether token is the first number of a UTC offset, like the 05 in
// +05:30 or the 0500 in -0500. previousPart is the time component read just before it.
// A '+' always starts an offset. A '-' starts one unless the time itself is written with
// '-' between its parts, as in the file name style 15-00-32.
func startsOffset(token DateToken, previousPart DateToken) bool {
	if !token.IsInt() {
		return false
	}
	return token.Prev == '+' || (token.Prev == '-' && previousPart.Prev != '-')
}

// skipOffsetAfterMeridiem ignores a UTC offset written after AM/PM, like the -05:00 in
// "3:00 PM -05:00", along with anything after it.
func (p *DateParser) skipOffsetAfterMeridiem() {
	if t := p.CurrentToken(); t.IsInt() && (t.Prev == '+' || t.Prev == '-') {
		p.Current = len(p.Tokens)
	}
}

// ParseTime reads hour, minute, second, and fraction of a second, with AM/PM allowed after
// the hour or later. A UTC offset ends the time. The offset and anything after it are
// ignored, since a date/time has no time zone.
func (p *DateParser) ParseTime() (hour int32, minute int32, second int32, nanosecond int32, err error) {
	t := p.CurrentToken()
	if t.IsInt() && t.Prev == '+' {
		// A date followed directly by an offset, like 2026-08-17+05:00.
		p.Current = len(p.Tokens)
		return 0, 0, 0, 0, nil
	}
	if !(t.Type == DATEINT1 || t.Type == DATEINT2) {
		return 0, 0, 0, 0, errTimeExpected
	}
	hour, _ = p.parseTimeComponent()

	if p.AtEnd() {
		return hour, 0, 0, 0, nil
	}
	minuteToken := p.CurrentToken()
	if minuteToken.Type == DATEAM || minuteToken.Type == DATEPM {
		// Only an hour, like 5 PM.
		hour = p.applyMeridiem(hour)
		p.skipOffsetAfterMeridiem()
		return hour, 0, 0, 0, nil
	}
	if minuteToken.IsInt() && minuteToken.Prev == '+' {
		// Only '+' is an offset here. In 15-00 the '-' is between the hour and the minute.
		p.Current = len(p.Tokens)
		return hour, 0, 0, 0, nil
	}
	minute, err = p.parseTimeComponent()
	if err != nil {
		// Matches prior behavior: a non numeric token after the hour ends the time.
		return hour, 0, 0, 0, nil
	}

	if p.AtEnd() {
		return hour, minute, 0, 0, nil
	}

	secondToken := p.CurrentToken()
	if secondToken.Type == DATEAM || secondToken.Type == DATEPM {
		hour = p.applyMeridiem(hour)
		p.skipOffsetAfterMeridiem()
		return hour, minute, 0, 0, nil
	}
	if startsOffset(secondToken, minuteToken) {
		p.Current = len(p.Tokens)
		return hour, minute, 0, 0, nil
	}

	second, err = p.parseTimeComponent()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	// A number of any length right after the seconds is the fraction of a second,
	// whatever separates the two, unless it starts an offset.
	if t = p.CurrentToken(); t.IsInt() && !startsOffset(t, secondToken) {
		nanosecond = fractionNanoseconds(t)
		p.Current++
	}
	hour = p.applyMeridiem(hour)
	// Anything after the seconds, the fraction and AM/PM is ignored.
	p.Current = len(p.Tokens)

	return hour, minute, second, nanosecond, nil
}

func ParseDateTimeTokens(tokens []DateToken, order *DateOrder) (time.Time, error) {
	parser := DateParser{Tokens: tokens, Current: 0, Order: order}

	date, err := parser.ParseDate()
	if err != nil {
		return time.Time{}, err
	}

	var hour, minute, second, nanosecond int32
	if !parser.AtEnd() {
		hour, minute, second, nanosecond, err = parser.ParseTime()
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

	return time.Date(int(date.year), time.Month(date.month), int(date.day), int(hour), int(minute), int(second), int(nanosecond), time.UTC), nil
}
