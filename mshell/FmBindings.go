package main

// Key bindings in the file manager (ai/fm-bindings.typ). A binding maps a
// sequence of keys to a program for the file manager: a list of FmOp and
// EditOp values, or a quote that returns one. The table is the shell's
// (Bindings.go) over other ops and modes, and the same resolver
// (KeyResolver.go) reads it. The defaults are bindings like any other,
// added before the init file runs. Their types are declared in
// builtinDeclSource.

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// fmMode is a keymap of the file manager: browsing, the prompt line, or
// the help screen.
type fmMode uint8

const (
	fmModeNormal fmMode = iota
	fmModeInsert
	fmModeHelp
	fmModeCount
)

var fmModeMembers = [fmModeCount]string{"fmModeNormal", "fmModeInsert", "fmModeHelp"}

// fmModeNames are the modes as the help screen and messages name them.
var fmModeNames = [fmModeCount]string{"browsing", "the prompt line", "the help screen"}

// fmOpKind is an FmOp member, in the order of the declaration. The moves
// come first: they change only the cursor, and can be undone by restoring
// it.
type fmOpKind uint8

const (
	fmOpDown fmOpKind = iota
	fmOpUp
	fmOpTop
	fmOpBottom
	fmOpDownBy
	fmOpUpBy
	fmOpSearchNext
	fmOpSearchPrev
	fmOpCursorTo
	// Normal mode, effects.
	fmOpOpen
	fmOpParent
	fmOpCd
	fmOpReload
	fmOpOpenEditor
	fmOpToggle
	fmOpSelectionInvert
	fmOpSelectionClear
	fmOpPrompt
	fmOpCut
	fmOpCopy
	fmOpPaste
	fmOpClipboardClear
	fmOpYankName
	fmOpYankPath
	fmOpYankGitPath
	fmOpTrash
	fmOpBookmarkSet
	fmOpBookmarkGo
	fmOpHelp
	// Insert mode, effects.
	fmOpLineAccept
	fmOpLineCancel
	// Help mode.
	fmOpHelpClose
	// Any mode.
	fmOpRun
	fmOpRunWait
	fmOpRunOps
	fmOpMessage
	fmOpQuit
	fmOpQuitNoCd
	fmOpCount
	// fmOpEdit is an EditOp in a program: a line edit.
	fmOpEdit = fmOpCount
)

var fmOpMembers = [fmOpCount]string{
	"fmDown", "fmUp", "fmTop", "fmBottom", "fmDownBy", "fmUpBy",
	"fmSearchNext", "fmSearchPrev", "fmCursorTo",
	"fmOpen", "fmParent", "fmCd", "fmReload", "fmOpenEditor",
	"fmToggle", "fmSelectionInvert", "fmSelectionClear",
	"fmPrompt",
	"fmCut", "fmCopy", "fmPaste", "fmClipboardClear",
	"fmYankName", "fmYankPath", "fmYankGitPath",
	"fmTrash", "fmBookmarkSet", "fmBookmarkGo", "fmHelp",
	"fmLineAccept", "fmLineCancel",
	"fmHelpClose",
	"fmRun", "fmRunWait", "fmRunOps",
	"fmMessage", "fmQuit", "fmQuitNoCd",
}

var fmOpKinds = sync.OnceValue(func() map[string]fmOpKind {
	m := make(map[string]fmOpKind, fmOpCount)
	for k, name := range fmOpMembers {
		m[name] = fmOpKind(k)
	}
	return m
})

// fmLineKind is what a prompt line is for: an FmPrompt member.
type fmLineKind uint8

const (
	fmLineSearch fmLineKind = iota
	fmLineRename
	fmLineAsk
)

var fmPromptMembers = []string{"fmPromptSearch", "fmPromptRename", "fmPromptAsk"}

// fmPrompt is an FmPrompt value: for fmPromptAsk, its label, the line to
// start from, and the quote Enter runs.
type fmPrompt struct {
	kind        fmLineKind
	label, text string
	quote       *MShellQuotation
}

// fmDistance is an FmDistance value: rows entries, or percent of the
// visible rows.
type fmDistance struct {
	isPercent bool
	rows      int
	percent   float64
}

// positive reports whether the distance moves at all.
func (d fmDistance) positive() bool {
	if d.isPercent {
		return d.percent > 0
	}
	return d.rows > 0
}

// fmOp is one op of a file manager program: an FmOp, or an EditOp.
type fmOp struct {
	kind   fmOpKind
	edit   editOp // for fmOpEdit
	a      string // fmCursorTo, fmCd, fmMessage
	dist   fmDistance
	prompt fmPrompt
	quote  *MShellQuotation // fmRun, fmRunWait, fmRunOps
}

// name is the op's member name.
func (op fmOp) name() string {
	if op.kind == fmOpEdit {
		return editOpMembers[op.edit.kind]
	}
	return fmOpMembers[op.kind]
}

// isLineEdit reports whether an EditOp kind is a pure edit of a line, which
// the prompt line can do.
func isLineEdit(k editOpKind) bool { return k <= opDeleteToEnd }

// isMove reports whether the op only moves the cursor, or scrolls the help.
func (op fmOp) isMove() bool { return op.kind <= fmOpUpBy }

// runsIn reports whether the op can run in mode m (section 5.2).
func (op fmOp) runsIn(m fmMode) bool {
	switch {
	case op.kind == fmOpEdit:
		return m == fmModeInsert && isLineEdit(op.edit.kind)
	case op.kind <= fmOpUpBy:
		return m == fmModeNormal || m == fmModeHelp
	case op.kind <= fmOpHelp:
		return m == fmModeNormal
	case op.kind <= fmOpLineCancel:
		return m == fmModeInsert
	case op.kind == fmOpHelpClose:
		return m == fmModeHelp
	}
	return true
}

// pureIn reports whether the op, run in mode m, changes only what a
// snapshot holds.
func (op fmOp) pureIn(m fmMode) bool {
	switch m {
	case fmModeNormal:
		return op.kind <= fmOpCursorTo
	case fmModeInsert:
		return op.kind == fmOpEdit && isLineEdit(op.edit.kind)
	}
	return op.isMove()
}

// modeAfter is the mode after the op runs in mode m, when it does what it
// is for.
func (op fmOp) modeAfter(m fmMode) fmMode {
	switch op.kind {
	case fmOpPrompt:
		return fmModeInsert
	case fmOpLineAccept, fmOpLineCancel, fmOpHelpClose:
		return fmModeNormal
	case fmOpHelp:
		return fmModeHelp
	}
	return m
}

// ends reports whether the op ends the file manager.
func (op fmOp) ends() bool { return op.kind == fmOpQuit || op.kind == fmOpQuitNoCd }

// parseFmPrompt reads an FmPrompt value.
func parseFmPrompt(v MShellObject) (fmPrompt, error) {
	e, ok := v.(*MShellEnum)
	if !ok || e.EnumName != "FmPrompt" {
		return fmPrompt{}, fmt.Errorf("'fmPrompt' must hold an FmPrompt")
	}
	p := fmPrompt{kind: fmLineKind(e.MemberIndex)}
	if p.kind == fmLineAsk {
		label, errA := payloadString(e, 0)
		text, errB := payloadString(e, 1)
		q, ok := e.Payload[len(e.Payload)-1].(*MShellQuotation)
		if errA != nil || errB != nil || len(e.Payload) != 3 || !ok {
			return fmPrompt{}, fmt.Errorf("'fmPromptAsk' must hold two str and a quotation")
		}
		p.label, p.text, p.quote = label, text, q
	}
	return p, nil
}

// parseFmDistance reads an FmDistance value.
func parseFmDistance(v MShellObject) (fmDistance, error) {
	e, ok := v.(*MShellEnum)
	if ok && e.EnumName == "FmDistance" && len(e.Payload) == 1 {
		switch p := e.Payload[0].(type) {
		case MShellInt:
			return fmDistance{rows: p.Value}, nil
		case MShellFloat:
			return fmDistance{isPercent: true, percent: p.Value}, nil
		}
	}
	return fmDistance{}, fmt.Errorf("a distance must be an FmDistance")
}

// parseFmOp reads an FmOp or EditOp value.
func parseFmOp(v MShellObject) (fmOp, error) {
	e, ok := v.(*MShellEnum)
	if ok && e.EnumName == "EditOp" {
		op, err := parseEditOp(e)
		return fmOp{kind: fmOpEdit, edit: op}, err
	}
	if !ok || e.EnumName != "FmOp" {
		return fmOp{}, fmt.Errorf("a program must be a list of FmOp and EditOp, found a %s (%s)", v.TypeName(), v.DebugString())
	}
	kind, ok := fmOpKinds()[e.Member]
	if !ok {
		return fmOp{}, fmt.Errorf("unknown FmOp '%s'", e.Member)
	}
	op := fmOp{kind: kind}
	switch kind {
	case fmOpCursorTo, fmOpCd, fmOpMessage:
		s, err := payloadString(e, 0)
		if err != nil {
			return fmOp{}, fmt.Errorf("'%s' must hold a str", e.Member)
		}
		op.a = s
	case fmOpDownBy, fmOpUpBy:
		if len(e.Payload) != 1 {
			return fmOp{}, fmt.Errorf("'%s' must hold an FmDistance", e.Member)
		}
		d, err := parseFmDistance(e.Payload[0])
		if err != nil {
			return fmOp{}, err
		}
		op.dist = d
	case fmOpPrompt:
		if len(e.Payload) != 1 {
			return fmOp{}, fmt.Errorf("'fmPrompt' must hold an FmPrompt")
		}
		p, err := parseFmPrompt(e.Payload[0])
		if err != nil {
			return fmOp{}, err
		}
		op.prompt = p
	case fmOpRun, fmOpRunWait, fmOpRunOps:
		q, ok := e.Payload[0].(*MShellQuotation)
		if len(e.Payload) != 1 || !ok {
			return fmOp{}, fmt.Errorf("'%s' must hold a quotation", e.Member)
		}
		op.quote = q
	}
	return op, nil
}

// parseFmOps reads a list of FmOp and EditOp values.
func parseFmOps(v MShellObject) ([]fmOp, error) {
	list, ok := v.(*MShellList)
	if !ok {
		return nil, fmt.Errorf("a program must be a list of FmOp and EditOp, found a %s (%s)", v.TypeName(), v.DebugString())
	}
	ops := make([]fmOp, len(list.Items))
	for i, item := range list.Items {
		op, err := parseFmOp(item)
		if err != nil {
			return nil, err
		}
		ops[i] = op
	}
	return ops, nil
}

// checkFmProgram follows the mode through a program that starts in mode,
// and says why it cannot run, or gives "" (section 5.3).
func checkFmProgram(ops []fmOp, mode fmMode) string {
	for i, op := range ops {
		if !op.runsIn(mode) {
			return fmt.Sprintf("'%s' cannot run in %s", op.name(), fmModeMembers[mode])
		}
		if (op.kind == fmOpDownBy || op.kind == fmOpUpBy) && !op.dist.positive() {
			return fmt.Sprintf("'%s' moves %s; a distance must be more than 0", op.name(), op.dist.source())
		}
		if op.ends() && i < len(ops)-1 {
			return fmt.Sprintf("'%s' must be its last op", op.name())
		}
		mode = op.modeAfter(mode)
	}
	return ""
}

// fmDistanceValue, fmPromptValue and fmOpValue are the values of an op's
// parts, for the records of the default bindings.
func (state *EvalState) fmDistanceValue(d fmDistance) MShellObject {
	if d.isPercent {
		return state.builtinEnumValue("fmPercent", MShellFloat{d.percent})
	}
	return state.builtinEnumValue("fmRows", MShellInt{d.rows})
}

func (state *EvalState) fmPromptValue(p fmPrompt) MShellObject {
	if p.kind == fmLineAsk {
		return state.builtinEnumValue("fmPromptAsk", MShellString{p.label}, MShellString{p.text}, p.quote)
	}
	return state.builtinEnumValue(fmPromptMembers[p.kind])
}

func (state *EvalState) fmOpValue(op fmOp) MShellObject {
	switch op.kind {
	case fmOpEdit:
		return state.opValue(op.edit)
	case fmOpCursorTo, fmOpCd, fmOpMessage:
		return state.builtinEnumValue(op.name(), MShellString{op.a})
	case fmOpDownBy, fmOpUpBy:
		return state.builtinEnumValue(op.name(), state.fmDistanceValue(op.dist))
	case fmOpPrompt:
		return state.builtinEnumValue(op.name(), state.fmPromptValue(op.prompt))
	case fmOpRun, fmOpRunWait, fmOpRunOps:
		return state.builtinEnumValue(op.name(), op.quote)
	}
	return state.builtinEnumValue(op.name())
}

// ----- Writing bindings for people (sections 6.1 and 7.5) -----

// source writes a distance as it is written in a program: 10 fmRows.
func (d fmDistance) source() string {
	if d.isPercent {
		n := strconv.FormatFloat(d.percent, 'f', -1, 64)
		if !strings.ContainsAny(n, ".eEIN") {
			n += ".0" // a float literal, as fmPercent takes
		}
		return n + " fmPercent"
	}
	return strconv.Itoa(d.rows) + " fmRows"
}

// fmOpsSource writes a program as it is written in a binding, with
// quotations shortened to (...).
func fmOpsSource(ops []fmOp) string {
	parts := make([]string, len(ops))
	for i, op := range ops {
		var sb strings.Builder
		switch op.kind {
		case fmOpEdit:
			switch op.edit.kind {
			case opInsert, opMessage:
				sb.WriteString(strconv.Quote(op.edit.a) + " ")
			case opSetLine:
				sb.WriteString(strconv.Quote(op.edit.a) + " " + strconv.Quote(op.edit.b) + " ")
			case opRun, opRunOps:
				sb.WriteString("(...) ")
			}
		case fmOpCursorTo, fmOpCd, fmOpMessage:
			sb.WriteString(strconv.Quote(op.a) + " ")
		case fmOpDownBy, fmOpUpBy:
			sb.WriteString(op.dist.source() + " ")
		case fmOpPrompt:
			if op.prompt.kind == fmLineAsk {
				sb.WriteString(strconv.Quote(op.prompt.label) + " " + strconv.Quote(op.prompt.text) + " (...) ")
			}
			sb.WriteString(fmPromptMembers[op.prompt.kind] + " ")
		case fmOpRun, fmOpRunWait, fmOpRunOps:
			sb.WriteString("(...) ")
		}
		sb.WriteString(op.name())
		parts[i] = sb.String()
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// shortNamedKeys are the named keys as the popup and the help screen write
// them.
var shortNamedKeys = []string{
	"enter", "tab", "backspace", "escape", "delete", "insert",
	"up", "down", "left", "right", "home", "end", "pageup", "pagedown",
}

// keyShort writes a key short, as a person reads it: g, ctrl-d, alt-x,
// enter, F5, space.
func keyShort(k KeyCode) string {
	var sb strings.Builder
	for _, m := range []struct {
		bit  KeyCode
		name string
	}{{KeyModCtrl, "ctrl-"}, {KeyModAlt, "alt-"}, {KeyModSuper, "super-"}, {KeyModShift, "shift-"}} {
		if k&m.bit != 0 {
			sb.WriteString(m.name)
		}
	}
	switch {
	case k.isNamed():
		i := int(k.base() - keyNamedBase)
		switch {
		case i < len(shortNamedKeys):
			sb.WriteString(shortNamedKeys[i])
		case i < namedKeyCount:
			fmt.Fprintf(&sb, "F%d", i-len(shortNamedKeys)+1)
		default:
			fmt.Fprintf(&sb, "<key %d>", i)
		}
	case k.baseRune() == ' ':
		sb.WriteString("space")
	default:
		sb.WriteString(terminalSafeText(string(k.baseRune()), false))
	}
	return sb.String()
}

// keysShort writes keys short, separated by spaces.
func keysShort(keys []KeyCode) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = keyShort(k)
	}
	return strings.Join(parts, " ")
}

// ----- The table (section 5.3) -----

// fmBindingEntry is one binding fmBindingAdd added, or a default.
type fmBindingEntry struct {
	bindingCommon[fmMode]
	// The action: ops for fmOps, fn for fmFunc.
	ops []fmOp
	fn  *MShellQuotation
}

// pure reports whether every op of the binding is a pure edit of its mode.
// A quote's result is not known until it runs, so a fmFunc is never pure.
func (e *fmBindingEntry) pure() bool {
	if e.fn != nil {
		return false
	}
	for _, op := range e.ops {
		if !op.pureIn(e.mode) {
			return false
		}
	}
	return true
}

// describe names a binding in a message.
func (e *fmBindingEntry) describe() string { return e.describeIn(fmModeMembers[:]) }

// what is what the popup and the help screen say the binding does: its
// description, or its ops written as source.
func (e *fmBindingEntry) what() string {
	switch {
	case e.description != "":
		return e.description
	case e.fn != nil:
		return "(quotation)"
	}
	return fmOpsSource(e.ops)
}

// parseFmBinding reads an FmBinding record. It fails only on a value the
// type checker refuses; a record that can never fire gives an inert entry.
func (state *EvalState) parseFmBinding(record *MShellDict) (*fmBindingEntry, error) {
	common, opsValue, fn, err := parseBindingFields[fmMode](state, record, "FmAction", "FmMode", fmModeMembers[:])
	if err != nil {
		return nil, err
	}
	e := &fmBindingEntry{bindingCommon: common, fn: fn}
	if opsValue != nil {
		if e.ops, err = parseFmOps(opsValue); err != nil {
			return nil, err
		}
	}
	if e.invalid == "" {
		e.invalid = checkFmProgram(e.ops, e.mode)
	}
	return e, nil
}

var fmKeymapKind = keymapKind[*fmBindingEntry]{
	builtin:   "fmBinding",
	record:    "an FmBinding",
	modeCount: int(fmModeCount),
	parse:     (*EvalState).parseFmBinding,
	defaults:  (*EvalState).fmDefaultBindingEntries,
}

// fmKeymap is the file manager's keymap, with its defaults.
func (state *EvalState) fmKeymap() *keymap[*fmBindingEntry] {
	return state.fmBindings.ready(state, &fmKeymapKind)
}

// ----- The default keymap (section 5.4) -----

// fmDefaultBinding is a binding the file manager has before the init file
// runs.
type fmDefaultBinding struct {
	mode        fmMode
	keys        []KeyCode
	ops         []fmOp
	description string
}

func fmOps(kinds ...fmOpKind) []fmOp {
	out := make([]fmOp, len(kinds))
	for i, k := range kinds {
		out[i] = fmOp{kind: k}
	}
	return out
}

// fmEdit is a program of one line edit.
func fmEdit(k editOpKind) []fmOp {
	return []fmOp{{kind: fmOpEdit, edit: editOp{kind: k}}}
}

// fmBy is a program that moves down or up by a distance.
func fmBy(kind fmOpKind, d fmDistance) []fmOp {
	return []fmOp{{kind: kind, dist: d}}
}

var fmDefaultBindings = sync.OnceValue(func() []fmDefaultBinding {
	ctrl := func(r rune) KeyCode { return KeyCode(r) | KeyModCtrl }
	alt := func(r rune) KeyCode { return KeyCode(r) | KeyModAlt }
	var bs []fmDefaultBinding
	// add binds each key alone; seq binds one sequence.
	add := func(mode fmMode, o []fmOp, description string, keys ...KeyCode) {
		for _, k := range keys {
			bs = append(bs, fmDefaultBinding{mode: mode, keys: []KeyCode{k}, ops: o, description: description})
		}
	}
	seq := func(mode fmMode, o []fmOp, description string, keys ...KeyCode) {
		bs = append(bs, fmDefaultBinding{mode: mode, keys: keys, ops: o, description: description})
	}
	ten := fmDistance{rows: 10}
	page := fmDistance{isPercent: true, percent: 100}
	searchPrompt := []fmOp{{kind: fmOpPrompt, prompt: fmPrompt{kind: fmLineSearch}}}
	renamePrompt := []fmOp{{kind: fmOpPrompt, prompt: fmPrompt{kind: fmLineRename}}}

	n := fmModeNormal
	add(n, fmOps(fmOpQuit), "quit", 'q')
	add(n, fmOps(fmOpQuitNoCd), "quit without changing directory", 'Q')
	add(n, fmOps(fmOpDown), "down", 'j', NamedDown)
	add(n, fmOps(fmOpUp), "up", 'k', NamedUp)
	add(n, fmOps(fmOpParent), "parent directory", 'h', NamedLeft)
	add(n, fmOps(fmOpOpen), "open", 'l', NamedRight, NamedEnter)
	seq(n, fmOps(fmOpTop), "jump to top", 'g', 'g')
	add(n, fmOps(fmOpTop), "jump to top", NamedHome)
	add(n, fmOps(fmOpBottom), "jump to bottom", 'G', NamedEnd)
	add(n, fmBy(fmOpDownBy, ten), "10 down", ctrl('d'))
	add(n, fmBy(fmOpUpBy, ten), "10 up", ctrl('u'))
	add(n, fmBy(fmOpDownBy, page), "a page down", NamedPageDown)
	add(n, fmBy(fmOpUpBy, page), "a page up", NamedPageUp)
	add(n, fmOps(fmOpOpenEditor), "edit in $EDITOR", 'e')
	add(n, renamePrompt, "rename", 'r')
	add(n, searchPrompt, "search", '/')
	add(n, fmOps(fmOpSearchNext), "next match", 'n')
	add(n, fmOps(fmOpSearchPrev), "previous match", 'N')
	add(n, fmOps(fmOpCut), "cut to the file-manager clipboard", 'd')
	seq(n, fmOps(fmOpCopy), "copy entry to file-manager clipboard", 'y', 'y')
	seq(n, fmOps(fmOpYankName), "file name → system clipboard", 'y', 'f')
	seq(n, fmOps(fmOpYankPath), "full path → system clipboard", 'y', 'p')
	seq(n, fmOps(fmOpYankGitPath), "git-relative path → system clipboard", 'y', 'g')
	add(n, fmOps(fmOpPaste), "paste the file-manager clipboard", 'p')
	add(n, fmOps(fmOpClipboardClear), "clear the file-manager clipboard", 'c')
	add(n, fmOps(fmOpTrash), "move to the trash", 'x')
	add(n, fmOps(fmOpToggle, fmOpDown), "select, and move down", ' ')
	add(n, fmOps(fmOpSelectionInvert), "invert the selection", 'v')
	add(n, fmOps(fmOpSelectionClear), "clear the selection", 'u')
	add(n, fmOps(fmOpBookmarkSet), "set a bookmark", 'm')
	add(n, fmOps(fmOpBookmarkGo), "go to a bookmark", ';')
	add(n, fmOps(fmOpReload), "reload", NamedF(5))
	add(n, fmOps(fmOpHelp), "help", '?')

	i := fmModeInsert
	add(i, fmOps(fmOpLineAccept), "", NamedEnter)
	add(i, fmOps(fmOpLineCancel), "", NamedEscape, ctrl('c'))
	add(i, fmEdit(opLeft), "", NamedLeft, ctrl('b'))
	add(i, fmEdit(opRight), "", NamedRight, ctrl('f'))
	add(i, fmEdit(opWordLeft), "", alt('b'))
	add(i, fmEdit(opWordRight), "", alt('f'))
	add(i, fmEdit(opLineStart), "", NamedHome, ctrl('a'))
	add(i, fmEdit(opLineEnd), "", NamedEnd, ctrl('e'))
	add(i, fmEdit(opBackspace), "", NamedBackspace)
	add(i, fmEdit(opDelete), "", NamedDelete)
	add(i, fmEdit(opDeleteWordLeft), "", ctrl('w'), ctrl('h'), NamedBackspace|KeyModCtrl)
	add(i, fmEdit(opDeleteToStart), "", ctrl('u'))
	add(i, fmEdit(opDeleteToEnd), "", ctrl('k'))

	h := fmModeHelp
	add(h, fmOps(fmOpDown), "", 'j', NamedDown)
	add(h, fmOps(fmOpUp), "", 'k', NamedUp)
	add(h, fmBy(fmOpDownBy, ten), "", ctrl('d'))
	add(h, fmBy(fmOpUpBy, ten), "", ctrl('u'))
	add(h, fmBy(fmOpDownBy, page), "", NamedPageDown)
	add(h, fmBy(fmOpUpBy, page), "", NamedPageUp)
	seq(h, fmOps(fmOpTop), "", 'g', 'g')
	add(h, fmOps(fmOpTop), "", NamedHome)
	add(h, fmOps(fmOpBottom), "", 'G', NamedEnd)
	add(h, fmOps(fmOpHelpClose), "", 'q', NamedEscape, '?')
	return bs
})

// fmDefaultBindingEntries are the entries of the file manager's default
// keymap.
func (state *EvalState) fmDefaultBindingEntries() []*fmBindingEntry {
	var entries []*fmBindingEntry
	for _, d := range fmDefaultBindings() {
		opList := NewList(len(d.ops))
		for i, op := range d.ops {
			opList.Items[i] = state.fmOpValue(op)
		}
		record := state.defaultRecord(d.keys, state.builtinEnumValue("fmOps", opList), fmModeMembers[d.mode], d.description)
		common := bindingCommon[fmMode]{record: record, keys: d.keys, mode: d.mode, description: d.description}
		entries = append(entries, &fmBindingEntry{bindingCommon: common, ops: d.ops})
	}
	return entries
}
