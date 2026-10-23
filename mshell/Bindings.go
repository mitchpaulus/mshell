package main

// Key bindings (ai/cli_bindings.typ). A binding maps a sequence of keys to a
// program for the editor: a list of EditOp values, or a quote that returns
// one. The defaults are bindings like any other, added before the init file
// runs, and the editor resolves every key against them (resolveKey). Their
// types are declared in builtinDeclSource.

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// editMode is a keymap: the bindings of modeInsert, or of modeComplete
// while Tab cycles through completions.
type editMode uint8

const (
	modeInsert editMode = iota
	modeComplete
	editModeCount
)

var editModeMembers = [editModeCount]string{"modeInsert", "modeComplete"}

// editOpKind is an EditOp member. The pure edits come first: they change
// only the line and the cursor, and can be undone by restoring them.
type editOpKind uint8

const (
	opInsert editOpKind = iota
	opSelfInsert
	opSetLine
	opLeft
	opRight
	opWordLeft
	opWordRight
	opLineStart
	opLineEnd
	opBackspace
	opDelete
	opDeleteWordLeft
	opDeleteToStart
	opDeleteToEnd
	opExpandAbbr
	// The effects.
	opAccept
	opExit
	opRun
	opRunOps
	opForward
	opAcceptSuggestion
	opHistoryPrev
	opHistoryNext
	opHistorySearchBack
	opHistorySearchForward
	opLastArg
	opComplete
	opCompleteBack
	opCompleteNext
	opCompletePrev
	opCompleteLeft
	opCompleteRight
	opCompleteAccept
	opClearScreen
	opRemeasure
	opMessage
	editOpCount
)

// editOpMembers are the EditOp members, by kind.
var editOpMembers = [editOpCount]string{
	"editInsert", "editSelfInsert", "editSetLine",
	"editLeft", "editRight", "editWordLeft", "editWordRight", "editLineStart", "editLineEnd",
	"editBackspace", "editDelete", "editDeleteWordLeft", "editDeleteToStart", "editDeleteToEnd",
	"editExpandAbbr",
	"editAccept", "editExit", "editRun", "editRunOps",
	"editForward", "editAcceptSuggestion",
	"editHistoryPrev", "editHistoryNext", "editHistorySearchBack", "editHistorySearchForward",
	"editLastArg", "editComplete", "editCompleteBack",
	"editCompleteNext", "editCompletePrev", "editCompleteLeft", "editCompleteRight", "editCompleteAccept",
	"editClearScreen", "editRemeasure", "editMessage",
}

var editOpKinds = sync.OnceValue(func() map[string]editOpKind {
	m := make(map[string]editOpKind, editOpCount)
	for k, name := range editOpMembers {
		m[name] = editOpKind(k)
	}
	return m
})

func (k editOpKind) pure() bool { return k <= opExpandAbbr }

// ends reports whether the op starts something the program cannot follow:
// a new line, or no shell.
func (k editOpKind) ends() bool { return k == opAccept || k == opExit }

// editOp is one EditOp: its kind and payload.
type editOp struct {
	kind  editOpKind
	a, b  string
	quote *MShellQuotation
}

// parseEditOp reads an EditOp value.
func parseEditOp(v MShellObject) (editOp, error) {
	e, ok := v.(*MShellEnum)
	if !ok || e.EnumName != "EditOp" {
		return editOp{}, fmt.Errorf("an action must be a list of EditOp, found a %s (%s)", v.TypeName(), v.DebugString())
	}
	kind, ok := editOpKinds()[e.Member]
	if !ok {
		return editOp{}, fmt.Errorf("unknown EditOp '%s'", e.Member)
	}
	op := editOp{kind: kind}
	switch kind {
	case opInsert, opMessage:
		s, err := payloadString(e, 0)
		if err != nil {
			return editOp{}, fmt.Errorf("'%s' must hold a str", e.Member)
		}
		op.a = s
	case opSetLine:
		a, errA := payloadString(e, 0)
		b, errB := payloadString(e, 1)
		if errA != nil || errB != nil {
			return editOp{}, fmt.Errorf("'editSetLine' must hold two str")
		}
		op.a, op.b = a, b
	case opRun, opRunOps:
		q, ok := e.Payload[0].(*MShellQuotation)
		if len(e.Payload) != 1 || !ok {
			return editOp{}, fmt.Errorf("'%s' must hold a quotation", e.Member)
		}
		op.quote = q
	}
	return op, nil
}

// parseEditOps reads a list of EditOp values.
func parseEditOps(v MShellObject) ([]editOp, error) {
	list, ok := v.(*MShellList)
	if !ok {
		return nil, fmt.Errorf("an action must be a list of EditOp, found a %s (%s)", v.TypeName(), v.DebugString())
	}
	ops := make([]editOp, len(list.Items))
	for i, item := range list.Items {
		op, err := parseEditOp(item)
		if err != nil {
			return nil, err
		}
		ops[i] = op
	}
	return ops, nil
}

// opValue is the EditOp value of an op.
func (state *EvalState) opValue(op editOp) MShellObject {
	member := editOpMembers[op.kind]
	switch op.kind {
	case opInsert, opMessage:
		return state.builtinEnumValue(member, MShellString{op.a})
	case opSetLine:
		return state.builtinEnumValue(member, MShellString{op.a}, MShellString{op.b})
	case opRun, opRunOps:
		return state.builtinEnumValue(member, op.quote)
	}
	return state.builtinEnumValue(member)
}

// bindingEntry is one binding bindingAdd added, or a default.
type bindingEntry struct {
	bindingCommon[editMode]
	// The action: ops for bindingOps, fn for bindingFunc.
	ops []editOp
	fn  *MShellQuotation
}

// pure reports whether the binding only edits the line. A quote's result
// is not known until it runs, so a bindingFunc is never pure.
func (e *bindingEntry) pure() bool {
	if e.fn != nil {
		return false
	}
	for _, op := range e.ops {
		if !op.kind.pure() {
			return false
		}
	}
	return true
}

// describe names a binding in a message.
func (e *bindingEntry) describe() string { return e.describeIn(editModeMembers[:]) }

// parseBinding reads a Binding record. It fails only on a value the type
// checker refuses; a record that can never fire gives an inert entry.
func (state *EvalState) parseBinding(record *MShellDict) (*bindingEntry, error) {
	common, opsValue, fn, err := parseBindingFields[editMode](state, record, "BindingAction", "EditMode", editModeMembers[:])
	if err != nil {
		return nil, err
	}
	e := &bindingEntry{bindingCommon: common, fn: fn}
	if opsValue != nil {
		if e.ops, err = parseEditOps(opsValue); err != nil {
			return nil, err
		}
	}
	if e.invalid == "" {
		for i, op := range e.ops {
			if op.kind.ends() && i < len(e.ops)-1 {
				e.invalid = fmt.Sprintf("'%s' must be its last op", editOpMembers[op.kind])
				break
			}
		}
	}
	return e, nil
}

var shellKeymapKind = keymapKind[*bindingEntry]{
	builtin:   "binding",
	record:    "a Binding",
	modeCount: int(editModeCount),
	parse:     (*EvalState).parseBinding,
	defaults:  (*EvalState).defaultBindingEntries,
}

// shellKeymap is the shell's keymap, with its defaults.
func (state *EvalState) shellKeymap() *keymap[*bindingEntry] {
	return state.bindings.ready(state, &shellKeymapKind)
}

// ----- The default keymap (section 5.4) -----

// defaultBinding is a binding the shell adds before the init file runs.
type defaultBinding struct {
	mode editMode
	keys []KeyCode
	ops  []editOp
}

func ops(kinds ...editOpKind) []editOp {
	out := make([]editOp, len(kinds))
	for i, k := range kinds {
		out[i] = editOp{kind: k}
	}
	return out
}

// defaultQuote parses a quotation of the default keymap.
func defaultQuote(src string) *MShellQuotation {
	file, err := parseMShellInput(src, builtinDeclFile)
	if err != nil || len(file.Items) != 1 {
		panic("default binding quote: " + src)
	}
	q := file.Items[0].(*MShellParseQuote)
	return &MShellQuotation{Tokens: q.Items, Variables: map[string]MShellObject{}, MShellParseQuote: q}
}

var defaultBindings = sync.OnceValue(func() []defaultBinding {
	ctrl := func(r rune) KeyCode { return KeyCode(r) | KeyModCtrl }
	alt := func(r rune) KeyCode { return KeyCode(r) | KeyModAlt }
	var bs []defaultBinding
	add := func(mode editMode, o []editOp, keys ...KeyCode) {
		for _, k := range keys {
			bs = append(bs, defaultBinding{mode: mode, keys: []KeyCode{k}, ops: o})
		}
	}
	add(modeInsert, ops(opAccept), NamedEnter)
	add(modeInsert, ops(opExpandAbbr, opSelfInsert), ' ', '|', ';', '<', '>', ']', ')')
	add(modeInsert, []editOp{{kind: opInsert, a: " "}}, ctrl(' '))
	add(modeInsert, ops(opComplete), NamedTab)
	add(modeInsert, ops(opCompleteBack), NamedTab|KeyModShift)
	add(modeInsert, ops(opLeft), NamedLeft, ctrl('b'))
	add(modeInsert, ops(opRight), NamedRight)
	add(modeInsert, ops(opForward), ctrl('f'))
	add(modeInsert, ops(opWordLeft), alt('b'))
	add(modeInsert, ops(opWordRight), alt('f'))
	add(modeInsert, ops(opLineStart), NamedHome, ctrl('a'))
	add(modeInsert, ops(opLineEnd), NamedEnd, ctrl('e'))
	add(modeInsert, ops(opBackspace), NamedBackspace)
	add(modeInsert, ops(opDelete), NamedDelete)
	add(modeInsert, ops(opDeleteWordLeft), ctrl('w'), ctrl('h'), NamedBackspace|KeyModCtrl)
	add(modeInsert, ops(opDeleteToStart), ctrl('u'))
	add(modeInsert, ops(opDeleteToEnd), ctrl('k'))
	add(modeInsert, ops(opHistoryPrev), NamedUp)
	add(modeInsert, ops(opHistoryNext), NamedDown)
	add(modeInsert, ops(opHistorySearchBack), ctrl('p'))
	add(modeInsert, ops(opHistorySearchForward), ctrl('n'))
	add(modeInsert, ops(opAcceptSuggestion), ctrl('y'))
	add(modeInsert, ops(opLastArg), alt('.'))
	add(modeInsert, ops(opClearScreen), ctrl('l'))
	add(modeInsert, []editOp{{kind: opRun, quote: defaultQuote("('.' mshFileManager)")}, {kind: opClearScreen}}, ctrl('o'))
	add(modeInsert, []editOp{{kind: opRunOps, quote: defaultQuote("(editInEditor)")}}, ctrl('g'))
	add(modeInsert, ops(opRemeasure), KeyCode('r')|KeyModAlt|KeyModShift)
	add(modeInsert, ops(opExit), ctrl('c'), ctrl('d'), alt('o'))

	add(modeComplete, ops(opCompleteNext), NamedTab, NamedDown, ctrl('n'))
	add(modeComplete, ops(opCompletePrev), NamedTab|KeyModShift, NamedUp, ctrl('p'))
	add(modeComplete, ops(opCompleteLeft), NamedLeft)
	add(modeComplete, ops(opCompleteRight), NamedRight)
	add(modeComplete, ops(opCompleteAccept), NamedEnter)
	return bs
})

// defaultBindingEntries are the entries of the default keymap.
func (state *EvalState) defaultBindingEntries() []*bindingEntry {
	var entries []*bindingEntry
	for _, d := range defaultBindings() {
		opList := NewList(len(d.ops))
		for i, op := range d.ops {
			opList.Items[i] = state.opValue(op)
		}
		record := state.defaultRecord(d.keys, state.builtinEnumValue("bindingOps", opList), editModeMembers[d.mode], "")
		entries = append(entries, &bindingEntry{bindingCommon: bindingCommon[editMode]{record: record, keys: d.keys, mode: d.mode}, ops: d.ops})
	}
	return entries
}

// ----- Resolving keys (section 6.5, ai/key-bindings/KeyResolution.tla) -----

// editSnapshot is what a pure binding of the editor changes: the line and
// the cursor.
type editSnapshot struct {
	line  SourceText
	index ByteOffset
}

// editMode is the keymap the editor is in.
func (state *TermState) editMode() editMode {
	if state.tabCycleActive {
		return modeComplete
	}
	return modeInsert
}

// resolveKey handles one key: it runs the longest binding the key
// completes, or does the key's own action, and keeps the keys that may
// still become part of a binding. It returns whether the shell should end.
func (state *TermState) resolveKey(code KeyCode, now time.Time) (bool, error) {
	km := state.evalState.shellKeymap()
	r := &state.keys
	mode := state.editMode()
	if mode != r.mode {
		r.clear()
		r.mode = mode
	}
	w := r.push(code, now, editSnapshot{line: state.currentCommand, index: state.index})

	for {
		trie := &km.tries[mode]
		fired, end, err := r.fireLongest(trie, w, state.fireBinding)
		if fired || err != nil {
			state.showPendingKeys()
			return end, err
		}
		// A key complete mode does not bind leaves it, and acts as in
		// insert mode, on its own.
		if mode == modeComplete && !r.canGrow(trie, w) {
			state.resetTabCycle()
			state.tabCompletions0 = state.tabCompletions0[:0]
			state.tabCompletions1 = state.tabCompletions1[:0]
			mode, r.mode = modeInsert, modeInsert
			r.clear()
			w = w[len(w)-1:]
			continue
		}
		state.resetLastArgCycle()
		state.ownAction(code)
		r.keep(trie, w)
		state.showPendingKeys()
		return false, nil
	}
}

// ownAction is what a key does when no binding uses it: a key that types
// text inserts it, and any other key does nothing.
func (state *TermState) ownAction(code KeyCode) {
	if r, ok := code.text(); ok {
		state.PushChars([]rune{r})
	}
}

// showPendingKeys shows the pending keys below the line when one of them
// has nothing on the line to show for it, as Emacs does: r'x' keyCtrl -
func (state *TermState) showPendingKeys() {
	r := &state.keys
	for _, k := range r.pending[:r.n] {
		if _, ok := k.code.text(); !ok {
			state.abbrMessage = keysSource(r.pendingCodes()) + " -"
			return
		}
	}
}

// fireBinding runs a binding for the keys of w, on the line as it was
// before them. It reports false when the binding's quote declined.
func (state *TermState) fireBinding(e *bindingEntry, w []pendingKey[editSnapshot]) (bool, bool, error) {
	program, ok := state.bindingProgram(e, w)
	if !ok {
		return false, false, nil
	}
	state.currentCommand, state.index = w[0].snap.line, w[0].snap.index
	end, err := state.runEditOps(program, w[len(w)-1].code)
	return true, end, err
}

// keyUsed reports whether a binding of the current mode starts with key,
// or continues the pending keys with it.
func (state *TermState) keyUsed(key KeyCode) bool {
	return state.keys.used(&state.evalState.shellKeymap().tries[state.editMode()], key)
}

// bindingProgram is the program a binding gives for the keys of w: its ops,
// or what its quote returns. It reports false when the quote declined or
// failed.
func (state *TermState) bindingProgram(e *bindingEntry, w []pendingKey[editSnapshot]) ([]editOp, bool) {
	if e.fn == nil {
		return e.ops, true
	}
	ev := &state.evalState
	info := NewDict()
	line, index := w[0].snap.line, w[0].snap.index
	// The quote may keep what it is given, so it gets copies, not the
	// editor's arena.
	info.Items["before"] = MShellString{strings.Clone(string(line[:index]))}
	info.Items["after"] = MShellString{strings.Clone(string(line[index:]))}
	keys := NewList(len(w))
	for i, k := range w {
		keys.Items[i] = ev.keyValue(k.code)
	}
	info.Items["keys"] = keys
	info.Items["mode"] = ev.builtinEnumValue(editModeMembers[state.keys.mode])
	stack := MShellStack{info}

	var out bytes.Buffer
	context := state.context
	context.StandardOutput = &out
	context.StandardError = &out
	run := state.runUserQuote(e.fn, &stack, context, abbrTimeout, newEvalCancel())

	what := "The quote of binding " + e.describe()
	var program []editOp
	some := false
	problem := run.problem(what, "the shell", abbrTimeout)
	if problem == "" {
		program, some, problem = stackProgram(stack, what, "Maybe[[EditOp]]", parseEditOps)
	}
	if problem == "" {
		state.abbrProblem = ""
		for i, op := range program {
			if op.kind.ends() && i < len(program)-1 {
				state.abbrMessage = fmt.Sprintf("%s returned ops after '%s'; they were not run.\n", what, editOpMembers[op.kind])
				program = program[:i+1]
				break
			}
		}
		return program, some
	}
	// The same problem is shown once, not again while the quote keeps
	// failing the same way.
	if problem != state.abbrProblem {
		state.abbrMessage = problem
	}
	state.abbrProblem = problem
	return nil, false
}

// ----- Running ops (section 7) -----

// runEditOps runs a program for the editor. key is the last key of the
// sequence that ran it, which editSelfInsert types.
func (state *TermState) runEditOps(program []editOp, key KeyCode) (bool, error) {
	keepLastArg := false
	for _, op := range program {
		if op.kind == opLastArg {
			keepLastArg = true
		}
	}
	if !keepLastArg {
		state.resetLastArgCycle()
	}
	state.abbrCursor = abbrExpansion{}
	defer func() { state.abbrCursor = abbrExpansion{} }()
	for _, op := range program {
		end, err := state.runEditOp(op, key)
		if end || err != nil {
			return end, err
		}
	}
	return false, nil
}

func (state *TermState) runEditOp(op editOp, key KeyCode) (bool, error) {
	if start, end, ok := lineMoveOrDelete(op.kind, state.currentCommand, state.index, isSpaceByte); ok {
		if start < end {
			state.replaceText("", start, end)
		} else {
			state.index = start
		}
		return false, nil
	}
	switch op.kind {
	case opInsert:
		state.replaceText(op.a, state.index, state.index)
	case opSelfInsert:
		r, ok := key.text()
		if !ok {
			return false, nil
		}
		// After an abbreviation that put the cursor at its marker, the key
		// goes at the end of the expansion, and a space is dropped.
		if exp := state.abbrCursor; exp.HasCursor {
			if r != ' ' {
				state.replaceText(string(r), exp.End, exp.End)
			}
			state.index = exp.Cursor
			state.abbrCursor = abbrExpansion{}
			return false, nil
		}
		state.PushChars([]rune{r})
	case opSetLine:
		state.currentCommand = SourceText(state.lineArena.concat(op.a, op.b, ""))
		state.index = graphemeCeil(state.currentCommand, ByteOffset(len(op.a)))
		state.resetHistorySearch()
	case opExpandAbbr:
		if exp, ok := state.expandAbbr(false); ok {
			state.abbrCursor = exp
		}
	case opAccept:
		state.expandAbbrOnEnter()
		shouldExit, _ := state.ExecuteCurrentCommand()
		state.keys.clear()
		return shouldExit, nil
	case opExit:
		fmt.Fprintf(os.Stdout, "\r\n")
		return true, nil
	case opRun:
		return state.runBindingQuote(op.quote, false)
	case opRunOps:
		return state.runBindingQuote(op.quote, true)
	case opForward:
		if state.index == state.commandEnd() {
			state.acceptHistoryCompletion()
		} else {
			state.index = graphemeNext(state.currentCommand, state.index)
		}
	case opAcceptSuggestion:
		state.acceptHistoryCompletion()
	case opHistoryPrev:
		state.historyPrev()
	case opHistoryNext:
		state.historyNext()
	case opHistorySearchBack:
		state.historySearch(-1)
	case opHistorySearchForward:
		state.historySearch(1)
	case opLastArg:
		state.cycleLastArgument()
	case opComplete:
		if state.tabCycleActive && len(state.tabCycleMatches) > 0 {
			state.cycleTabCompletion(1)
		} else {
			state.completeAtCursor()
		}
	case opCompleteBack:
		if state.tabCycleActive && len(state.tabCycleMatches) > 0 {
			state.cycleTabCompletion(-1)
		} else {
			state.completeAtCursor()
		}
	case opCompleteNext:
		state.cycleTabCompletion(1)
	case opCompletePrev:
		state.cycleTabCompletion(-1)
	case opCompleteLeft:
		state.cycleTabCompletionColumn(-1)
	case opCompleteRight:
		state.cycleTabCompletionColumn(1)
	case opCompleteAccept:
		state.resetTabCycle()
		state.tabCompletions0 = state.tabCompletions0[:0]
		state.tabCompletions1 = state.tabCompletions1[:0]
	case opClearScreen:
		state.ClearScreen()
	case opRemeasure:
		// The user's reset for measured widths after a reattach, font
		// change, or any odd frame: forget every observation and measure
		// again on the next repaint. There is no automatic epoch tracking.
		clear(state.widthCache.Entries)
		state.widthProbesBlocked = false
	case opMessage:
		state.abbrMessage = op.a
	}
	return false, nil
}

// runBindingQuote runs the quote of editRun, or of editRunOps with the
// editor's state, as a line runs: in the foreground, with the terminal
// and no time limit (section 7.3). The line and cursor are kept, and
// nothing is added to history. For editRunOps, the ops the quote returns
// then run.
func (state *TermState) runBindingQuote(quote *MShellQuotation, withState bool) (bool, error) {
	ev := &state.evalState
	line, index := state.currentCommand, state.index
	state.resetTabCycle()
	state.tabCompletions0 = state.tabCompletions0[:0]
	state.tabCompletions1 = state.tabCompletions1[:0]
	state.keys.clear()

	// Leave the command line as Enter does.
	state.index = state.commandEnd()
	if err := state.refreshInteractiveDisplay(false); err != nil {
		state.Logf("Error painting before a binding's quote: %s\n", err)
	}
	state.leaveRawMode()
	state.resetCursorShape()
	fmt.Fprintln(os.Stdout)

	stack := MShellStack{}
	if withState {
		info := NewDict()
		info.Items["before"] = MShellString{strings.Clone(string(line[:index]))}
		info.Items["after"] = MShellString{strings.Clone(string(line[index:]))}
		info.Items["keys"] = NewList(0)
		info.Items["mode"] = ev.builtinEnumValue("modeInsert")
		stack = append(stack, info)
	}
	result, err := ev.EvaluateQuote(quote, &stack, state.context, state.stdLibDefs)
	ev.printAbbrWarnings()

	var program []editOp
	switch {
	case err != nil:
		fmt.Fprintln(os.Stderr, terminalSafeText(err.Error(), true))
	case result.ExitCalled:
		return true, nil
	case !result.Success:
	case withState:
		var problem string
		program, _, problem = stackProgram(stack, "The quote of editRunOps", "Maybe[[EditOp]]", parseEditOps)
		fmt.Fprint(os.Stderr, terminalSafeText(problem, true))
	case len(stack) > 0:
		fmt.Fprintf(os.Stderr, "The quote of editRun left %d values on the stack; they were dropped.\n", len(stack))
	}

	state.currentCommand, state.index = line, index
	if err := state.printPrompt(); err != nil {
		return true, err
	}
	if len(program) > 0 {
		return state.runEditOps(program, noKey)
	}
	return false, nil
}

// stackProgram reads the program a quote left on its stack, which must be
// one Maybe of a program, written maybeType. some is false when the Maybe
// is None. problem says why the stack holds no program, or is "". what names
// the quote, starting a sentence.
func stackProgram[O any](stack MShellStack, what, maybeType string, parse func(MShellObject) ([]O, error)) (program []O, some bool, problem string) {
	m, ok := singleMaybe(stack)
	if !ok {
		return nil, false, what + " must leave one " + maybeType + " on the stack.\n"
	}
	if m.IsNone() {
		return nil, false, ""
	}
	program, err := parse(m.obj)
	if err != nil {
		return nil, false, what + ": " + err.Error() + ".\n"
	}
	return program, true, ""
}

// singleMaybe is the one Maybe left on a stack.
func singleMaybe(stack MShellStack) (*Maybe, bool) {
	if len(stack) != 1 {
		return nil, false
	}
	m, ok := stack[0].(*Maybe)
	return m, ok
}
