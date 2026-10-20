package main

// A TOML v1.0.0 parser that builds mshell values directly.
// https://toml.io/en/v1.0.0

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type tomlError struct {
	msg  string
	line int
	col  int
}

func (e *tomlError) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.line, e.col, e.msg)
}

// How a table came to exist, which decides what may add to it later. A table
// not in tomlParser.kinds was defined by a [header], is an element of an
// array of tables, or is the root.
const (
	tomlTableImplicit uint8 = iota + 1 // created as a parent by a [header]; a later [header] may define it
	tomlTableDotted                    // created by a dotted key; only more dotted keys may add to it
	tomlTableInline                    // an inline table; nothing may add to it
)

type tomlParser struct {
	s     string
	i     int
	kinds map[*MShellDict]uint8
	// Arrays made by [[header]]. Other arrays cannot be added to.
	tableArrays map[*MShellList]struct{}
	// Reused by keyPath.
	keys []string
	// Values of the arrays being parsed, so each list is made at its final size.
	values []MShellObject
}

// decodeToml parses a TOML document. A document is a table, so it always
// gives a dict. Datetimes become datetimes: one with an offset is converted to
// UTC, and a local one (no offset) keeps its clock time. A local date is a
// datetime at midnight. A local time has no date, so it is a string, such as
// "07:32:00".
func decodeToml(s string) (dict *MShellDict, err error) {
	if !utf8.ValidString(s) {
		return nil, fmt.Errorf("input is not valid UTF-8")
	}
	s = strings.TrimPrefix(s, "\xef\xbb\xbf")
	p := &tomlParser{s: s, kinds: map[*MShellDict]uint8{}, tableArrays: map[*MShellList]struct{}{}}
	// Errors are raised with panic from deep in the parse and caught here.
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*tomlError); ok {
				dict, err = nil, e
				return
			}
			panic(r)
		}
	}()
	root := NewDict()
	table := root
	for {
		p.skipBlank()
		if p.i >= len(p.s) {
			return root, nil
		}
		if p.s[p.i] == '[' {
			table = p.header(root)
		} else {
			p.keyValue(table)
		}
		p.endOfLine()
	}
}

func (p *tomlParser) fail(msg string) {
	line := strings.Count(p.s[:p.i], "\n") + 1
	col := p.i - strings.LastIndexByte(p.s[:p.i], '\n')
	panic(&tomlError{msg: msg, line: line, col: col})
}

func (p *tomlParser) peek() byte {
	if p.i < len(p.s) {
		return p.s[p.i]
	}
	return 0
}

func (p *tomlParser) expect(c byte) {
	if p.peek() != c {
		p.fail(fmt.Sprintf("expected '%c'", c))
	}
	p.i++
}

func (p *tomlParser) skipWs() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t') {
		p.i++
	}
}

func (p *tomlParser) comment() {
	p.i++
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '\n' || c == '\r' {
			return
		}
		if !tomlIsTextChar(c) {
			p.fail("control character in comment")
		}
		p.i++
	}
}

// newline reads "\n" or "\r\n" if one is next, and reports whether it did. A
// carriage return on its own is an error.
func (p *tomlParser) newline() bool {
	switch p.peek() {
	case '\n':
		p.i++
		return true
	case '\r':
		if p.i+1 < len(p.s) && p.s[p.i+1] == '\n' {
			p.i += 2
			return true
		}
		p.fail("carriage return without a line feed")
	}
	return false
}

// skipBlank skips whitespace, comments and newlines.
func (p *tomlParser) skipBlank() {
	for {
		p.skipWs()
		if p.peek() == '#' {
			p.comment()
		}
		if !p.newline() {
			return
		}
	}
}

func (p *tomlParser) endOfLine() {
	p.skipWs()
	if p.peek() == '#' {
		p.comment()
	}
	if p.i < len(p.s) && !p.newline() {
		p.fail("expected the end of the line")
	}
}

// header reads [a.b] or [[a.b]] and gives the table that the key/value lines
// after it go in.
func (p *tomlParser) header(root *MShellDict) *MShellDict {
	p.i++
	isArray := p.peek() == '['
	if isArray {
		p.i++
	}
	keys := p.keyPath()
	p.expect(']')
	if isArray {
		p.expect(']')
	}
	table := root
	for _, key := range keys[:len(keys)-1] {
		table = p.headerParent(table, key)
	}
	last := keys[len(keys)-1]
	existing, exists := table.Items[last]

	if isArray {
		var list *MShellList
		if !exists {
			list = NewList(0)
			table.Items[last] = list
			p.tableArrays[list] = struct{}{}
		} else {
			l, isList := existing.(*MShellList)
			if !isList {
				p.fail(fmt.Sprintf("'%s' is not an array of tables", last))
			}
			if _, ok := p.tableArrays[l]; !ok {
				p.fail(fmt.Sprintf("cannot add a table to the array '%s'", last))
			}
			list = l
		}
		dict := NewDict()
		list.Items = append(list.Items, dict)
		return dict
	}

	if !exists {
		dict := NewDict()
		table.Items[last] = dict
		return dict
	}
	dict, isDict := existing.(*MShellDict)
	if !isDict || p.kinds[dict] != tomlTableImplicit {
		p.fail(fmt.Sprintf("'%s' is already defined", last))
	}
	delete(p.kinds, dict)
	return dict
}

// headerParent gives the table under key that a header continues into. For
// an array of tables, that is its last table.
func (p *tomlParser) headerParent(table *MShellDict, key string) *MShellDict {
	existing, exists := table.Items[key]
	if !exists {
		dict := NewDict()
		p.kinds[dict] = tomlTableImplicit
		table.Items[key] = dict
		return dict
	}
	switch v := existing.(type) {
	case *MShellDict:
		if p.kinds[v] == tomlTableInline {
			p.fail(fmt.Sprintf("cannot add to the inline table '%s'", key))
		}
		return v
	case *MShellList:
		if _, ok := p.tableArrays[v]; !ok {
			p.fail(fmt.Sprintf("cannot add to the array '%s'", key))
		}
		return v.Items[len(v.Items)-1].(*MShellDict)
	}
	p.fail(fmt.Sprintf("'%s' is not a table", key))
	return nil
}

func (p *tomlParser) keyValue(table *MShellDict) {
	keys := p.keyPath()
	p.expect('=')
	p.skipWs()
	for _, key := range keys[:len(keys)-1] {
		existing, exists := table.Items[key]
		if !exists {
			dict := NewDict()
			p.kinds[dict] = tomlTableDotted
			table.Items[key] = dict
			table = dict
			continue
		}
		dict, isDict := existing.(*MShellDict)
		if !isDict || p.kinds[dict] != tomlTableDotted {
			p.fail(fmt.Sprintf("cannot add to '%s' with a dotted key", key))
		}
		table = dict
	}
	// Read before value(), which reuses the key slice.
	last := keys[len(keys)-1]
	if _, exists := table.Items[last]; exists {
		p.fail(fmt.Sprintf("'%s' is already defined", last))
	}
	table.Items[last] = p.value()
}

// keyPath reads a key, dotted or not, and the whitespace around it. The slice
// is reused by the next call.
func (p *tomlParser) keyPath() []string {
	p.keys = p.keys[:0]
	for {
		p.skipWs()
		p.keys = append(p.keys, p.simpleKey())
		p.skipWs()
		if p.peek() != '.' {
			return p.keys
		}
		p.i++
	}
}

func tomlIsBareKeyChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

func (p *tomlParser) simpleKey() string {
	switch p.peek() {
	case '"':
		return p.basicString()
	case '\'':
		return p.literalString()
	}
	start := p.i
	for p.i < len(p.s) && tomlIsBareKeyChar(p.s[p.i]) {
		p.i++
	}
	if p.i == start {
		p.fail("expected a key")
	}
	return strings.Clone(p.s[start:p.i])
}

func (p *tomlParser) value() MShellObject {
	rest := p.s[p.i:]
	switch p.peek() {
	case '"':
		if strings.HasPrefix(rest, `"""`) {
			return MShellString{p.multilineBasicString()}
		}
		return MShellString{p.basicString()}
	case '\'':
		if strings.HasPrefix(rest, "'''") {
			return MShellString{p.multilineLiteralString()}
		}
		return MShellString{p.literalString()}
	case 't':
		if strings.HasPrefix(rest, "true") {
			p.i += 4
			return MShellBool{true}
		}
	case 'f':
		if strings.HasPrefix(rest, "false") {
			p.i += 5
			return MShellBool{false}
		}
	case '[':
		return p.array()
	case '{':
		return p.inlineTable()
	case 0:
		p.fail("expected a value")
	}
	return p.numberOrDate()
}

func (p *tomlParser) array() MShellObject {
	p.i++
	start := len(p.values)
	for {
		p.skipBlank()
		if p.peek() == ']' {
			break
		}
		v := p.value()
		p.values = append(p.values, v)
		p.skipBlank()
		if p.peek() == ',' {
			p.i++
			continue
		}
		if p.peek() != ']' {
			p.fail("expected ',' or ']'")
		}
		break
	}
	p.i++
	list := NewList(len(p.values) - start)
	copy(list.Items, p.values[start:])
	clear(p.values[start:])
	p.values = p.values[:start]
	return list
}

func (p *tomlParser) inlineTable() MShellObject {
	p.i++
	dict := NewDict()
	p.kinds[dict] = tomlTableInline
	p.skipWs()
	if p.peek() == '}' {
		p.i++
		return dict
	}
	for {
		p.keyValue(dict)
		p.skipWs()
		switch p.peek() {
		case ',':
			p.i++
		case '}':
			p.i++
			return dict
		default:
			p.fail("expected ',' or '}'")
		}
	}
}

// Strings {{{

// tomlIsTextChar reports whether c may appear in a comment or string. Tab is
// the only control character allowed. Bytes of multi-byte characters are
// allowed; the input was checked to be UTF-8.
func tomlIsTextChar(c byte) bool {
	return c >= 0x20 && c != 0x7f || c == '\t'
}

func (p *tomlParser) basicString() string {
	p.i++
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '"' {
			s := p.s[start:p.i]
			p.i++
			return strings.Clone(s)
		}
		if c == '\\' {
			break
		}
		if !tomlIsTextChar(c) {
			p.fail("control character in string")
		}
		p.i++
	}
	// The string has escapes, so build it up.
	var sb strings.Builder
	sb.WriteString(p.s[start:p.i])
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == '"':
			p.i++
			return sb.String()
		case c == '\\':
			p.escape(&sb)
		case !tomlIsTextChar(c):
			p.fail("control character in string")
		default:
			sb.WriteByte(c)
			p.i++
		}
	}
	p.fail("unterminated string")
	return ""
}

func (p *tomlParser) escape(sb *strings.Builder) {
	p.i++
	c := p.peek()
	p.i++
	switch c {
	case 'b':
		sb.WriteByte('\b')
	case 't':
		sb.WriteByte('\t')
	case 'n':
		sb.WriteByte('\n')
	case 'f':
		sb.WriteByte('\f')
	case 'r':
		sb.WriteByte('\r')
	case '"':
		sb.WriteByte('"')
	case '\\':
		sb.WriteByte('\\')
	case 'u', 'U':
		n := 4
		if c == 'U' {
			n = 8
		}
		if p.i+n > len(p.s) {
			p.fail("unicode escape is too short")
		}
		hex := p.s[p.i : p.i+n]
		v, err := strconv.ParseUint(hex, 16, 32)
		if err != nil || !utf8.ValidRune(rune(v)) {
			p.fail("invalid unicode escape")
		}
		p.i += n
		sb.WriteRune(rune(v))
	default:
		p.i--
		p.fail("invalid escape")
	}
}

// closingQuotes is called at a quote inside a multi-line string. If the
// quotes there close the string, it moves past them and gives the index where
// the string's text ends; up to two quotes before the closing three belong to
// the string. Otherwise it moves past the quotes and gives -1.
func (p *tomlParser) closingQuotes(q byte) int {
	n := 0
	for p.i+n < len(p.s) && p.s[p.i+n] == q {
		n++
	}
	if n > 5 {
		p.fail("too many quotes")
	}
	p.i += n
	if n < 3 {
		return -1
	}
	return p.i - 3
}

// A newline right after the opening delimiter of a multi-line string is not
// part of the string.
func (p *tomlParser) multilineStart() {
	p.i += 3
	p.newline()
}

func (p *tomlParser) multilineBasicString() string {
	p.multilineStart()
	var sb strings.Builder
	escaped := false
	start, segment := p.i, p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == '"':
			if end := p.closingQuotes('"'); end >= 0 {
				if !escaped {
					return strings.Clone(p.s[start:end])
				}
				sb.WriteString(p.s[segment:end])
				return sb.String()
			}
		case c == '\\':
			escaped = true
			sb.WriteString(p.s[segment:p.i])
			j := p.i + 1
			for j < len(p.s) && (p.s[j] == ' ' || p.s[j] == '\t') {
				j++
			}
			if j < len(p.s) && (p.s[j] == '\n' || p.s[j] == '\r') {
				// A backslash at the end of a line removes the newline and
				// all whitespace and newlines after it.
				p.i = j
				for {
					p.skipWs()
					if !p.newline() {
						break
					}
				}
			} else {
				p.escape(&sb)
			}
			segment = p.i
		case c == '\n' || c == '\r':
			p.newline()
		case !tomlIsTextChar(c):
			p.fail("control character in string")
		default:
			p.i++
		}
	}
	p.fail("unterminated string")
	return ""
}

func (p *tomlParser) literalString() string {
	p.i++
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c == '\'' {
			s := p.s[start:p.i]
			p.i++
			return strings.Clone(s)
		}
		if !tomlIsTextChar(c) {
			p.fail("control character in string")
		}
		p.i++
	}
	p.fail("unterminated string")
	return ""
}

func (p *tomlParser) multilineLiteralString() string {
	p.multilineStart()
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case c == '\'':
			if end := p.closingQuotes('\''); end >= 0 {
				return strings.Clone(p.s[start:end])
			}
		case c == '\n' || c == '\r':
			p.newline()
		case !tomlIsTextChar(c):
			p.fail("control character in string")
		default:
			p.i++
		}
	}
	p.fail("unterminated string")
	return ""
}

// }}}

// Numbers and datetimes {{{

func tomlIsValueChar(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		c == '_' || c == '+' || c == '-' || c == '.' || c == ':'
}

func tomlIsDigit(c byte) bool { return c >= '0' && c <= '9' }
func tomlIsHex(c byte) bool   { return tomlIsDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }
func tomlIsOctal(c byte) bool { return c >= '0' && c <= '7' }
func tomlIsBinary(c byte) bool {
	return c == '0' || c == '1'
}

func (p *tomlParser) numberOrDate() MShellObject {
	start := p.i
	for p.i < len(p.s) && tomlIsValueChar(p.s[p.i]) {
		p.i++
	}
	tok := p.s[start:p.i]
	if len(tok) >= 3 && tok[2] == ':' && tomlIsDigit(tok[0]) {
		h, m, s, ns, rest := p.clock(tok)
		if rest != "" {
			p.fail("invalid time")
		}
		return MShellString{time.Date(0, 1, 1, h, m, s, ns, time.UTC).Format("15:04:05.999999999")}
	}
	if len(tok) >= 5 && tok[4] == '-' && tomlIsDigit(tok[0]) {
		// A space may separate the date and the time.
		if len(tok) == 10 && p.i+2 < len(p.s) && p.s[p.i] == ' ' && tomlIsDigit(p.s[p.i+1]) && tomlIsDigit(p.s[p.i+2]) {
			p.i++
			for p.i < len(p.s) && tomlIsValueChar(p.s[p.i]) {
				p.i++
			}
			tok = p.s[start:p.i]
		}
		return p.dateTime(tok)
	}
	return p.number(tok)
}

func (p *tomlParser) digits(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if !tomlIsDigit(s[i]) {
			p.fail("expected a digit")
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}

func tomlDaysInMonth(month, year int) int {
	switch month {
	case 2:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	}
	return 31
}

// clock reads HH:MM:SS with an optional fraction and gives the text after it.
// Digits past nanoseconds are dropped.
func (p *tomlParser) clock(s string) (h, m, sec, ns int, rest string) {
	if len(s) < 8 || s[2] != ':' || s[5] != ':' {
		p.fail("invalid time")
	}
	h, m, sec = p.digits(s[0:2]), p.digits(s[3:5]), p.digits(s[6:8])
	if h > 23 || m > 59 || sec > 59 {
		p.fail("time out of range")
	}
	i := 8
	if i < len(s) && s[i] == '.' {
		i++
		j := i
		scale := 100000000
		for j < len(s) && tomlIsDigit(s[j]) {
			ns += int(s[j]-'0') * scale
			scale /= 10
			j++
		}
		if j == i {
			p.fail("expected digits after '.'")
		}
		i = j
	}
	return h, m, sec, ns, s[i:]
}

func (p *tomlParser) dateTime(tok string) MShellObject {
	if len(tok) < 10 || tok[7] != '-' {
		p.fail("invalid date")
	}
	year, month, day := p.digits(tok[0:4]), p.digits(tok[5:7]), p.digits(tok[8:10])
	if month < 1 || month > 12 || day < 1 || day > tomlDaysInMonth(month, year) {
		p.fail("date out of range")
	}
	if len(tok) == 10 {
		return &MShellDateTime{Time: time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)}
	}
	if c := tok[10]; c != 'T' && c != 't' && c != ' ' {
		p.fail("invalid datetime")
	}
	h, m, s, ns, rest := p.clock(tok[11:])
	t := time.Date(year, time.Month(month), day, h, m, s, ns, time.UTC)
	switch {
	case rest == "" || rest == "Z" || rest == "z":
	case len(rest) == 6 && (rest[0] == '+' || rest[0] == '-') && rest[3] == ':':
		offsetH, offsetM := p.digits(rest[1:3]), p.digits(rest[4:6])
		if offsetH > 23 || offsetM > 59 {
			p.fail("offset out of range")
		}
		offset := time.Duration(offsetH*60+offsetM) * time.Minute
		if rest[0] == '+' {
			offset = -offset
		}
		t = t.Add(offset)
	default:
		p.fail("invalid offset")
	}
	return &MShellDateTime{Time: t}
}

// digitRun reads digits from s at i, with single underscores allowed between
// digits, and gives the index after them.
func (p *tomlParser) digitRun(s string, i int, isDigit func(byte) bool) int {
	start := i
	for i < len(s) {
		if isDigit(s[i]) {
			i++
		} else if s[i] == '_' && i > start && i+1 < len(s) && isDigit(s[i+1]) {
			i++
		} else {
			break
		}
	}
	if i == start {
		p.fail("expected digits")
	}
	return i
}

func tomlRemoveUnderscores(s string) string {
	if strings.IndexByte(s, '_') < 0 {
		return s
	}
	return strings.ReplaceAll(s, "_", "")
}

func (p *tomlParser) number(tok string) MShellObject {
	switch tok {
	case "inf", "+inf":
		return MShellFloat{math.Inf(1)}
	case "-inf":
		return MShellFloat{math.Inf(-1)}
	case "nan", "+nan", "-nan":
		return MShellFloat{math.NaN()}
	}

	if len(tok) > 2 && tok[0] == '0' && (tok[1] == 'x' || tok[1] == 'o' || tok[1] == 'b') {
		base, isDigit := 16, tomlIsHex
		if tok[1] == 'o' {
			base, isDigit = 8, tomlIsOctal
		} else if tok[1] == 'b' {
			base, isDigit = 2, tomlIsBinary
		}
		if p.digitRun(tok, 2, isDigit) != len(tok) {
			p.fail("invalid integer")
		}
		v, err := strconv.ParseInt(tomlRemoveUnderscores(tok[2:]), base, 64)
		if err != nil {
			p.fail("integer out of range")
		}
		return MShellInt{int(v)}
	}

	i := 0
	if tok != "" && (tok[0] == '+' || tok[0] == '-') {
		i++
	}
	intStart := i
	i = p.digitRun(tok, i, tomlIsDigit)
	if tok[intStart] == '0' && i-intStart > 1 {
		p.fail("leading zero")
	}
	isFloat := false
	if i < len(tok) && tok[i] == '.' {
		isFloat = true
		i = p.digitRun(tok, i+1, tomlIsDigit)
	}
	if i < len(tok) && (tok[i] == 'e' || tok[i] == 'E') {
		isFloat = true
		i++
		if i < len(tok) && (tok[i] == '+' || tok[i] == '-') {
			i++
		}
		i = p.digitRun(tok, i, tomlIsDigit)
	}
	if i != len(tok) {
		p.fail("invalid number")
	}

	if isFloat {
		v, err := strconv.ParseFloat(tomlRemoveUnderscores(tok), 64)
		if err != nil {
			p.fail("float out of range")
		}
		return MShellFloat{v}
	}
	v, err := strconv.ParseInt(tomlRemoveUnderscores(tok), 10, 64)
	if err != nil {
		p.fail("integer out of range")
	}
	return MShellInt{int(v)}
}

// }}}
