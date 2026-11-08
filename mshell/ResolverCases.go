package main

// Resolver cases: the reference the GUI frontend's Rust key resolver is
// checked against (gui/msh-editor, ai/gui-terminal.typ "Keeping it in
// sync"). Hidden: msh --resolver-cases SEED COUNT prints COUNT cases as JSON
// lines.
//
// Each case is a random keymap, a starting line and cursor, and a sequence
// of keys and pastes with the times between them, chosen to land on both
// sides of each binding's within and idleBefore. The events go through the
// interactive editor's own path (handleTokenAt, resolveKey) and the
// expected line, cursor, pending keys and bindings fired are recorded
// after each one.
//
// The keymaps use the ops that need only the line, and editMessage as the
// effect, since it changes nothing a case checks but is not pure. A
// bindingFunc's quotation returns fixed ops or declines; in the GUI the
// engine answers it.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"time"
)

type resolverCaseOp struct {
	Op string `json:"op"`
	A  string `json:"a,omitempty"`
	B  string `json:"b,omitempty"`
}

type resolverCaseBinding struct {
	Keys       []uint32         `json:"keys"`
	WithinMs   int              `json:"within_ms"`
	IdleMs     int              `json:"idle_before_ms"`
	Ops        []resolverCaseOp `json:"ops,omitempty"`
	Func       bool             `json:"func,omitempty"`
	FuncResult []resolverCaseOp `json:"func_result,omitempty"` // with func: the ops it returns, or none: it declines
	Declines   bool             `json:"declines,omitempty"`
	// Active is whether the binding is in force: the engine works out
	// which bindings conflict, and sends the frontend only these.
	Active bool `json:"active"`
}

type resolverCaseEvent struct {
	AtMs  int    `json:"at_ms"`
	Key   uint32 `json:"key,omitempty"`
	EscOf uint32 `json:"esc_of,omitempty"` // the key came as ESC and EscOf in one read
	Paste string `json:"paste,omitempty"`
	IsPaste bool `json:"is_paste,omitempty"`
}

type resolverCaseState struct {
	Line    string   `json:"line"`
	Cursor  int      `json:"cursor"`
	Pending []uint32 `json:"pending"`
	Fired   []int    `json:"fired"`
}

type resolverCase struct {
	Bindings []resolverCaseBinding `json:"bindings"`
	Line     string                `json:"line"`
	Cursor   int                   `json:"cursor"`
	Events   []resolverCaseEvent   `json:"events"`
	Expect   []resolverCaseState   `json:"expect"`
}

// The keys cases use: few, so bindings share prefixes and conflict.
var resolverCaseKeys = []KeyCode{
	'a', 'b', 'j', 'k', ' ', ';', 'é', '世',
	'x' | KeyModCtrl, 'x' | KeyModAlt, 'j' | KeyModAlt,
	NamedLeft, NamedEnter, NamedEscape, NamedBackspace,
}

var resolverCaseTexts = []string{"x", "é", "é", "👍", " ", "ab", "世", "a b", ""}

var resolverCaseLines = []string{"", "abc", "foo bar", "é世 x", "éé", "a  b", "👨‍👩‍👧 z", "x y z w", "🇩🇪🇫🇷"}

// The times between events, around the bindings' times.
var resolverCaseGaps = []int{0, 1, 10, 49, 50, 51, 99, 100, 101, 199, 200, 201, 299, 300, 301, 1000}

var resolverCaseTimes = []int{0, 0, 0, 50, 100, 200, 300}

func resolverCaseRandomOp(rng *rand.Rand, effects bool) editOp {
	kinds := []editOpKind{opInsert, opSelfInsert, opSetLine, opLeft, opRight, opWordLeft, opWordRight,
		opLineStart, opLineEnd, opBackspace, opDelete, opDeleteWordLeft, opDeleteToStart, opDeleteToEnd}
	if effects {
		kinds = append(kinds, opMessage)
	}
	op := editOp{kind: kinds[rng.IntN(len(kinds))]}
	switch op.kind {
	case opInsert, opMessage:
		op.a = resolverCaseTexts[rng.IntN(len(resolverCaseTexts))]
	case opSetLine:
		op.a = resolverCaseTexts[rng.IntN(len(resolverCaseTexts))]
		op.b = resolverCaseTexts[rng.IntN(len(resolverCaseTexts))]
	}
	return op
}

func resolverCaseOps(rng *rand.Rand, effects bool) []editOp {
	ops := make([]editOp, 1+rng.IntN(3))
	for i := range ops {
		ops[i] = resolverCaseRandomOp(rng, effects)
	}
	return ops
}

func resolverCaseOpsJSON(ops []editOp) []resolverCaseOp {
	out := make([]resolverCaseOp, len(ops))
	for i, op := range ops {
		out[i] = resolverCaseOp{Op: editOpMembers[op.kind], A: op.a, B: op.b}
	}
	return out
}

// resolverCaseOpsSource writes ops as mshell: [editLeft 'x' editInsert].
func resolverCaseOpsSource(ops []editOp) string {
	parts := make([]string, len(ops))
	for i, op := range ops {
		switch op.kind {
		case opInsert, opMessage:
			parts[i] = "'" + op.a + "' " + editOpMembers[op.kind]
		case opSetLine:
			parts[i] = "'" + op.a + "' '" + op.b + "' " + editOpMembers[op.kind]
		default:
			parts[i] = editOpMembers[op.kind]
		}
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func runResolverCases(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: msh --resolver-cases SEED COUNT")
		return 1
	}
	seed, err1 := strconv.ParseUint(args[0], 10, 64)
	count, err2 := strconv.Atoi(args[1])
	if err1 != nil || err2 != nil {
		fmt.Fprintln(os.Stderr, "usage: msh --resolver-cases SEED COUNT")
		return 1
	}
	rng := rand.New(rand.NewPCG(seed, 0x6d7368))

	PrebuildCoreBuiltins()
	state := newSessionState(0, 80, 24, nil)
	state.evalState.ensureBuiltinEnums()
	var fired []int
	var firedIndex map[*bindingEntry]int
	bindingFiredHook = func(e *bindingEntry) { fired = append(fired, firedIndex[e]) }

	out := bufio.NewWriterSize(os.Stdout, 1<<20)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	for range count {
		c := resolverCase{}
		var entries []*bindingEntry
		firedIndex = map[*bindingEntry]int{}
		seen := map[string]bool{}
		for range 1 + rng.IntN(10) {
			keys := make([]KeyCode, 1+rng.IntN(4))
			for i := range keys {
				keys[i] = resolverCaseKeys[rng.IntN(len(resolverCaseKeys))]
			}
			// One binding per key sequence, as bindingAdd keeps.
			if name := fmt.Sprint(keys); seen[name] {
				continue
			} else {
				seen[name] = true
			}
			e := &bindingEntry{bindingCommon: bindingCommon[editMode]{keys: keys, mode: modeInsert,
				within:     time.Duration(resolverCaseTimes[rng.IntN(len(resolverCaseTimes))]) * time.Millisecond,
				idleBefore: time.Duration(resolverCaseTimes[rng.IntN(len(resolverCaseTimes))]) * time.Millisecond}}
			b := resolverCaseBinding{WithinMs: int(e.within / time.Millisecond), IdleMs: int(e.idleBefore / time.Millisecond)}
			for _, k := range keys {
				b.Keys = append(b.Keys, uint32(k))
			}
			if rng.IntN(4) == 0 {
				b.Func = true
				if rng.IntN(3) == 0 {
					b.Declines = true
					e.fn = defaultQuote("(drop none)")
				} else {
					result := resolverCaseOps(rng, rng.IntN(4) == 0)
					b.FuncResult = resolverCaseOpsJSON(result)
					e.fn = defaultQuote("(drop " + resolverCaseOpsSource(result) + " just)")
				}
			} else {
				e.ops = resolverCaseOps(rng, rng.IntN(4) == 0)
				b.Ops = resolverCaseOpsJSON(e.ops)
			}
			firedIndex[e] = len(entries)
			entries = append(entries, e)
			c.Bindings = append(c.Bindings, b)
		}
		km := keymap[*bindingEntry]{kind: &shellKeymapKind, entries: entries, tries: make([]bindingTrie[*bindingEntry], editModeCount)}
		km.recompute()
		for i, e := range entries {
			c.Bindings[i].Active = *e.inertReason() == ""
		}
		state.evalState.bindings = km

		c.Line = resolverCaseLines[rng.IntN(len(resolverCaseLines))]
		state.currentCommand = SourceText(c.Line)
		state.index = graphemeCeil(state.currentCommand, ByteOffset(rng.IntN(len(c.Line)+1)))
		c.Cursor = int(state.index)
		state.keys = keyResolver[editSnapshot, editMode, *bindingEntry]{}
		state.abbrCursor, state.abbrMessage, state.abbrProblem = abbrExpansion{}, "", ""

		at := 0
		base := time.Unix(1_000_000_000, 0)
		for range 1 + rng.IntN(12) {
			at += resolverCaseGaps[rng.IntN(len(resolverCaseGaps))]
			ev := resolverCaseEvent{AtMs: at}
			var token TerminalToken
			switch r := rng.IntN(20); {
			case r == 0:
				ev.IsPaste = true
				ev.Paste = resolverCaseTexts[rng.IntN(len(resolverCaseTexts))]
				token = PasteToken{Text: ev.Paste}
			case r <= 2:
				// Alt and a key, as the legacy encoding sends it: ESC then the key.
				k := resolverCaseKeys[rng.IntN(8)]
				ev.Key, ev.EscOf = uint32(k|KeyModAlt), uint32(k)
				token = KeyToken{Code: k | KeyModAlt, EscOf: k}
			default:
				k := resolverCaseKeys[rng.IntN(len(resolverCaseKeys))]
				ev.Key = uint32(k)
				token = KeyToken{Code: k}
			}
			fired = fired[:0]
			if _, err := state.handleTokenAt(token, base.Add(time.Duration(at)*time.Millisecond)); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			st := resolverCaseState{Line: string(state.currentCommand), Cursor: int(state.index), Pending: []uint32{}, Fired: append([]int{}, fired...)}
			for _, k := range state.keys.pendingCodes() {
				st.Pending = append(st.Pending, uint32(k))
			}
			c.Events = append(c.Events, ev)
			c.Expect = append(c.Expect, st)
		}
		if err := enc.Encode(c); err != nil {
			return 1
		}
	}
	if err := out.Flush(); err != nil {
		return 1
	}
	return 0
}
