package main

// The key resolver (ai/cli_bindings.typ, section 6; ai/fm-bindings.typ,
// section 6): the pending keys and the snapshots taken before them, each
// mode's trie of the bindings in force, timing, and the conflict rule. The
// shell's editor (Bindings.go) and the file manager (FmBindings.go) each
// keep a table of bindings (a keymap) and a resolver. What a snapshot
// holds, what a key does on its own, the ops, and how a program runs are
// theirs.

import (
	"fmt"
	"slices"
	"time"
)

// maxBindingKeys is the most keys a binding may have, and so the most a
// resolver ever keeps pending.
const maxBindingKeys = 8

// keyBinding is what the resolver and a keymap need of a binding.
type keyBinding interface {
	comparable
	bindingRecord() *MShellDict
	bindingKeys() []KeyCode
	bindingMode() uint8
	bindingTiming() (within, idleBefore time.Duration)
	// pure reports whether the binding changes only what a snapshot holds,
	// so restoring the snapshot takes it back.
	pure() bool
	// inertReason is where the reason the binding never fires is kept: ""
	// while it is in force. invalidReason is why its record can never fire,
	// or "".
	inertReason() *string
	invalidReason() string
	describe() string
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
func bindingsConflict[E keyBinding](a, b E) bool {
	ak, bk := a.bindingKeys(), b.bindingKeys()
	if slices.Equal(ak, bk) {
		return true
	}
	return (!a.pure() && endsProperPrefix(ak, bk)) || (!b.pure() && endsProperPrefix(bk, ak))
}

// recomputeInert works out which bindings are inert, oldest first: each
// binding makes inert the older ones in force it conflicts with.
func recomputeInert[E keyBinding](entries []E) {
	for _, e := range entries {
		*e.inertReason() = e.invalidReason()
	}
	for i, e := range entries {
		if *e.inertReason() != "" {
			continue
		}
		for _, o := range entries[:i] {
			if *o.inertReason() == "" && o.bindingMode() == e.bindingMode() && bindingsConflict(o, e) {
				*o.inertReason() = "it conflicts with the newer binding " + e.describe()
			}
		}
	}
}

// bindingTrie is one mode's bindings in force, by their keys.
type bindingTrie[E keyBinding] struct {
	nodes []trieNode[E]
}

type trieNode[E keyBinding] struct {
	next  map[KeyCode]int32
	entry E // the binding whose keys end here, or the zero value
	// For a node with bindings below it, what their timing allows, so the
	// resolver knows whether keys can still grow into one: the longest
	// within (or none at all), and the shortest idleBefore.
	anyWithin     bool
	maxWithin     time.Duration
	minIdleBefore time.Duration
}

func (t *bindingTrie[E]) build(entries []E, mode uint8) {
	t.nodes = append(t.nodes[:0], trieNode[E]{minIdleBefore: -1})
	for _, e := range entries {
		if *e.inertReason() != "" || e.bindingMode() != mode {
			continue
		}
		within, idleBefore := e.bindingTiming()
		n := int32(0)
		for _, k := range e.bindingKeys() {
			node := &t.nodes[n]
			if within == 0 {
				node.anyWithin = true
			}
			node.maxWithin = max(node.maxWithin, within)
			if node.minIdleBefore < 0 || idleBefore < node.minIdleBefore {
				node.minIdleBefore = idleBefore
			}
			next, ok := node.next[k]
			if !ok {
				if node.next == nil {
					node.next = make(map[KeyCode]int32)
				}
				next = int32(len(t.nodes))
				node.next[k] = next
				t.nodes = append(t.nodes, trieNode[E]{minIdleBefore: -1})
			}
			n = next
		}
		t.nodes[n].entry = e
	}
}

// pendingKey is a key that may still be part of a binding, with a snapshot
// of what a pure binding changes, taken before it.
type pendingKey[S any] struct {
	code KeyCode
	num  int           // the key's number since the resolver started
	gap  time.Duration // since the event before it
	// first is set for the first key the resolver read, which has no gap
	// before it.
	first bool
	snap  S
}

// walkKeys follows keys from the root of t, and gives the node they end
// at, or -1.
func walkKeys[S any, E keyBinding](t *bindingTrie[E], keys []pendingKey[S]) int32 {
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

// timed reports whether the keys of w satisfy e's within and idleBefore.
func timed[S any, E keyBinding](e E, w []pendingKey[S]) bool {
	within, idleBefore := e.bindingTiming()
	if within > 0 {
		for _, k := range w[1:] {
			if k.gap > within {
				return false
			}
		}
	}
	return idleBefore == 0 || w[0].first || w[0].gap >= idleBefore
}

// grows reports whether some binding of t has the keys of w as a proper
// prefix, with its times so far.
func grows[S any, E keyBinding](t *bindingTrie[E], w []pendingKey[S]) bool {
	n := walkKeys(t, w)
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

// keySpan is a pure binding in force among the pending keys: keys start
// to end, by number.
type keySpan struct{ start, end int }

// keyResolver is what a program remembers between keys: S is its snapshot,
// M its modes, and E its bindings.
type keyResolver[S any, M comparable, E keyBinding] struct {
	pending [maxBindingKeys]pendingKey[S]
	n       int
	// window is the pending keys and the key being resolved.
	window  [maxBindingKeys]pendingKey[S]
	inForce [maxBindingKeys]keySpan
	nForce  int
	mode    M
	next    int
	lastAt  time.Time
	hasLast bool
}

func (r *keyResolver[S, M, E]) clear() {
	r.n, r.nForce = 0, 0
}

// event notes an event that is not a key, such as a paste. It ends any
// sequence.
func (r *keyResolver[S, M, E]) event(now time.Time) {
	r.clear()
	r.lastAt, r.hasLast = now, true
}

// push adds a key, with the snapshot taken before it, and gives the keys
// to resolve: the pending keys and it.
func (r *keyResolver[S, M, E]) push(code KeyCode, now time.Time, snap S) []pendingKey[S] {
	gap, first := now.Sub(r.lastAt), !r.hasLast
	r.lastAt, r.hasLast = now, true
	if r.n == maxBindingKeys {
		// Never: a pending key is the start of a binding of at most
		// maxBindingKeys keys, and this key would be past its end.
		r.clear()
	}
	r.pending[r.n] = pendingKey[S]{code: code, num: r.next, gap: gap, first: first, snap: snap}
	r.next++
	n := copy(r.window[:], r.pending[:r.n+1])
	return r.window[:n]
}

// fits reports whether a binding may start at key num: not inside a
// binding in force.
func (r *keyResolver[S, M, E]) fits(num int) bool {
	for _, s := range r.inForce[:r.nForce] {
		if num > s.start && num <= s.end {
			return false
		}
	}
	return true
}

// canGrow reports whether some suffix of w can still grow into a binding.
func (r *keyResolver[S, M, E]) canGrow(t *bindingTrie[E], w []pendingKey[S]) bool {
	for p := range w {
		if r.fits(w[p].num) && grows(t, w[p:]) {
			return true
		}
	}
	return false
}

// keep keeps as pending the keys of w from the first one from which they
// can still grow into a binding, and forgets the bindings in force that
// start before it.
func (r *keyResolver[S, M, E]) keep(t *bindingTrie[E], w []pendingKey[S]) {
	for p := range w {
		if r.fits(w[p].num) && grows(t, w[p:]) {
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

// fireLongest runs the longest binding of t that ends the keys of w, whose
// times fit and which does not decline. fire restores the snapshot of the
// binding's first key and runs it, or reports that it declined. It reports
// whether a binding ran, and what fire returned.
func (r *keyResolver[S, M, E]) fireLongest(t *bindingTrie[E], w []pendingKey[S], fire func(e E, w []pendingKey[S]) (bool, bool, error)) (bool, bool, error) {
	var none E
	for p := range w {
		n := walkKeys(t, w[p:])
		if n < 0 {
			continue
		}
		e := t.nodes[n].entry
		if e == none || !timed(e, w[p:]) || !r.fits(w[p].num) {
			continue
		}
		ran, end, err := fire(e, w[p:])
		if !ran {
			continue
		}
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
			r.keep(t, w)
		} else {
			r.clear()
		}
		return true, end, err
	}
	return false, false, nil
}

// used reports whether a binding of t starts with key, or continues the
// pending keys with it.
func (r *keyResolver[S, M, E]) used(t *bindingTrie[E], key KeyCode) bool {
	var w [maxBindingKeys]pendingKey[S]
	n := copy(w[:maxBindingKeys-1], r.pending[:r.n])
	w[n] = pendingKey[S]{code: key}
	for p := 0; p <= n; p++ {
		if walkKeys(t, w[p:n+1]) >= 0 {
			return true
		}
	}
	return false
}

// pendingCodes are the keys pending.
func (r *keyResolver[S, M, E]) pendingCodes() []KeyCode {
	codes := make([]KeyCode, r.n)
	for i := range codes {
		codes[i] = r.pending[i].code
	}
	return codes
}

// ----- Keymaps -----

// bindingCommon is what a binding of either keymap holds besides its
// action. M is the keymap's mode type, whose first member is the mode a
// record without one gets.
type bindingCommon[M ~uint8] struct {
	// record is the record the add builtin was given, with its mode filled
	// in, which the list builtin gives back.
	record *MShellDict
	keys   []KeyCode
	mode   M
	// The timing fields, zero when not given.
	within, idleBefore time.Duration
	description        string
	// invalid says why the record can never fire, or is "". inert is that,
	// or the conflict with a newer binding that made it inert.
	invalid string
	inert   string
}

func (b *bindingCommon[M]) bindingRecord() *MShellDict { return b.record }
func (b *bindingCommon[M]) bindingKeys() []KeyCode     { return b.keys }
func (b *bindingCommon[M]) bindingMode() uint8         { return uint8(b.mode) }
func (b *bindingCommon[M]) bindingTiming() (time.Duration, time.Duration) {
	return b.within, b.idleBefore
}
func (b *bindingCommon[M]) inertReason() *string  { return &b.inert }
func (b *bindingCommon[M]) invalidReason() string { return b.invalid }

// describeIn names a binding in a message: its keys, and its mode, named by
// modeMembers, when that is not the first.
func (b *bindingCommon[M]) describeIn(modeMembers []string) string {
	s := "[" + keysSource(b.keys) + "]"
	if b.mode != 0 {
		s += " in " + modeMembers[b.mode]
	}
	return s
}

// parseBindingFields reads the fields every binding record has. The
// action's payload is given back unread as ops, or as fn when the action is
// the member that holds a quotation. It fails only on a value the type
// checker refuses; a record that can never fire gets a reason in invalid.
func parseBindingFields[M ~uint8](state *EvalState, record *MShellDict, actionEnum, modeEnum string, modeMembers []string) (b bindingCommon[M], ops MShellObject, fn *MShellQuotation, err error) {
	b.record = record

	keys, ok := record.Items["keys"].(*MShellList)
	if !ok {
		return b, nil, nil, fmt.Errorf("the 'keys' field must be a list of Key")
	}
	for _, item := range keys.Items {
		k, err := keyCodeOf(item)
		if err != nil {
			return b, nil, nil, err
		}
		b.keys = append(b.keys, k)
	}

	action := enumField(record, "action", actionEnum)
	if action == nil || len(action.Payload) != 1 {
		return b, nil, nil, fmt.Errorf("the 'action' field must be of type %s", actionEnum)
	}
	// The first member holds the ops, the second a quotation.
	if action.MemberIndex == 0 {
		ops = action.Payload[0]
	} else if fn, ok = action.Payload[0].(*MShellQuotation); !ok {
		return b, nil, nil, fmt.Errorf("'%s' must hold a quotation", action.Member)
	}

	if _, given := record.Items["mode"]; !given {
		record.Items["mode"] = state.builtinEnumValue(modeMembers[0])
	}
	mode := enumField(record, "mode", modeEnum)
	if mode == nil {
		return b, nil, nil, fmt.Errorf("the 'mode' field must be an %s", modeEnum)
	}
	b.mode = M(mode.MemberIndex)

	timing := []struct {
		key string
		to  *time.Duration
	}{{"within", &b.within}, {"idleBefore", &b.idleBefore}}
	for _, f := range timing {
		v, given := record.Items[f.key]
		if !given {
			continue
		}
		n, ok := v.(MShellInt)
		if !ok {
			return b, nil, nil, fmt.Errorf("the '%s' field must be an int", f.key)
		}
		if n.Value <= 0 && b.invalid == "" {
			b.invalid = fmt.Sprintf("'%s' is %d; a time must be more than 0 ms", f.key, n.Value)
		}
		*f.to = time.Duration(n.Value) * time.Millisecond
	}
	if v, given := record.Items["description"]; given {
		s, ok := v.(MShellString)
		if !ok {
			return b, nil, nil, fmt.Errorf("the 'description' field must be a str")
		}
		b.description = s.Content
	}

	switch {
	case b.invalid != "":
	case len(b.keys) == 0:
		b.invalid = "it has no keys"
	case len(b.keys) > maxBindingKeys:
		b.invalid = fmt.Sprintf("it has %d keys, and a binding has at most %d", len(b.keys), maxBindingKeys)
	}
	return b, ops, fn, nil
}

// defaultRecord is the record of a default binding, which the list builtin
// gives back.
func (state *EvalState) defaultRecord(keys []KeyCode, action MShellObject, mode, description string) *MShellDict {
	keyList := NewList(len(keys))
	for i, k := range keys {
		keyList.Items[i] = state.keyValue(k)
	}
	record := NewDict()
	record.Items["keys"] = keyList
	record.Items["action"] = action
	record.Items["mode"] = state.builtinEnumValue(mode)
	if description != "" {
		record.Items["description"] = MShellString{description}
	}
	return record
}

// keymapKind is what differs between the shell's keymap and the file
// manager's.
type keymapKind[E keyBinding] struct {
	// builtin starts the names of the keymap's builtins: "binding" for
	// bindingAdd. record is the record bindingAdd takes, as a message names
	// it: "a Binding".
	builtin, record string
	modeCount       int
	parse           func(state *EvalState, record *MShellDict) (E, error)
	defaults        func(state *EvalState) []E
}

// keymap is a table of bindings: the entries, oldest first, the defaults
// first, and each mode's trie of the bindings in force.
type keymap[E keyBinding] struct {
	kind    *keymapKind[E]
	entries []E
	tries   []bindingTrie[E]
}

// ready sets the keymap up as kind on first use, with its defaults added
// before any binding a file adds, so a user's binding is always the newer
// one.
func (k *keymap[E]) ready(state *EvalState, kind *keymapKind[E]) *keymap[E] {
	if k.kind == nil {
		k.kind = kind
		state.ensureBuiltinEnums()
		k.tries = make([]bindingTrie[E], kind.modeCount)
		k.entries = kind.defaults(state)
		k.recompute()
	}
	return k
}

// recompute works out which bindings are inert, and builds each mode's
// trie of the bindings in force.
func (k *keymap[E]) recompute() {
	recomputeInert(k.entries)
	for m := range k.tries {
		k.tries[m].build(k.entries, uint8(m))
	}
}

// add adds e as the newest binding, replacing one with the same keys in the
// same mode, and notes each binding it makes inert for the interactive
// shell to print.
func (k *keymap[E]) add(state *EvalState, e E) {
	k.entries = slices.DeleteFunc(k.entries, func(o E) bool {
		return o.bindingMode() == e.bindingMode() && slices.Equal(o.bindingKeys(), e.bindingKeys())
	})
	wasInert := make(map[E]bool, len(k.entries))
	for _, o := range k.entries {
		wasInert[o] = *o.inertReason() != ""
	}
	k.entries = append(k.entries, e)
	k.recompute()
	for _, o := range k.entries {
		if *o.inertReason() != "" && !wasInert[o] {
			state.AbbrWarnings = append(state.AbbrWarnings, "Binding "+o.describe()+" never fires: "+*o.inertReason()+".")
		}
	}
}

// evalAdd runs the add builtin: (Binding -- ).
func (k *keymap[E]) evalAdd(state *EvalState, t *Token, stack *MShellStack) *EvalResult {
	name := k.kind.builtin + "Add"
	obj, err := stack.Pop()
	if err != nil {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, name))
	}
	dict, ok := obj.(*MShellDict)
	if !ok {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: '%s' takes %s, found a %s (%s).\n", t.Line, t.Column, name, k.kind.record, obj.TypeName(), obj.DebugString()))
	}
	copied, err := DeepCopy(dict)
	if err != nil {
		return state.checkedPtr(fmt.Sprintf("%d:%d: '%s': %s\n", t.Line, t.Column, name, err.Error()))
	}
	e, err := k.kind.parse(state, copied.(*MShellDict))
	if err != nil {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: '%s' takes %s: %s.\n", t.Line, t.Column, name, k.kind.record, err.Error()))
	}
	k.add(state, e)
	return nil
}

// evalErase runs the erase builtin: ([Key] -- ). It removes the bindings
// with those keys in every mode, defaults included.
func (k *keymap[E]) evalErase(state *EvalState, t *Token, stack *MShellStack) *EvalResult {
	name := k.kind.builtin + "Erase"
	obj, err := stack.Pop()
	if err != nil {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: Cannot do '%s' operation on an empty stack.\n", t.Line, t.Column, name))
	}
	list, ok := obj.(*MShellList)
	if !ok {
		return state.mismatchPtr(fmt.Sprintf("%d:%d: '%s' takes a list of Key, found a %s (%s).\n", t.Line, t.Column, name, obj.TypeName(), obj.DebugString()))
	}
	keys := make([]KeyCode, len(list.Items))
	for i, item := range list.Items {
		key, err := keyCodeOf(item)
		if err != nil {
			return state.mismatchPtr(fmt.Sprintf("%d:%d: '%s': %s.\n", t.Line, t.Column, name, err.Error()))
		}
		keys[i] = key
	}
	k.entries = slices.DeleteFunc(k.entries, func(e E) bool { return slices.Equal(e.bindingKeys(), keys) })
	k.recompute()
	return nil
}

// evalList runs the list builtin: ( -- new [Binding]), oldest first, so the
// defaults come first.
func (k *keymap[E]) evalList(state *EvalState, t *Token, stack *MShellStack) *EvalResult {
	list := NewList(len(k.entries))
	for i, e := range k.entries {
		copied, err := DeepCopy(e.bindingRecord())
		if err != nil {
			return state.checkedPtr(fmt.Sprintf("%d:%d: '%s': %s\n", t.Line, t.Column, k.kind.builtin+"List", err.Error()))
		}
		list.Items[i] = copied
	}
	stack.Push(list)
	return nil
}
