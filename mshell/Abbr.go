package main

// Abbreviations (ai/abbr.typ). The init file adds them with abbrAdd, and
// the interactive shell expands a word into one as it is typed. Their
// types are declared in builtinDeclSource.

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// abbrTimeout is how long an abbreviation's quote may run. The user is
// waiting for a key to appear, so it is shorter than promptTimeout.
const abbrTimeout = 1 * time.Second

// isAbbrSeparator reports whether r ends the word an abbreviation is
// tried on: white space as the lexer defines it, and some of the
// characters that end a literal.
func isAbbrSeparator(r rune) bool {
	switch r {
	case '[', ']', '(', ')', '{', '}', '|', ';', '<', '>':
		return true
	}
	return unicode.IsSpace(r)
}

// isAbbrTrigger reports whether typing r tries an abbreviation on the word
// before the cursor, besides Space and Enter.
func isAbbrTrigger(r rune) bool {
	switch r {
	case '|', ';', '<', '>', ']', ')':
		return true
	}
	return false
}

type abbrPositionKind uint8

const (
	abbrPosCommand abbrPositionKind = iota
	abbrPosAnywhere
	abbrPosArgOf
)

// abbrEntry is one abbreviation abbrAdd added.
type abbrEntry struct {
	// record is the Abbr abbrAdd was given, with its position filled in,
	// which abbrList gives back.
	record *MShellDict

	// The match: word is the abbrWord or abbrRegex text, and re the
	// compiled regex, when it compiles.
	isRegex bool
	word    string
	re      *regexp.Regexp

	pos   abbrPositionKind
	argOf []string

	// The expansion: fn is the abbrFunc quote, or text the abbrText.
	text string
	fn   *MShellQuotation

	// marker is the cursor marker, or "" for none.
	marker string

	// inert says why the entry can never expand, or is "".
	inert string
}

// sameMatch reports whether two entries have equal abbr values.
func (e *abbrEntry) sameMatch(isRegex bool, word string) bool {
	return e.isRegex == isRegex && e.word == word
}

// samePosition reports whether two entries have equal positions.
func (e *abbrEntry) samePosition(o *abbrEntry) bool {
	return e.pos == o.pos && (e.pos != abbrPosArgOf || slices.Equal(e.argOf, o.argOf))
}

// describe names the entry's abbr in a message.
func (e *abbrEntry) describe() string {
	if e.isRegex {
		return "regex '" + e.word + "'"
	}
	return "'" + e.word + "'"
}

// matches reports whether the entry's abbr matches word.
func (e *abbrEntry) matches(word string) bool {
	if e.isRegex {
		return e.re.MatchString(word)
	}
	return e.word == word
}

// appliesAt reports whether the entry's position allows the word at site.
func (e *abbrEntry) appliesAt(site abbrSite) bool {
	switch e.pos {
	case abbrPosCommand:
		return site.CommandPosition
	case abbrPosAnywhere:
		return true
	default:
		return site.HasCommand && slices.Contains(e.argOf, site.Command)
	}
}

// builtinEnumValue makes a value of a built-in enum's member.
func (state *EvalState) builtinEnumValue(member string, payload ...MShellObject) *MShellEnum {
	state.ensureBuiltinEnums()
	info := state.EnumMembers[member]
	return &MShellEnum{EnumName: info.EnumName, Member: member, MemberIndex: info.Ordinal, Payload: payload}
}

// enumField is the value of an Abbr field that holds a member of the enum
// named enumName, or nil when it is missing or not one.
func enumField(dict *MShellDict, key string, enumName string) *MShellEnum {
	e, ok := dict.Items[key].(*MShellEnum)
	if !ok || e.EnumName != enumName {
		return nil
	}
	return e
}

// parseAbbr reads an Abbr record into an entry. It fails only on a value
// the type checker would refuse; a value that can never expand gives an
// inert entry.
func (state *EvalState) parseAbbr(record *MShellDict) (*abbrEntry, error) {
	e := &abbrEntry{record: record}

	match := enumField(record, "abbr", "AbbrMatch")
	if match == nil {
		return nil, fmt.Errorf("the 'abbr' field must be an AbbrMatch")
	}
	word, err := payloadString(match, 0)
	if err != nil {
		return nil, fmt.Errorf("the 'abbr' field must hold a str")
	}
	e.word = word
	e.isRegex = match.Member == "abbrRegex"

	expansion := enumField(record, "expansion", "AbbrExpansion")
	if expansion == nil || len(expansion.Payload) != 1 {
		return nil, fmt.Errorf("the 'expansion' field must be an AbbrExpansion")
	}
	switch v := expansion.Payload[0].(type) {
	case MShellString:
		e.text = v.Content
	case *MShellQuotation:
		e.fn = v
	default:
		return nil, fmt.Errorf("the 'expansion' field holds a %s", v.TypeName())
	}

	if _, given := record.Items["position"]; !given {
		record.Items["position"] = state.builtinEnumValue("abbrCommand")
	}
	position := enumField(record, "position", "AbbrPosition")
	if position == nil {
		return nil, fmt.Errorf("the 'position' field must be an AbbrPosition")
	}
	switch position.Member {
	case "abbrCommand":
		e.pos = abbrPosCommand
	case "abbrAnywhere":
		e.pos = abbrPosAnywhere
	default:
		e.pos = abbrPosArgOf
		list, ok := position.Payload[0].(*MShellList)
		if !ok {
			return nil, fmt.Errorf("'abbrArgOf' must hold a list of str")
		}
		for _, item := range list.Items {
			s, ok := item.(MShellString)
			if !ok {
				return nil, fmt.Errorf("'abbrArgOf' must hold a list of str")
			}
			e.argOf = append(e.argOf, s.Content)
		}
	}

	for _, key := range []string{"cursorMarker", "description"} {
		if v, given := record.Items[key]; given {
			s, ok := v.(MShellString)
			if !ok {
				return nil, fmt.Errorf("the '%s' field must be a str", key)
			}
			if key == "cursorMarker" {
				e.marker = s.Content
			}
		}
	}

	e.inert = e.inertReason()
	return e, nil
}

// inertReason says why an entry can never expand, or is "".
func (e *abbrEntry) inertReason() string {
	if e.isRegex {
		if e.word == "" {
			return "the regex is empty"
		}
		re, err := regexp.Compile("^(?:" + e.word + ")$")
		if err != nil {
			return "the regex does not compile: " + err.Error()
		}
		e.re = re
	} else {
		switch {
		case e.word == "":
			return "the word is empty"
		case strings.IndexFunc(e.word, isAbbrSeparator) >= 0:
			return "the word has white space or a separator"
		case strings.ContainsRune("#'\"`", rune(e.word[0])) || strings.HasPrefix(e.word, `$"`):
			return "the word starts a comment, string or path"
		}
	}
	if e.pos == abbrPosArgOf {
		if len(e.argOf) == 0 {
			return "'abbrArgOf' has no commands"
		}
		for _, c := range e.argOf {
			if c == "" || strings.IndexFunc(c, unicode.IsSpace) >= 0 {
				return "'abbrArgOf' has a command that is empty or has white space"
			}
		}
	}
	return ""
}

// addAbbr adds an entry as the newest, replacing one with an equal abbr
// and an equal position. An inert entry is noted for the interactive shell
// to print.
func (state *EvalState) addAbbr(e *abbrEntry) {
	state.Abbrs = slices.DeleteFunc(state.Abbrs, func(o *abbrEntry) bool {
		return o.sameMatch(e.isRegex, e.word) && o.samePosition(e)
	})
	state.Abbrs = append(state.Abbrs, e)
	if e.inert != "" {
		state.AbbrWarnings = append(state.AbbrWarnings, fmt.Sprintf("Abbreviation %s never expands: %s.", e.describe(), e.inert))
	}
}

// popAbbrMatch pops an AbbrMatch.
func (state *EvalState) popAbbrMatch(t *Token, stack *MShellStack, name string) (bool, string, *EvalResult) {
	obj, err := stack.Pop()
	if err != nil {
		return false, "", state.mismatchPtr(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, name))
	}
	e, ok := obj.(*MShellEnum)
	if ok && e.EnumName == "AbbrMatch" {
		if word, err := payloadString(e, 0); err == nil {
			return e.Member == "abbrRegex", word, nil
		}
	}
	return false, "", state.mismatchPtr(fmt.Sprintf("%d:%d: '%s' takes an AbbrMatch, found a %s (%s).\n", t.Line, t.Column, name, obj.TypeName(), obj.DebugString()))
}

// evalAbbrAdd runs abbrAdd: (Abbr -- ).
func (state *EvalState) evalAbbrAdd(t *Token, stack *MShellStack) *EvalResult {
	obj, err := stack.Pop()
	if err != nil {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: Cannot do 'abbrAdd' operation on an empty stack.\n", t.Line, t.Column))
	}
	dict, ok := obj.(*MShellDict)
	if !ok {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: 'abbrAdd' takes an Abbr, found a %s (%s).\n", t.Line, t.Column, obj.TypeName(), obj.DebugString()))
	}
	copied, err := DeepCopy(dict)
	if err != nil {
		return state.checkedPtr(fmt.Sprintf("%d:%d: 'abbrAdd': %s\n", t.Line, t.Column, err.Error()))
	}
	e, err := state.parseAbbr(copied.(*MShellDict))
	if err != nil {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: 'abbrAdd' takes an Abbr: %s.\n", t.Line, t.Column, err.Error()))
	}
	state.addAbbr(e)
	return nil
}

// evalAbbrErase runs abbrErase: (AbbrMatch -- ). It removes the
// abbreviations with that abbr in every position.
func (state *EvalState) evalAbbrErase(t *Token, stack *MShellStack) *EvalResult {
	isRegex, word, r := state.popAbbrMatch(t, stack, "abbrErase")
	if r != nil {
		return r
	}
	state.Abbrs = slices.DeleteFunc(state.Abbrs, func(e *abbrEntry) bool { return e.sameMatch(isRegex, word) })
	return nil
}

// evalAbbrRename runs abbrRename: (AbbrMatch:old AbbrMatch:new -- ). Each
// abbreviation with the old abbr is erased and added again with the new
// one, in order, so it replaces any with the new abbr and an equal
// position.
func (state *EvalState) evalAbbrRename(t *Token, stack *MShellStack) *EvalResult {
	newObj, err := stack.Peek()
	if err != nil {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: Cannot do 'abbrRename' operation on an empty stack.\n", t.Line, t.Column))
	}
	_, _, r := state.popAbbrMatch(t, stack, "abbrRename")
	if r != nil {
		return r
	}
	oldIsRegex, oldWord, r := state.popAbbrMatch(t, stack, "abbrRename")
	if r != nil {
		return r
	}
	var renamed []*abbrEntry
	state.Abbrs = slices.DeleteFunc(state.Abbrs, func(e *abbrEntry) bool {
		if e.sameMatch(oldIsRegex, oldWord) {
			renamed = append(renamed, e)
			return true
		}
		return false
	})
	for _, old := range renamed {
		copied, err := DeepCopy(newObj)
		if err != nil {
			return state.checkedPtr(fmt.Sprintf("%d:%d: 'abbrRename': %s\n", t.Line, t.Column, err.Error()))
		}
		old.record.Items["abbr"] = copied
		e, err := state.parseAbbr(old.record)
		if err != nil {
			return state.mismatchPtr(fmt.Sprintf("%d:%d: 'abbrRename': %s.\n", t.Line, t.Column, err.Error()))
		}
		state.addAbbr(e)
	}
	return nil
}

// evalAbbrList runs abbrList: ( -- new [Abbr]), oldest first.
func (state *EvalState) evalAbbrList(t *Token, stack *MShellStack) *EvalResult {
	list := NewList(len(state.Abbrs))
	for i, e := range state.Abbrs {
		copied, err := DeepCopy(e.record)
		if err != nil {
			return state.checkedPtr(fmt.Sprintf("%d:%d: 'abbrList': %s\n", t.Line, t.Column, err.Error()))
		}
		list.Items[i] = copied
	}
	stack.Push(list)
	return nil
}

// abbrMayMatch reports whether an abbreviation that can expand matches
// word, wherever it is.
func (state *EvalState) abbrMayMatch(word string) bool {
	for _, e := range state.Abbrs {
		if e.inert == "" && e.matches(word) {
			return true
		}
	}
	return false
}

// commandAbbrWords are the words of the abbreviations that expand in
// command position, for completion.
func (state *EvalState) commandAbbrWords() []string {
	var words []string
	for _, e := range state.Abbrs {
		if !e.isRegex && e.inert == "" && e.pos != abbrPosArgOf && !slices.Contains(words, e.word) {
			words = append(words, e.word)
		}
	}
	return words
}

// abbrSite is the word an abbreviation is tried on, and its position.
type abbrSite struct {
	Start, End ByteOffset
	// CommandPosition is set for a command word: at the start of the
	// line, at the head of a list, or after '|' on a command line.
	CommandPosition bool
	// Command is the command the word is an argument of, when HasCommand.
	Command    string
	HasCommand bool
}

// isAbbrTextToken reports whether a token is a string, rune, path or
// comment, where a word is data and never expands.
func isAbbrTextToken(t TokenType) bool {
	switch t {
	case STRING, SINGLEQUOTESTRING, FORMATSTRING, FORMATSTRINGSTART, FORMATSTRINGMID, FORMATSTRINGEND,
		UNFINISHEDSTRING, UNFINISHEDFORMATSTRING, UNFINISHEDSINGLEQUOTESTRING, PATH, UNFINISHEDPATH, LINECOMMENT,
		RUNE, UNFINISHEDRUNE:
		return true
	}
	return false
}

// abbrWordAt finds the word to try an abbreviation on in line, with the
// cursor at cursor: the text from the nearest separator before the cursor
// to the cursor, or with toWordEnd (for Enter), to the next separator. It
// returns false when there is no word, or when the word does not start and
// end on grapheme boundaries: replacing it would take the rest of a
// grapheme with it.
func abbrWordAt(line SourceText, cursor ByteOffset, toWordEnd bool) (start, end ByteOffset, ok bool) {
	start, end = cursor, cursor
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(string(line[:start]))
		if isAbbrSeparator(r) {
			break
		}
		start -= ByteOffset(size)
	}
	for toWordEnd && int(end) < len(line) {
		r, size := utf8.DecodeRuneInString(string(line[end:]))
		if isAbbrSeparator(r) {
			break
		}
		end += ByteOffset(size)
	}
	if start == end || graphemeFloor(line, start) != start || graphemeCeil(line, end) != end {
		return 0, 0, false
	}
	return start, end, true
}

// findAbbrSite finds the position of the word [start, end) of line, from
// the line's tokens. The word's token is the line's token that starts at
// the word, as it is painted, even when it runs on past the word. isProgram
// says whether a word names a program, by the rule the shell uses when it
// runs a line. It returns false when the word may not expand: it is not at
// the start of a token, or is in a string, path or comment, or the line
// does not lex up to the word's end.
func findAbbrSite(lt *lineTokens, line SourceText, start, end ByteOffset, isProgram func(string) bool) (abbrSite, bool) {
	tokens := lt.get(line)
	if lt.errAt >= 0 && lt.errAt < end {
		return abbrSite{}, false
	}

	// The tokens before the word, without white space or comments, are
	// tracked by index: the first, the last, the last '|' outside brackets,
	// and the open brackets.
	open := lt.brackets[:0]
	first, last, lastPipe := -1, -1, -1
	found := false
	// The tokens tile the line, so their byte offsets add up.
	var offset ByteOffset
	for i, t := range tokens {
		if t.Type == EOF {
			break
		}
		tokenStart := offset
		offset += ByteOffset(len(t.Lexeme))
		if tokenStart >= start {
			// The word must begin a token that is not a string, path or
			// comment.
			found = tokenStart == start && !isAbbrTextToken(t.Type)
			break
		}
		if offset > start {
			// A token that starts before the word runs into it: the word is
			// inside a string, path or comment.
			break
		}
		switch t.Type {
		case WHITESPACE, LINECOMMENT:
			continue
		case LEFT_SQUARE_BRACKET, LEFT_PAREN, LEFT_CURLY, GRID_OPEN:
			open = append(open, int32(i))
		case RIGHT_SQUARE_BRACKET, RIGHT_PAREN, RIGHT_CURLY, GRID_CLOSE:
			if len(open) > 0 {
				open = open[:len(open)-1]
			}
		case PIPE:
			if len(open) == 0 {
				lastPipe = i
			}
		}
		if first < 0 {
			first = i
		}
		last = i
	}
	lt.brackets = open
	if !found {
		return abbrSite{}, false
	}

	site := abbrSite{Start: start, End: end}
	if first < 0 {
		site.CommandPosition = true
		return site, true
	}
	commandLine := tokens[first].Type == LITERAL && isProgram(tokens[first].Lexeme)
	prev := tokens[last].Type
	if prev == LEFT_SQUARE_BRACKET || (prev == PIPE && commandLine) {
		site.CommandPosition = true
		return site, true
	}

	// The command the word is an argument of: the first token after the
	// innermost open '[', or after the last '|' (or the first token) on a
	// command line.
	head := -1
	switch {
	case len(open) > 0:
		if i := open[len(open)-1]; tokens[i].Type == LEFT_SQUARE_BRACKET {
			head = nextAbbrToken(tokens, int(i), last)
		}
	case commandLine && prev != LESSTHAN && prev != GREATERTHAN:
		// A word after a redirect is a file, not an argument.
		head = first
		if lastPipe >= 0 {
			head = nextAbbrToken(tokens, lastPipe, last)
		}
	}
	if head >= 0 && tokens[head].Type == LITERAL {
		site.Command, site.HasCommand = tokens[head].Lexeme, true
	}
	return site, true
}

// nextAbbrToken is the index of the first token after i, up to last, that
// is not white space or a comment, or -1.
func nextAbbrToken(tokens []Token, i int, last int) int {
	for j := i + 1; j <= last; j++ {
		if t := tokens[j].Type; t != WHITESPACE && t != LINECOMMENT {
			return j
		}
	}
	return -1
}

// isProgramWord reports whether word names a program, by the rule the
// shell uses to decide that a line is a command line.
func (state *TermState) isProgramWord(word string) bool {
	if state.context.Pbm == nil || IsDefinitionDefined(word, state.stdLibDefs) {
		return false
	}
	if _, ok := state.context.Pbm.Lookup(word); ok {
		return true
	}
	if _, ok := knownCommands[word]; ok {
		return true
	}
	return strings.ContainsRune(word, os.PathSeparator) && state.context.Pbm.IsExecutableFile(word)
}

// abbrExpansion is where an expansion left the line.
type abbrExpansion struct {
	// End is the end of the expansion's text.
	End ByteOffset
	// Cursor is where the cursor marker was, when HasCursor.
	Cursor    ByteOffset
	HasCursor bool
}

// expandAbbr tries the abbreviations on the word at the cursor, newest
// first, and replaces the word with the first expansion. The cursor is
// left at the end of the expansion's text. It returns false when nothing
// was expanded.
func (state *TermState) expandAbbr(toWordEnd bool) (abbrExpansion, bool) {
	abbrs := state.evalState.Abbrs
	if len(abbrs) == 0 {
		return abbrExpansion{}, false
	}
	line := state.currentCommand
	start, end, ok := abbrWordAt(line, state.index, toWordEnd)
	if !ok {
		return abbrExpansion{}, false
	}
	// Most words match nothing; they need no tokens.
	word := string(line[start:end])
	if !state.evalState.abbrMayMatch(word) {
		return abbrExpansion{}, false
	}
	site, ok := findAbbrSite(&state.lineTokens, line, start, end, state.isProgramWord)
	if !ok {
		return abbrExpansion{}, false
	}
	for i := len(abbrs) - 1; i >= 0; i-- {
		e := abbrs[i]
		if e.inert != "" || !e.matches(word) || !e.appliesAt(site) {
			continue
		}
		text := e.text
		if e.fn != nil {
			var stop bool
			text, ok, stop = state.runAbbrFunc(e, word, site)
			if stop {
				return abbrExpansion{}, false
			}
			if !ok {
				continue
			}
		}
		marker := -1
		if e.marker != "" {
			if marker = strings.Index(text, e.marker); marker >= 0 {
				text = text[:marker] + text[marker+len(e.marker):]
			}
		}
		state.replaceText(text, site.Start, site.End)
		exp := abbrExpansion{End: state.index}
		if marker >= 0 {
			exp.Cursor = graphemeCeil(state.currentCommand, site.Start+ByteOffset(marker))
			exp.HasCursor = true
		}
		return exp, true
	}
	return abbrExpansion{}, false
}

// typeAbbrTrigger handles Space or a separator typed in the command line:
// it tries an abbreviation on the word before the cursor, then inserts the
// key. A Space is dropped when the expansion put the cursor at its marker.
func (state *TermState) typeAbbrTrigger(key rune) {
	exp, ok := state.expandAbbr(false)
	if !ok {
		state.PushChars([]rune{key})
		return
	}
	if exp.HasCursor {
		if key != ' ' {
			state.replaceText(string(key), exp.End, exp.End)
		}
		state.index = exp.Cursor
		return
	}
	state.PushChars([]rune{key})
}

// expandAbbrOnEnter tries an abbreviation on the word that contains the
// cursor or ends at it, before the line runs.
func (state *TermState) expandAbbrOnEnter() {
	if exp, ok := state.expandAbbr(true); ok && exp.HasCursor {
		state.index = exp.Cursor
	}
}

// runAbbrFunc runs an abbreviation's quote on the word. It returns the
// text to replace the word with, or false when the quote declined. stop
// is set when the quote failed: the word is left as it is, and no other
// abbreviation is tried. The quote's output is not shown.
func (state *TermState) runAbbrFunc(e *abbrEntry, word string, site abbrSite) (string, bool, bool) {
	// The quote may keep what it is given, so it gets copies, not the
	// editor's arena.
	info := NewDict()
	info.Items["word"] = MShellString{strings.Clone(word)}
	command := &Maybe{}
	if site.HasCommand {
		command.obj = MShellString{strings.Clone(site.Command)}
	}
	info.Items["command"] = command
	info.Items["line"] = MShellString{strings.Clone(string(state.currentCommand))}
	stack := MShellStack{info}

	var out bytes.Buffer
	context := state.context
	context.StandardOutput = &out
	context.StandardError = &out
	// Only the time limit stops the quote. The terminal stays raw, so
	// Ctrl-C is a key, read after the quote ends.
	run := state.runUserQuote(e.fn, &stack, context, abbrTimeout, newEvalCancel())

	what := "The quote of abbreviation " + e.describe()
	problem := run.problem(what, "the shell", abbrTimeout)
	if problem == "" {
		if m, ok := singleMaybe(stack); ok {
			if m.IsNone() {
				state.abbrProblem = ""
				return "", false, false
			}
			if s, ok := m.obj.(MShellString); ok {
				state.abbrProblem = ""
				return s.Content, true, false
			}
		}
		problem = what + " must leave one Maybe[str] on the stack.\n"
	}
	// The same problem is shown once, not again while the quote keeps
	// failing the same way.
	if problem != state.abbrProblem {
		state.abbrMessage = problem
	}
	state.abbrProblem = problem
	return "", false, true
}

// abbrMessageLines are the rows of the message shown below the command
// line about an abbreviation's quote, when there are no completions to
// show.
func (state *TermState) abbrMessageLines() []string {
	if state.abbrMessage == "" {
		return nil
	}
	return strings.Split(terminalSafeText(strings.TrimRight(state.abbrMessage, "\n"), true), "\n")
}

// printAbbrWarnings prints, and forgets, the warnings abbrAdd noted about
// abbreviations that never expand. The terminal must be in its normal
// mode.
func (state *EvalState) printAbbrWarnings() {
	for _, w := range state.AbbrWarnings {
		fmt.Fprintln(os.Stderr, terminalSafeText(w, false))
	}
	state.AbbrWarnings = state.AbbrWarnings[:0]
}
