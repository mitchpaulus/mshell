package main

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
	"time"
)

// Bindings.go and Keys.go read the members of the binding enums by their
// order in builtinDeclSource; the tables there must follow it.
func TestBindingEnumsMatch(t *testing.T) {
	enums := builtinEnums(t)
	check := func(name string, have []string) {
		t.Helper()
		d, ok := enums[name]
		if !ok {
			t.Fatalf("builtinDeclSource does not declare %s", name)
		}
		if !slices.Equal(d.Members, have) {
			t.Errorf("%s: the Go table is %v, the declaration %v", name, have, d.Members)
		}
	}
	check("EditOp", editOpMembers[:])
	check("EditMode", editModeMembers[:])
	check("NamedKey", namedKeyMembers)
	var mods []string
	for _, m := range enums["ModifiedKey"].Members {
		mods = append(mods, m)
	}
	for _, m := range keyModifiers {
		if !slices.Contains(mods, m.member) {
			t.Errorf("ModifiedKey has no member %s", m.member)
		}
	}
	if len(mods) != len(keyModifiers) {
		t.Errorf("ModifiedKey has %d members, keyModifiers %d", len(mods), len(keyModifiers))
	}
	if n := len(namedKeyMembers); n != namedKeyCount {
		t.Errorf("%d named keys, namedKeyCount is %d", n, namedKeyCount)
	}
}

// Every default binding is in force: none conflicts with another.
func TestDefaultBindingsInForce(t *testing.T) {
	var ev EvalState
	for _, e := range ev.shellKeymap().entries {
		if e.inert != "" {
			t.Errorf("default binding %s is inert: %s", e.describe(), e.inert)
		}
	}
}

// Every key normalizes to itself after a round trip through its Key value.
func TestKeyValueRoundTrip(t *testing.T) {
	var ev EvalState
	keys := []KeyCode{
		'a', 'A', 'é', ' ', '😀', NamedEnter, NamedF(35),
		'x' | KeyModCtrl, 'r' | KeyModAlt | KeyModShift, NamedLeft | KeyModAlt | KeyModCtrl,
		';' | KeyModShift, NamedTab | KeyModShift, 'q' | KeyModSuper,
	}
	for _, k := range keys {
		got, err := keyCodeOf(ev.keyValue(k))
		if err != nil || got != k {
			t.Errorf("%s: round trip gave %s, %v", keySource(k), keySource(got), err)
		}
	}
	// Two spellings of a key are the same key.
	same := [][2]KeyCode{
		{'j' | KeyModShift, 'J'},
		{'W' | KeyModAlt, 'w' | KeyModAlt | KeyModShift},
		{'J' | KeyModShift, 'J'},
	}
	for _, p := range same {
		if a, b := normalizeKey(p[0]), normalizeKey(p[1]); a != b {
			t.Errorf("%s and %s differ: %s, %s", keySource(p[0]), keySource(p[1]), keySource(a), keySource(b))
		}
	}
}

func TestDecodeCsiKey(t *testing.T) {
	tests := []struct {
		final  byte
		params string
		want   KeyCode
		ok     bool
	}{
		{'C', "1;5", NamedRight | KeyModCtrl, true},
		{'H', "1;2", NamedHome | KeyModShift, true},
		{'~', "3;3", NamedDelete | KeyModAlt, true},
		{'~', "15", NamedF(5), true},
		{'~', "24;5", NamedF(12) | KeyModCtrl, true},
		{'u', "27", NamedEscape, true},
		{'u', "97;5", 'a' | KeyModCtrl, true},
		{'u', "97;6", 'a' | KeyModCtrl | KeyModShift, true},
		{'u', "1094::119;5", 'w' | KeyModCtrl, true}, // Ctrl and the key where w is, on a Cyrillic layout
		{'u', "57376", NamedF(13), true},
		{'u', "13;3", NamedEnter | KeyModAlt, true},
		{'u', "97;69", 'a' | KeyModCtrl, true}, // Caps Lock is not a modifier
		{'R', "3;5", 0, false},                 // a cursor report
		{'u', "?1", 0, false},                  // a reply to the protocol query
		{'u', "57441", 0, false},               // a modifier key alone
	}
	for _, tt := range tests {
		got, ok := decodeCsiKey(tt.final, tt.params)
		if ok != tt.ok || got != tt.want {
			t.Errorf("CSI %s %c: got %s, %v; want %s, %v", tt.params, tt.final, keySource(got), ok, keySource(tt.want), tt.ok)
		}
	}
}

// ESC and a key in one read, when Alt and the key has no binding, is
// Escape and then the key, and Escape is not bound. ESC b is Alt-B, which
// is bound.
func TestEscapeThenUnboundKey(t *testing.T) {
	state := &TermState{stdInFd: -1}
	in := []byte("\x1ba\x1bb")
	state.stdInState = &StdinReaderState{array: in, n: len(in)}
	for range 2 {
		token, err := state.readInputToken()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := state.HandleToken(token); err != nil {
			t.Fatal(err)
		}
	}
	if string(state.currentCommand) != "a" || state.index != 0 {
		t.Errorf("line %q, cursor %d; want \"a\", 0", state.currentCommand, state.index)
	}
}

// ----- The oracle test (ai/cli_bindings.typ, section 13) -----

// oracleEvent is a key or a paste, with the gap since the event before it.
type oracleEvent struct {
	key   KeyCode // 0 for a paste
	gap   time.Duration
	paste bool
}

// oracleBinding is a binding of the random keymap: a pure one inserts its
// text, an effect shows a message and keeps the line.
type oracleBinding struct {
	keys       []KeyCode
	pure       bool
	text       string
	within, ib time.Duration
}

type oracleUse struct{ b, s, e int }

// oracleResult is the line and the effects that ran, each with the line it
// ran on.
type oracleResult struct {
	line string
	log  []string
}

// definition is section 6.1 written plainly: at each key, the longest
// binding that ends there, on keys with no paste among them, whose times
// fit, and that does not start inside a binding in force, replaces the
// ones in force that start at or after its first key. Then each key a
// binding covers does the binding at its last key, on the line before its
// first, and every other event does its own action.
func definition(bs []oracleBinding, active []bool, events []oracleEvent, n int) oracleResult {
	var used []oracleUse
	for i := 0; i < n; i++ {
		if events[i].paste {
			continue
		}
		best := oracleUse{b: -1}
		for b, ob := range bs {
			s := i - len(ob.keys) + 1
			if !active[b] || s < 0 {
				continue
			}
			ok := true
			for j := s; j <= i && ok; j++ {
				ok = !events[j].paste && events[j].key == ob.keys[j-s]
			}
			for j := s + 1; j <= i && ok && ob.within > 0; j++ {
				ok = events[j].gap <= ob.within
			}
			if ok && ob.ib > 0 && s > 0 {
				ok = events[s].gap >= ob.ib
			}
			for _, u := range used {
				ok = ok && (s > u.e || s <= u.s)
			}
			if ok && (best.b < 0 || s < best.s) {
				best = oracleUse{b: b, s: s, e: i}
			}
		}
		if best.b >= 0 {
			used = slices.DeleteFunc(used, func(u oracleUse) bool { return u.s >= best.s })
			used = append(used, best)
		}
	}
	var r oracleResult
	for i := 0; i < n; i++ {
		covered := false
		for _, u := range used {
			if u.e == i {
				if ob := bs[u.b]; ob.pure {
					r.line += ob.text
				} else {
					r.log = append(r.log, ob.text+"@"+r.line)
				}
				covered = true
			} else if u.s <= i && i < u.e {
				covered = true
			}
		}
		if covered {
			continue
		}
		if events[i].paste {
			r.line += "p"
		} else if ch, ok := events[i].key.text(); ok {
			r.line += string(ch)
		}
	}
	return r
}

// TestResolveMatchesDefinition runs the editor's resolver on random keymaps
// and random keys, and checks after every event that the line and the
// effects are the definition's: KeyResolution.tla's StepMatches, on more
// keymaps than the model has.
func TestResolveMatchesDefinition(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	alphabet := []KeyCode{'a', 'b', 'x' | KeyModCtrl}
	gaps := []time.Duration{0, 2 * time.Millisecond}
	limits := []time.Duration{0, 0, time.Millisecond}
	for trial := range 4000 {
		var bs []oracleBinding
		for i := range 1 + rng.Intn(6) {
			keys := make([]KeyCode, 1+rng.Intn(3))
			for j := range keys {
				keys[j] = alphabet[rng.Intn(len(alphabet))]
			}
			bs = append(bs, oracleBinding{
				keys: keys, pure: rng.Intn(2) == 0, text: fmt.Sprintf("<%d>", i),
				within: limits[rng.Intn(len(limits))], ib: limits[rng.Intn(len(limits))],
			})
		}

		state := &TermState{stdInFd: -1}
		ev := &state.evalState
		km := ev.shellKeymap()
		km.entries = nil
		for _, ob := range bs {
			e := &bindingEntry{bindingCommon: bindingCommon[editMode]{keys: ob.keys, within: ob.within, idleBefore: ob.ib}}
			if ob.pure {
				e.ops = []editOp{{kind: opInsert, a: ob.text}}
			} else {
				e.ops = []editOp{{kind: opMessage, a: "E" + ob.text}}
			}
			km.entries = slices.DeleteFunc(km.entries, func(o *bindingEntry) bool { return slices.Equal(o.keys, e.keys) })
			km.entries = append(km.entries, e)
		}
		km.recompute()
		// The keymap after replacements, and which bindings are in force.
		bs = bs[:0]
		var active []bool
		for _, e := range km.entries {
			ob := oracleBinding{keys: e.keys, pure: e.pure(), within: e.within, ib: e.idleBefore}
			ob.text = e.ops[0].a
			if !ob.pure {
				ob.text = strings.TrimPrefix(ob.text, "E")
			}
			bs = append(bs, ob)
			active = append(active, e.inert == "")
		}

		events := make([]oracleEvent, rng.Intn(9))
		for i := range events {
			events[i].gap = gaps[rng.Intn(len(gaps))]
			if rng.Intn(8) == 0 {
				events[i].paste = true
			} else {
				events[i].key = alphabet[rng.Intn(len(alphabet))]
			}
		}

		now := time.Unix(1_000_000, 0)
		var log []string
		for i, e := range events {
			now = now.Add(e.gap)
			state.abbrMessage = ""
			if e.paste {
				state.keys.event(now)
				state.replaceText("p", state.index, state.index)
			} else if _, err := state.resolveKey(e.key, now); err != nil {
				t.Fatal(err)
			}
			if m := state.abbrMessage; strings.HasPrefix(m, "E") {
				log = append(log, strings.TrimPrefix(m, "E")+"@"+string(state.currentCommand))
			}
			want := definition(bs, active, events, i+1)
			if string(state.currentCommand) != want.line || !slices.Equal(log, want.log) {
				t.Fatalf("trial %d, after event %d of %s\nkeymap %s\nline %q, effects %v\nwant %q, %v",
					trial, i, describeEvents(events), describeKeymap(bs, active), state.currentCommand, log, want.line, want.log)
			}
		}
	}
}

func describeEvents(events []oracleEvent) string {
	var parts []string
	for _, e := range events {
		what := "paste"
		if !e.paste {
			what = keySource(e.key)
		}
		parts = append(parts, fmt.Sprintf("+%v %s", e.gap, what))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func describeKeymap(bs []oracleBinding, active []bool) string {
	var parts []string
	for i, b := range bs {
		kind := "effect"
		if b.pure {
			kind = "pure"
		}
		state := ""
		if !active[i] {
			state = " inert"
		}
		parts = append(parts, fmt.Sprintf("[%s] %s %s within %v ib %v%s", keysSource(b.keys), kind, b.text, b.within, b.ib, state))
	}
	return strings.Join(parts, "; ")
}
