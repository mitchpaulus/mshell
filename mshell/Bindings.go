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
	"slices"
	"strings"
	"sync"
	"time"
)

// maxBindingKeys is the most keys a binding may have, and so the most the
// editor ever keeps pending.
const maxBindingKeys = 8

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
	// record is the Binding bindingAdd was given, with its mode filled in,
	// which bindingList gives back.
	record *MShellDict
	keys   []KeyCode
	mode   editMode
	// The action: ops for bindingOps, fn for bindingFunc.
	ops []editOp
	fn  *MShellQuotation
	// The timing fields, zero when not given.
	within, idleBefore time.Duration
	// invalid says why the record can never fire, or is "". inert is that,
	// or the conflict with a newer binding that made it inert.
	invalid string
	inert   string
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

// parseBinding reads a Binding record. It fails only on a value the type
// checker refuses; a record that can never fire gives an inert entry.
func (state *EvalState) parseBinding(record *MShellDict) (*bindingEntry, error) {
	e := &bindingEntry{record: record}

	keys, ok := record.Items["keys"].(*MShellList)
	if !ok {
		return nil, fmt.Errorf("the 'keys' field must be a list of Key")
	}
	for _, item := range keys.Items {
		k, err := keyCodeOf(item)
		if err != nil {
			return nil, err
		}
		e.keys = append(e.keys, k)
	}

	action := enumField(record, "action", "BindingAction")
	if action == nil || len(action.Payload) != 1 {
		return nil, fmt.Errorf("the 'action' field must be a BindingAction")
	}
	switch action.Member {
	case "bindingOps":
		ops, err := parseEditOps(action.Payload[0])
		if err != nil {
			return nil, err
		}
		e.ops = ops
	default:
		q, ok := action.Payload[0].(*MShellQuotation)
		if !ok {
			return nil, fmt.Errorf("'bindingFunc' must hold a quotation")
		}
		e.fn = q
	}

	if _, given := record.Items["mode"]; !given {
		record.Items["mode"] = state.builtinEnumValue("modeInsert")
	}
	mode := enumField(record, "mode", "EditMode")
	if mode == nil {
		return nil, fmt.Errorf("the 'mode' field must be an EditMode")
	}
	e.mode = editMode(mode.MemberIndex)

	timing := []struct {
		key string
		to  *time.Duration
	}{{"within", &e.within}, {"idleBefore", &e.idleBefore}}
	for _, f := range timing {
		v, given := record.Items[f.key]
		if !given {
			continue
		}
		n, ok := v.(MShellInt)
		if !ok {
			return nil, fmt.Errorf("the '%s' field must be an int", f.key)
		}
		if n.Value <= 0 && e.invalid == "" {
			e.invalid = fmt.Sprintf("'%s' is %d; a time must be more than 0 ms", f.key, n.Value)
		}
		*f.to = time.Duration(n.Value) * time.Millisecond
	}
	if v, given := record.Items["description"]; given {
		if _, ok := v.(MShellString); !ok {
			return nil, fmt.Errorf("the 'description' field must be a str")
		}
	}

	switch {
	case e.invalid != "":
	case len(e.keys) == 0:
		e.invalid = "it has no keys"
	case len(e.keys) > maxBindingKeys:
		e.invalid = fmt.Sprintf("it has %d keys, and a binding has at most %d", len(e.keys), maxBindingKeys)
	default:
		for i, op := range e.ops {
			if op.kind.ends() && i < len(e.ops)-1 {
				e.invalid = fmt.Sprintf("'%s' must be its last op", editOpMembers[op.kind])
				break
			}
		}
	}
	return e, nil
}

// describe names a binding in a message.
func (e *bindingEntry) describe() string {
	s := "[" + keysSource(e.keys) + "]"
	if e.mode != modeInsert {
		s += " in " + editModeMembers[e.mode]
	}
	return s
}

// endsProperPrefix reports whether s ends a proper prefix of t.
func endsProperPrefix(s, t []KeyCode) bool {
	for n := len(s); n < len(t); n++ {
		if slices.Equal(t[n-len(s):n], s) {
			return true
		}
	}
	return false
}

// bindingsConflict reports whether two bindings of one mode conflict
// (section 6.3): an effect that ends a proper prefix of another binding
// would run, and then the other would complete over it.
func bindingsConflict(a, b *bindingEntry) bool {
	if slices.Equal(a.keys, b.keys) {
		return true
	}
	return (!a.pure() && endsProperPrefix(a.keys, b.keys)) || (!b.pure() && endsProperPrefix(b.keys, a.keys))
}

// recomputeBindings works out which bindings are inert, oldest first: each
// binding makes inert the older ones in force it conflicts with. Then it
// builds each mode's trie of the bindings in force.
func (state *EvalState) recomputeBindings() {
	for _, e := range state.Bindings {
		e.inert = e.invalid
	}
	for i, e := range state.Bindings {
		if e.inert != "" {
			continue
		}
		for _, o := range state.Bindings[:i] {
			if o.inert == "" && o.mode == e.mode && bindingsConflict(o, e) {
				o.inert = "it conflicts with the newer binding " + e.describe()
			}
		}
	}
	for m := range state.bindingTries {
		state.bindingTries[m].build(state.Bindings, editMode(m))
	}
}

// addBinding adds an entry as the newest, replacing one with the same keys
// in the same mode, and notes each binding it makes inert for the
// interactive shell to print.
func (state *EvalState) addBinding(e *bindingEntry) {
	state.ensureDefaultBindings()
	state.Bindings = slices.DeleteFunc(state.Bindings, func(o *bindingEntry) bool {
		return o.mode == e.mode && slices.Equal(o.keys, e.keys)
	})
	wasInert := make(map[*bindingEntry]bool, len(state.Bindings))
	for _, o := range state.Bindings {
		wasInert[o] = o.inert != ""
	}
	state.Bindings = append(state.Bindings, e)
	state.recomputeBindings()
	for _, o := range state.Bindings {
		if o.inert != "" && !wasInert[o] {
			state.AbbrWarnings = append(state.AbbrWarnings, fmt.Sprintf("Binding %s never fires: %s.", o.describe(), o.inert))
		}
	}
}

// evalBindingAdd runs bindingAdd: (Binding -- ).
func (state *EvalState) evalBindingAdd(t *Token, stack *MShellStack) *EvalResult {
	obj, err := stack.Pop()
	if err != nil {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: Cannot do 'bindingAdd' operation on an empty stack.\n", t.Line, t.Column))
	}
	dict, ok := obj.(*MShellDict)
	if !ok {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: 'bindingAdd' takes a Binding, found a %s (%s).\n", t.Line, t.Column, obj.TypeName(), obj.DebugString()))
	}
	copied, err := DeepCopy(dict)
	if err != nil {
		return state.checkedPtr(fmt.Sprintf("%d:%d: 'bindingAdd': %s\n", t.Line, t.Column, err.Error()))
	}
	e, err := state.parseBinding(copied.(*MShellDict))
	if err != nil {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: 'bindingAdd' takes a Binding: %s.\n", t.Line, t.Column, err.Error()))
	}
	state.addBinding(e)
	return nil
}

// evalBindingErase runs bindingErase: ([Key] -- ). It removes the bindings
// with those keys in every mode, defaults included.
func (state *EvalState) evalBindingErase(t *Token, stack *MShellStack) *EvalResult {
	obj, err := stack.Pop()
	if err != nil {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: Cannot do 'bindingErase' operation on an empty stack.\n", t.Line, t.Column))
	}
	list, ok := obj.(*MShellList)
	if !ok {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: 'bindingErase' takes a list of Key, found a %s (%s).\n", t.Line, t.Column, obj.TypeName(), obj.DebugString()))
	}
	keys := make([]KeyCode, len(list.Items))
	for i, item := range list.Items {
		k, err := keyCodeOf(item)
		if err != nil {
			return state.mismatchPtr(fmt.Sprintf("%d:%d: 'bindingErase': %s.\n", t.Line, t.Column, err.Error()))
		}
		keys[i] = k
	}
	state.ensureDefaultBindings()
	state.Bindings = slices.DeleteFunc(state.Bindings, func(e *bindingEntry) bool { return slices.Equal(e.keys, keys) })
	state.recomputeBindings()
	return nil
}

// evalBindingList runs bindingList: ( -- new [Binding]), oldest first, so
// the defaults come first.
func (state *EvalState) evalBindingList(t *Token, stack *MShellStack) *EvalResult {
	state.ensureDefaultBindings()
	list := NewList(len(state.Bindings))
	for i, e := range state.Bindings {
		copied, err := DeepCopy(e.record)
		if err != nil {
			return state.checkedPtr(fmt.Sprintf("%d:%d: 'bindingList': %s\n", t.Line, t.Column, err.Error()))
		}
		list.Items[i] = copied
	}
	stack.Push(list)
	return nil
}

// bindingTrie is one mode's bindings in force, by their keys.
type bindingTrie struct {
	nodes []trieNode
}

type trieNode struct {
	next  map[KeyCode]int32
	entry *bindingEntry // the binding whose keys end here, or nil
	// For a node with bindings below it, what their timing allows, so the
	// editor knows whether keys can still grow into one: the longest
	// within (or none at all), and the shortest idleBefore.
	anyWithin     bool
	maxWithin     time.Duration
	minIdleBefore time.Duration
}

func (t *bindingTrie) build(entries []*bindingEntry, mode editMode) {
	t.nodes = append(t.nodes[:0], trieNode{minIdleBefore: -1})
	for _, e := range entries {
		if e.inert != "" || e.mode != mode {
			continue
		}
		n := int32(0)
		for _, k := range e.keys {
			node := &t.nodes[n]
			if e.within == 0 {
				node.anyWithin = true
			}
			node.maxWithin = max(node.maxWithin, e.within)
			if node.minIdleBefore < 0 || e.idleBefore < node.minIdleBefore {
				node.minIdleBefore = e.idleBefore
			}
			next, ok := node.next[k]
			if !ok {
				if node.next == nil {
					node.next = make(map[KeyCode]int32)
				}
				next = int32(len(t.nodes))
				node.next[k] = next
				t.nodes = append(t.nodes, trieNode{minIdleBefore: -1})
			}
			n = next
		}
		t.nodes[n].entry = e
	}
}

// walk follows keys from the root, and gives the node they end at, or -1.
func (t *bindingTrie) walk(keys []pendingKey) int32 {
	if len(t.nodes) == 0 {
		return -1
	}
	n := int32(0)
	for i := range keys {
		next, ok := t.nodes[n].next[keys[i].code]
		if !ok {
			return -1
		}
		n = next
	}
	return n
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

// ensureDefaultBindings adds the default keymap, once, before any binding
// a file adds, so a user's binding is always the newer one.
func (state *EvalState) ensureDefaultBindings() {
	if state.defaultBindingsAdded {
		return
	}
	state.defaultBindingsAdded = true
	state.ensureBuiltinEnums()
	for _, d := range defaultBindings() {
		keys := NewList(len(d.keys))
		for i, k := range d.keys {
			keys.Items[i] = state.keyValue(k)
		}
		opList := NewList(len(d.ops))
		for i, op := range d.ops {
			opList.Items[i] = state.opValue(op)
		}
		record := NewDict()
		record.Items["keys"] = keys
		record.Items["action"] = state.builtinEnumValue("bindingOps", opList)
		record.Items["mode"] = state.builtinEnumValue(editModeMembers[d.mode])
		e := &bindingEntry{record: record, keys: d.keys, mode: d.mode, ops: d.ops}
		state.Bindings = append(state.Bindings, e)
	}
	state.recomputeBindings()
}

// ----- Resolving keys (section 6.5, ai/key-bindings/KeyResolution.tla) -----

// pendingKey is a key that may still be part of a binding, with the line
// as it was before it.
type pendingKey struct {
	code KeyCode
	num  int           // the key's number since the shell started
	gap  time.Duration // since the event before it
	// first is set for the first key the shell read, which has no gap
	// before it.
	first bool
	line  SourceText
	index ByteOffset
}

// keySpan is a pure binding in force among the pending keys: keys start
// to end, by number.
type keySpan struct{ start, end int }

// keyResolver is what the editor remembers between keys.
type keyResolver struct {
	pending [maxBindingKeys]pendingKey
	n       int
	inForce [maxBindingKeys]keySpan
	nForce  int
	mode    editMode
	next    int
	lastAt  time.Time
	hasLast bool
}

func (r *keyResolver) clear() {
	r.n, r.nForce = 0, 0
}

// event notes an event that is not a key: a paste. It ends any sequence.
func (r *keyResolver) event(now time.Time) {
	r.clear()
	r.lastAt, r.hasLast = now, true
}

// fits reports whether a binding may start at key num: not inside a
// binding in force.
func (r *keyResolver) fits(num int) bool {
	for _, s := range r.inForce[:r.nForce] {
		if num > s.start && num <= s.end {
			return false
		}
	}
	return true
}

// timed reports whether the keys of w satisfy e's within and idleBefore.
func timed(e *bindingEntry, w []pendingKey) bool {
	if e.within > 0 {
		for _, k := range w[1:] {
			if k.gap > e.within {
				return false
			}
		}
	}
	return e.idleBefore == 0 || w[0].first || w[0].gap >= e.idleBefore
}

// grows reports whether some binding has the keys of w as a proper prefix,
// with its times so far.
func (t *bindingTrie) grows(w []pendingKey) bool {
	n := t.walk(w)
	if n < 0 || len(t.nodes[n].next) == 0 {
		return false
	}
	node := &t.nodes[n]
	if !node.anyWithin {
		for _, k := range w[1:] {
			if k.gap > node.maxWithin {
				return false
			}
		}
	}
	return node.minIdleBefore <= 0 || w[0].first || w[0].gap >= node.minIdleBefore
}

// canGrow reports whether some suffix of w can still grow into a binding.
func (r *keyResolver) canGrow(t *bindingTrie, w []pendingKey) bool {
	for p := range w {
		if r.fits(w[p].num) && t.grows(w[p:]) {
			return true
		}
	}
	return false
}

// keep keeps as pending the keys of w from the first one from which they
// can still grow into a binding, and forgets the bindings in force that
// start before it.
func (r *keyResolver) keep(t *bindingTrie, w []pendingKey) {
	for p := range w {
		if r.fits(w[p].num) && t.grows(w[p:]) {
			copy(r.pending[:], w[p:])
			r.n = len(w) - p
			start := w[p].num
			kept := 0
			for _, s := range r.inForce[:r.nForce] {
				if s.start >= start {
					r.inForce[kept] = s
					kept++
				}
			}
			r.nForce = kept
			return
		}
	}
	r.clear()
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
	ev := &state.evalState
	ev.ensureDefaultBindings()
	r := &state.keys
	mode := state.editMode()
	if mode != r.mode {
		r.clear()
		r.mode = mode
	}
	gap, first := now.Sub(r.lastAt), !r.hasLast
	r.lastAt, r.hasLast = now, true
	if r.n == maxBindingKeys {
		// Never: a pending key is the start of a binding of at most
		// maxBindingKeys keys, and this key would be past its end.
		r.clear()
	}
	r.pending[r.n] = pendingKey{code: code, num: r.next, gap: gap, first: first, line: state.currentCommand, index: state.index}
	r.next++
	var window [maxBindingKeys]pendingKey
	n := copy(window[:], r.pending[:r.n+1])
	w := window[:n]

	for {
		trie := &ev.bindingTries[mode]
		fired, end, err := state.fireLongest(trie, w)
		if fired || err != nil {
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
			codes := make([]KeyCode, r.n)
			for i := range codes {
				codes[i] = r.pending[i].code
			}
			state.abbrMessage = keysSource(codes) + " -"
			return
		}
	}
}

// fireLongest runs the longest binding that ends the keys of w, whose
// times fit and which does not decline. It reports whether one ran.
func (state *TermState) fireLongest(trie *bindingTrie, w []pendingKey) (bool, bool, error) {
	r := &state.keys
	for p := range w {
		n := trie.walk(w[p:])
		if n < 0 {
			continue
		}
		e := trie.nodes[n].entry
		if e == nil || !timed(e, w[p:]) || !r.fits(w[p].num) {
			continue
		}
		program, ok := state.bindingProgram(e, w[p:])
		if !ok {
			continue
		}
		state.currentCommand, state.index = w[p].line, w[p].index
		end, err := state.runEditOps(program, w[len(w)-1].code)
		if e.pure() {
			if r.nForce < len(r.inForce) {
				kept := 0
				for _, s := range r.inForce[:r.nForce] {
					if s.start < w[p].num {
						r.inForce[kept] = s
						kept++
					}
				}
				r.inForce[kept] = keySpan{start: w[p].num, end: w[len(w)-1].num}
				r.nForce = kept + 1
			}
			r.keep(trie, w)
			state.showPendingKeys()
		} else {
			r.clear()
		}
		return true, end, err
	}
	return false, false, nil
}

// keyUsed reports whether a binding of the current mode starts with key,
// or continues the pending keys with it.
func (state *TermState) keyUsed(key KeyCode) bool {
	ev := &state.evalState
	ev.ensureDefaultBindings()
	t := &ev.bindingTries[state.editMode()]
	r := &state.keys
	var w [maxBindingKeys]pendingKey
	n := copy(w[:maxBindingKeys-1], r.pending[:r.n])
	w[n] = pendingKey{code: key}
	for p := 0; p <= n; p++ {
		if t.walk(w[p:n+1]) >= 0 {
			return true
		}
	}
	return false
}

// bindingProgram is the program a binding gives for the keys of w: its ops,
// or what its quote returns. It reports false when the quote declined or
// failed.
func (state *TermState) bindingProgram(e *bindingEntry, w []pendingKey) ([]editOp, bool) {
	if e.fn == nil {
		return e.ops, true
	}
	ev := &state.evalState
	info := NewDict()
	line, index := w[0].line, w[0].index
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
	var problem string
	switch {
	case run.err != nil:
		problem = run.err.Error()
	case run.result.ExitCalled:
		problem = what + " called 'exit'; that does not end the shell.\n"
	case run.result.Success:
		if len(stack) == 1 {
			if m, ok := stack[0].(*Maybe); ok {
				if m.IsNone() {
					state.abbrProblem = ""
					return nil, false
				}
				program, err := parseEditOps(m.obj)
				if err == nil {
					state.abbrProblem = ""
					for i, op := range program {
						if op.kind.ends() && i < len(program)-1 {
							state.abbrMessage = fmt.Sprintf("%s returned ops after '%s'; they were not run.\n", what, editOpMembers[op.kind])
							program = program[:i+1]
							break
						}
					}
					return program, true
				}
			}
		}
		problem = what + " must leave one Maybe[[EditOp]] on the stack.\n"
	default:
		problem = run.failure(what, abbrTimeout)
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
	case opLeft:
		state.index = graphemePrevious(state.currentCommand, state.index)
	case opRight:
		state.index = graphemeNext(state.currentCommand, state.index)
	case opWordLeft:
		state.index = wordLeft(state.currentCommand, state.index)
	case opWordRight:
		state.index = wordRight(state.currentCommand, state.index)
	case opLineStart:
		state.index = 0
	case opLineEnd:
		state.index = state.commandEnd()
	case opBackspace:
		if state.index > 0 {
			start := graphemePrevious(state.currentCommand, state.index)
			state.replaceText("", start, state.index)
		}
	case opDelete:
		if state.index < state.commandEnd() {
			start := graphemeFloor(state.currentCommand, state.index)
			end := graphemeNext(state.currentCommand, state.index)
			state.replaceText("", start, end)
		}
	case opDeleteWordLeft:
		state.deletePreviousWord()
	case opDeleteToStart:
		state.replaceText("", 0, state.index)
	case opDeleteToEnd:
		state.replaceText("", state.index, state.commandEnd())
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
		if m, ok := singleMaybe(stack); ok && !m.IsNone() {
			if p, perr := parseEditOps(m.obj); perr == nil {
				program = p
			}
		} else if !ok {
			fmt.Fprintln(os.Stderr, "The quote of editRunOps must leave one Maybe[[EditOp]] on the stack.")
		}
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

// singleMaybe is the one Maybe left on a stack.
func singleMaybe(stack MShellStack) (*Maybe, bool) {
	if len(stack) != 1 {
		return nil, false
	}
	m, ok := stack[0].(*Maybe)
	return m, ok
}
