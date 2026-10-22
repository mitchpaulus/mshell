package main

// The key resolver (ai/cli_bindings.typ, section 6; ai/fm-bindings.typ,
// section 6): the pending keys and the snapshots taken before them, each
// mode's trie of the bindings in force, timing, and the conflict rule. The
// shell's editor (Bindings.go) and the file manager (FmBindings.go) each
// keep a table of bindings and a resolver. What a snapshot holds, what a
// key does on its own, and how a program runs are theirs.

import (
	"slices"
	"time"
)

// maxBindingKeys is the most keys a binding may have, and so the most a
// resolver ever keeps pending.
const maxBindingKeys = 8

// keyBinding is what the resolver needs of a binding.
type keyBinding interface {
	comparable
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

// addToKeymap adds e as the newest of entries, replacing one with the same
// keys in the same mode. recompute works out the bindings in force again;
// warn is given a line for each binding e makes inert.
func addToKeymap[E keyBinding](entries []E, e E, recompute func([]E), warn func(string)) []E {
	entries = slices.DeleteFunc(entries, func(o E) bool {
		return o.bindingMode() == e.bindingMode() && slices.Equal(o.bindingKeys(), e.bindingKeys())
	})
	wasInert := make(map[E]bool, len(entries))
	for _, o := range entries {
		wasInert[o] = *o.inertReason() != ""
	}
	entries = append(entries, e)
	recompute(entries)
	for _, o := range entries {
		if *o.inertReason() != "" && !wasInert[o] {
			warn("Binding " + o.describe() + " never fires: " + *o.inertReason() + ".")
		}
	}
	return entries
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
