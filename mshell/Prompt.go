package main

import (
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// The interactive prompt a user defines (ai/prompt-design.typ). The init
// file registers a quote with setPrompt; before each prompt the shell runs
// it on a PromptInfo and paints the list of PromptItem values it returns.
// A quote registered with setRightPrompt is run the same way, and its one
// line is painted at the right edge of the command's row.
// Text items are sanitized and style items take no cells, so the shell
// lays the prompt out exactly, as it does command text.

// builtinDeclSource declares Result (a value or an error, unwrapped by
// '?'), the types of the prompt builtins, the
// Cipher that encrypt and decrypt take (Crypto.go), the types of the
// abbreviation builtins (Abbr.go), the types of the key binding
// builtins (Bindings.go) and of the file manager's (FmBindings.go), and
// the cells parseExcel gives (Excel.go). They are
// part of the binary: the type checker and the runtime declare them before
// any file, and no file can declare their names again.
const builtinDeclSource = `
enum Result[a e] = ok a | err e end

enum Base16Color =
      baseBlack | baseRed | baseGreen | baseYellow
    | baseBlue | baseMagenta | baseCyan | baseWhite
end

enum BaseBrightness = baseNormal | baseBright end

enum TextAttribute =
      attrBold | attrFaint | attrItalic | attrUnderline
    | attrBlinking | attrReverse | attrHidden | attrStrikethrough
end

enum PromptItem =
      promptText str
    | promptNewline
    | setFgColorRgb int int int
    | setBgColorRgb int int int
    | setFgColor256 int
    | setBgColor256 int
    | setFgColorBase16 Base16Color BaseBrightness
    | setBgColorBase16 Base16Color BaseBrightness
    | setFgDefault
    | setBgDefault
    | setTextAttr TextAttribute
    | clearTextAttr TextAttribute
    | resetStyle
    | setWindowTitle str
    | promptHyperlink str str
end

enum CursorShape =
      blinkingBlock | steadyBlock
    | blinkingUnderline | steadyUnderline
    | blinkingBar | steadyBar
end

enum Cipher = cipherAesGcm | cipherChaCha20Poly1305 | cipherXChaCha20Poly1305 end

enum ExcelError = excelError str end

type ExcelCell = str | float | bool | ExcelError

type PromptInfo = {stackDepth: int, stackTypes: [str], lastLineOk: bool, lastExitCode: Maybe[int], lastDurationMs: int}

enum AbbrMatch = abbrWord str | abbrRegex str end

enum AbbrPosition = abbrCommand | abbrAnywhere | abbrArgOf [str] end

type AbbrInfo = {word: str, command: Maybe[str], line: str}

enum AbbrExpansion = abbrText str | abbrFunc (AbbrInfo -- Maybe[str]) end

type Abbr = {abbr: AbbrMatch, expansion: AbbrExpansion, position?: AbbrPosition, cursorMarker?: str, description?: str}

enum NamedKey =
      keyEnter | keyTab | keyBackspace | keyEscape | keyDelete | keyInsert
    | keyUp | keyDown | keyLeft | keyRight | keyHome | keyEnd | keyPageUp | keyPageDown
    | keyF1 | keyF2 | keyF3 | keyF4 | keyF5 | keyF6 | keyF7 | keyF8 | keyF9 | keyF10
    | keyF11 | keyF12 | keyF13 | keyF14 | keyF15 | keyF16 | keyF17 | keyF18 | keyF19 | keyF20
    | keyF21 | keyF22 | keyF23 | keyF24 | keyF25 | keyF26 | keyF27 | keyF28 | keyF29 | keyF30
    | keyF31 | keyF32 | keyF33 | keyF34 | keyF35
end

enum ModifiedKey = keyCtrl Key | keyAlt Key | keyShift Key | keySuper Key end

type Key = rune | NamedKey | ModifiedKey

enum EditMode = modeInsert | modeComplete end

type EditState = {before: str, after: str, keys: [Key], mode: EditMode}

enum EditOp =
      editInsert str | editSelfInsert | editSetLine str str
    | editLeft | editRight | editWordLeft | editWordRight | editLineStart | editLineEnd
    | editBackspace | editDelete | editDeleteWordLeft | editDeleteToStart | editDeleteToEnd
    | editExpandAbbr
    | editAccept | editExit | editRun ( -- ) | editRunOps (EditState -- Maybe[[EditOp]])
    | editForward | editAcceptSuggestion
    | editHistoryPrev | editHistoryNext | editHistorySearchBack | editHistorySearchForward
    | editLastArg | editComplete | editCompleteBack
    | editCompleteNext | editCompletePrev | editCompleteLeft | editCompleteRight | editCompleteAccept
    | editClearScreen | editRemeasure | editMessage str
end

enum BindingAction = bindingOps [EditOp] | bindingFunc (EditState -- Maybe[[EditOp]]) end

type Binding = {keys: [Key], action: BindingAction, mode?: EditMode, within?: int, idleBefore?: int, description?: str}

enum FmMode = fmModeNormal | fmModeInsert | fmModeHelp end

enum FmPrompt = fmPromptSearch | fmPromptRename | fmPromptAsk str str (str FmState -- Maybe[[FmOp | EditOp]]) end

type FmLine = {prompt: FmPrompt, before: str, after: str}

type FmState = {dir: str, current: Maybe[str], selected: [str], mode: FmMode, line: Maybe[FmLine], keys: [Key]}

enum FmDistance = fmRows int | fmPercent float end

enum FmOp =
      fmDown | fmUp | fmTop | fmBottom | fmDownBy FmDistance | fmUpBy FmDistance
    | fmSearchNext | fmSearchPrev | fmCursorTo str
    | fmOpen | fmParent | fmCd str | fmReload | fmOpenEditor
    | fmToggle | fmSelectionInvert | fmSelectionClear
    | fmPrompt FmPrompt
    | fmCut | fmCopy | fmPaste | fmClipboardClear
    | fmYankName | fmYankPath | fmYankGitPath
    | fmTrash | fmBookmarkSet | fmBookmarkGo | fmHelp
    | fmLineAccept | fmLineCancel
    | fmHelpClose
    | fmRun ( -- ) | fmRunWait ( -- ) | fmRunOps (FmState -- Maybe[[FmOp | EditOp]])
    | fmMessage str | fmQuit | fmQuitNoCd
end

type FmProgram = [FmOp | EditOp]

enum FmAction = fmOps FmProgram | fmFunc (FmState -- Maybe[FmProgram]) end

type FmBinding = {keys: [Key], action: FmAction, mode?: FmMode, within?: int, idleBefore?: int, description?: str}
`

// builtinDeclFile is the file the built-in declarations' tokens name.
var builtinDeclFile = &TokenFile{Path: "<built-in>"}

// builtinDeclItems are the parsed built-in declarations.
var builtinDeclItems = sync.OnceValue(func() []MShellParseItem {
	file, err := parseMShellInput(builtinDeclSource, builtinDeclFile)
	if err != nil {
		panic("built-in declarations: " + err.Error())
	}
	return declarationItems(file.Items)
})

// declareBuiltins declares the built-in enums and types in c. They are
// written correctly, so an error is a programmer error.
func (c *coreChecker) declareBuiltins() {
	c.declareAll(builtinDeclItems(), nil)
	if len(c.errs) > 0 {
		panic("built-in declarations: " + c.errs[0].Hint)
	}
}

// ensureBuiltinEnums makes the built-in enums' members construct values
// and their names match patterns at run time. The runtime calls it before
// it first evaluates or registers anything, so every state has them.
func (state *EvalState) ensureBuiltinEnums() {
	if state.EnumMembers != nil {
		return
	}
	state.EnumMembers = make(map[string]EnumMemberInfo)
	state.EnumNames = make(map[string]bool)
	for _, item := range builtinDeclItems() {
		if d, ok := item.(*MShellEnumDecl); ok {
			state.EnumNames[d.Name] = true
			for i, m := range d.Members {
				state.EnumMembers[m] = EnumMemberInfo{EnumName: d.Name, Arity: len(d.MemberPayloads[i]), Ordinal: i}
			}
		}
	}
}

// promptTimeout is how long the prompt quote may run before the shell
// stops it and shows the built-in prompt.
const promptTimeout = 3 * time.Second

// evalCancel stops an evaluation from another goroutine. The evaluator
// stops before its next item, and a process it is waiting on is killed.
type evalCancel struct {
	requested atomic.Bool
	done      chan struct{}
	once      sync.Once
}

func newEvalCancel() *evalCancel {
	return &evalCancel{done: make(chan struct{})}
}

func (c *evalCancel) request() {
	c.once.Do(func() {
		c.requested.Store(true)
		close(c.done)
	})
}

// cancelled reports whether the running evaluation has been asked to stop.
func (state *EvalState) cancelled() bool {
	return state.cancel != nil && state.cancel.requested.Load()
}

// cancelledResult is the failure an evaluation returns when it is stopped.
// Nothing is printed: whoever stopped it says why.
func cancelledResult() EvalResult {
	return EvalResult{Success: false, BreakNum: -1, ExitCode: 1}
}

// killOnCancel kills process (and the process group it leads, if any)
// when the running evaluation is stopped before it exits. The returned
// function ends the watch; call it once the process has been waited on.
func (state *EvalState) killOnCancel(process *os.Process) func() {
	if state.cancel == nil || process == nil {
		return func() {}
	}
	cancel := state.cancel
	exited := make(chan struct{})
	go func() {
		select {
		case <-cancel.done:
			KillProcessGroup(process.Pid)
			process.Kill()
		case <-exited:
		}
	}()
	return func() { close(exited) }
}

// sleepUnlessCancelled sleeps for d, or until the running evaluation is
// stopped.
func (state *EvalState) sleepUnlessCancelled(d time.Duration) {
	if state.cancel == nil {
		time.Sleep(d)
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-state.cancel.done:
	}
}

// clearLastExitCode forgets the exit code of the last process run, so a
// line that runs none reports none.
func (state *EvalState) clearLastExitCode() {
	state.lastExitCode, state.ranProcess = 0, false
}

// promptStyle is an escape sequence written before the prompt text at
// Offset. It takes no cells.
type promptStyle struct {
	Offset ByteOffset
	Seq    string
	Bg     bgEffect
}

// bgEffect is what a style does to the background color, which the
// painter turns off around each line break.
type bgEffect uint8

const (
	bgKeep  bgEffect = iota // leaves the background as it is
	bgSet                   // sets a background color
	bgClear                 // sets the default background
)

// promptRender is a prompt ready to paint: its text, laid out as command
// text is, and the styles to write within it.
type promptRender struct {
	Text     SourceText
	Styles   []promptStyle
	// all is the one string renderPrompt made: Text, then each style's Seq.
	all string
	Title    string
	HasTitle bool
}

// The tables below give each member of a built-in enum its escape
// sequence parameter. TestPromptTablesCoverBuiltinEnums keeps them in step
// with builtinDeclSource.

var base16Index = map[string]int{
	"baseBlack": 0, "baseRed": 1, "baseGreen": 2, "baseYellow": 3,
	"baseBlue": 4, "baseMagenta": 5, "baseCyan": 6, "baseWhite": 7,
}

// baseBrightOffset moves a base color to its bright version.
var baseBrightOffset = map[string]int{"baseNormal": 0, "baseBright": 60}

// SGR parameters to turn each attribute on, and off. Bold and faint are
// both turned off by 22.
var textAttrOn = map[string]int{
	"attrBold": 1, "attrFaint": 2, "attrItalic": 3, "attrUnderline": 4,
	"attrBlinking": 5, "attrReverse": 7, "attrHidden": 8, "attrStrikethrough": 9,
}

var textAttrOff = map[string]int{
	"attrBold": 22, "attrFaint": 22, "attrItalic": 23, "attrUnderline": 24,
	"attrBlinking": 25, "attrReverse": 27, "attrHidden": 28, "attrStrikethrough": 29,
}

// cursorShapeParam is the DECSCUSR parameter of each cursor shape.
var cursorShapeParam = map[string]int{
	"blinkingBlock": 1, "steadyBlock": 2, "blinkingUnderline": 3,
	"steadyUnderline": 4, "blinkingBar": 5, "steadyBar": 6,
}

// cursorShapeSequence is the escape sequence that sets a cursor shape.
func cursorShapeSequence(shape string) string {
	return "\033[" + strconv.Itoa(cursorShapeParam[shape]) + " q"
}

// cursorShapeReset gives the terminal back its default cursor shape.
const cursorShapeReset = "\033[0 q"

// appendSGR appends the escape sequence that sets the SGR parameters.
func appendSGR(dst []byte, params ...int) []byte {
	// With no parameters the sequence would be a reset.
	if len(params) == 0 {
		panic("appendSGR: no parameters")
	}
	dst = append(dst, "\033["...)
	for i, p := range params {
		if p < 0 {
			panic(fmt.Sprintf("appendSGR: parameter %d is negative", p))
		}
		if p > 255 {
			panic(fmt.Sprintf("appendSGR: parameter %d is above 255", p))
		}
		if i > 0 {
			dst = append(dst, ';')
		}
		dst = strconv.AppendInt(dst, int64(p), 10)
	}
	return append(dst, 'm')
}

func sgr(params ...int) string {
	return string(appendSGR(nil, params...))
}

// renderPrompt turns the list a prompt quote returned into a prompt to
// paint, or says what is wrong with it. A right prompt is one line, so
// promptNewline is wrong in it.
func renderPrompt(obj MShellObject, right bool) (promptRender, error) {
	return renderPromptReusing(obj, right, promptRender{})
}

// renderPromptReusing is renderPrompt, given what it returned for the last
// prompt, which is no longer painted. The prompt is rendered again for
// every prompt, so this allocates as little as it can. The text and every
// escape sequence are built in two buffers and then copied into one string,
// which the text and the styles all point into. The styles are stored in
// last's memory when they fit, and when the text and sequences are the same
// as last's, its string is used again and nothing is allocated.
func renderPromptReusing(obj MShellObject, right bool, last promptRender) (promptRender, error) {
	reuse := last.Styles
	name := "prompt"
	if right {
		name = "right prompt"
	}
	list, ok := obj.(*MShellList)
	if !ok {
		return promptRender{}, fmt.Errorf("the %s must return a list of PromptItem values, not a %s", name, obj.TypeName())
	}
	// Each item adds at most one style, but for a hyperlink, which adds two.
	if cap(reuse) < len(list.Items) {
		reuse = make([]promptStyle, 0, len(list.Items))
	}
	r := promptRender{Styles: reuse[:0]}
	// A prompt of ordinary size is built without leaving the stack.
	var textArray [256]byte
	var seqArray [512]byte
	var seqEndArray [32]int
	// text is the prompt's text, seqs every style's escape sequence one
	// after another, and seqEnds where each style's sequence ends in seqs.
	text, seqs, seqEnds := textArray[:0], seqArray[:0], seqEndArray[:0]
	// style adds a style whose escape sequence was just appended to seqs.
	style := func(bg bgEffect) {
		r.Styles = append(r.Styles, promptStyle{Offset: ByteOffset(len(text)), Bg: bg})
		seqEnds = append(seqEnds, len(seqs))
	}
	for i, item := range list.Items {
		e, ok := item.(*MShellEnum)
		if !ok || e.EnumName != "PromptItem" {
			return promptRender{}, fmt.Errorf("item %d of the %s is a %s, not a PromptItem", i, name, item.TypeName())
		}
		bad := func(format string, args ...any) error {
			return fmt.Errorf("item %d of the %s, '%s': %s", i, name, e.Member, fmt.Sprintf(format, args...))
		}
		switch e.Member {
		case "promptText":
			s, err := payloadString(e, 0)
			if err != nil {
				return promptRender{}, bad("%s", err)
			}
			text = appendTerminalSafeText(text, s, false)
		case "promptNewline":
			if right {
				return promptRender{}, bad("the right prompt is one line")
			}
			text = append(text, '\n')
		case "setFgColorRgb", "setBgColorRgb":
			var rgb [3]int
			for j := range rgb {
				n, err := payloadByte(e, j)
				if err != nil {
					return promptRender{}, bad("%s", err)
				}
				rgb[j] = n
			}
			if e.Member == "setFgColorRgb" {
				seqs = appendSGR(seqs, 38, 2, rgb[0], rgb[1], rgb[2])
				style(bgKeep)
			} else {
				seqs = appendSGR(seqs, 48, 2, rgb[0], rgb[1], rgb[2])
				style(bgSet)
			}
		case "setFgColor256", "setBgColor256":
			n, err := payloadByte(e, 0)
			if err != nil {
				return promptRender{}, bad("%s", err)
			}
			if e.Member == "setFgColor256" {
				seqs = appendSGR(seqs, 38, 5, n)
				style(bgKeep)
			} else {
				seqs = appendSGR(seqs, 48, 5, n)
				style(bgSet)
			}
		case "setFgColorBase16", "setBgColorBase16":
			color, ok1 := payloadMember(e, 0, "Base16Color")
			bright, ok2 := payloadMember(e, 1, "BaseBrightness")
			if !ok1 || !ok2 {
				return promptRender{}, bad("takes a Base16Color and a BaseBrightness")
			}
			n, ok1 := base16Index[color]
			offset, ok2 := baseBrightOffset[bright]
			if !ok1 || !ok2 {
				return promptRender{}, bad("no escape sequence for %s %s", color, bright)
			}
			n += offset
			if e.Member == "setFgColorBase16" {
				seqs = appendSGR(seqs, 30+n)
				style(bgKeep)
			} else {
				seqs = appendSGR(seqs, 40+n)
				style(bgSet)
			}
		case "setFgDefault":
			seqs = appendSGR(seqs, 39)
			style(bgKeep)
		case "setBgDefault":
			seqs = appendSGR(seqs, 49)
			style(bgClear)
		case "setTextAttr", "clearTextAttr":
			attr, ok := payloadMember(e, 0, "TextAttribute")
			if !ok {
				return promptRender{}, bad("takes a TextAttribute")
			}
			table := textAttrOff
			if e.Member == "setTextAttr" {
				table = textAttrOn
			}
			n, ok := table[attr]
			if !ok {
				return promptRender{}, bad("no escape sequence for %s", attr)
			}
			seqs = appendSGR(seqs, n)
			style(bgKeep)
		case "resetStyle":
			seqs = appendSGR(seqs, 0)
			style(bgClear)
		case "setWindowTitle":
			s, err := payloadString(e, 0)
			if err != nil {
				return promptRender{}, bad("%s", err)
			}
			r.Title, r.HasTitle = terminalSafeText(s, false), true
		case "promptHyperlink":
			url, err := payloadString(e, 0)
			if err != nil {
				return promptRender{}, bad("%s", err)
			}
			label, err := payloadString(e, 1)
			if err != nil {
				return promptRender{}, bad("%s", err)
			}
			if err := checkHyperlinkURL(url); err != nil {
				return promptRender{}, bad("%s", err)
			}
			seqs = append(append(append(seqs, "\033]8;;"...), url...), "\033\\"...)
			style(bgKeep)
			text = appendTerminalSafeText(text, label, false)
			seqs = append(seqs, "\033]8;;\033\\"...)
			style(bgKeep)
		default:
			return promptRender{}, bad("unknown prompt item")
		}
	}
	// Comparing a string with bytes does not copy the bytes.
	if len(last.all) == len(text)+len(seqs) && last.all[:len(text)] == string(text) && last.all[len(text):] == string(seqs) {
		r.all = last.all
	} else {
		var all strings.Builder
		all.Grow(len(text) + len(seqs))
		all.Write(text)
		all.Write(seqs)
		r.all = all.String()
	}
	// style is called once after each sequence appended to seqs. Otherwise
	// every later style would get another style's sequence.
	if len(seqEnds) != len(r.Styles) {
		panic(fmt.Sprintf("renderPrompt: %d sequence ends for %d styles", len(seqEnds), len(r.Styles)))
	}
	if len(seqEnds) > 0 && seqEnds[len(seqEnds)-1] != len(seqs) {
		panic(fmt.Sprintf("renderPrompt: the last style's sequence ends at %d of %d bytes", seqEnds[len(seqEnds)-1], len(seqs)))
	}
	if len(seqEnds) == 0 && len(seqs) != 0 {
		panic(fmt.Sprintf("renderPrompt: %d bytes of sequences and no styles", len(seqs)))
	}
	if len(r.all) != len(text)+len(seqs) {
		panic(fmt.Sprintf("renderPrompt: string of %d bytes for %d of text and %d of sequences", len(r.all), len(text), len(seqs)))
	}
	r.Text = SourceText(r.all[:len(text)])
	allSeqs, start := r.all[len(text):], 0
	for i, end := range seqEnds {
		r.Styles[i].Seq = allSeqs[start:end]
		start = end
	}
	checkPromptStyles(r.Styles, len(r.Text))
	return r, nil
}

// checkPromptStyles panics unless styles are as renderPrompt makes them for
// a text of textLen bytes: each has an escape sequence, is at an offset
// within the text, and is not before the style ahead of it. The loops that
// paint styles write them in order and would write one out of order late.
func checkPromptStyles(styles []promptStyle, textLen int) {
	last := ByteOffset(0)
	for i, s := range styles {
		if s.Seq == "" {
			panic(fmt.Sprintf("prompt style %d has no escape sequence", i))
		}
		if s.Offset < last {
			panic(fmt.Sprintf("prompt style %d is at offset %d, before the style ahead of it at %d", i, s.Offset, last))
		}
		if int(s.Offset) > textLen {
			panic(fmt.Sprintf("prompt style %d is at offset %d, past the text's %d bytes", i, s.Offset, textLen))
		}
		last = s.Offset
	}
}

func payloadString(e *MShellEnum, i int) (string, error) {
	if i < len(e.Payload) {
		if s, ok := e.Payload[i].(MShellString); ok {
			return s.Content, nil
		}
	}
	return "", fmt.Errorf("payload %d is not a str", i+1)
}

// payloadByte is payload i, an int from 0 to 255.
func payloadByte(e *MShellEnum, i int) (int, error) {
	if i < len(e.Payload) {
		if n, ok := e.Payload[i].(MShellInt); ok {
			if n.Value < 0 || n.Value > 255 {
				return 0, fmt.Errorf("%d is out of range; it must be 0 to 255", n.Value)
			}
			return n.Value, nil
		}
	}
	return 0, fmt.Errorf("payload %d is not an int", i+1)
}

// payloadMember is the member name of payload i, a value of enum.
func payloadMember(e *MShellEnum, i int, enum string) (string, bool) {
	if i < len(e.Payload) {
		if p, ok := e.Payload[i].(*MShellEnum); ok && p.EnumName == enum {
			return p.Member, true
		}
	}
	return "", false
}

// checkHyperlinkURL accepts a URL that can be written inside an OSC 8
// sequence: printable ASCII, with no spaces. Anything else, including the
// ESC that would end the sequence early, must be percent-encoded.
func checkHyperlinkURL(url string) error {
	if url == "" {
		return fmt.Errorf("the URL is empty")
	}
	for i := 0; i < len(url); i++ {
		if b := url[i]; b <= ' ' || b >= 0x7f {
			return fmt.Errorf("the URL has a character that must be percent-encoded at byte %d", i)
		}
	}
	return nil
}

// defaultPrompt is the built-in prompt: the directory and the stack depth,
// then the command on the next line.
func defaultPrompt(cwd string, ok bool, depth int) promptRender {
	text := "??? >"
	if ok {
		text = fmt.Sprintf("%s (%d)> \n:: ", terminalSafeText(cwd, false), depth)
	}
	return promptRender{Text: SourceText(text), Styles: []promptStyle{{Offset: 0, Seq: sgr(35)}}}
}

// promptInfo is what the prompt quote is told about the session.
func (state *TermState) promptInfo() *MShellDict {
	info := NewDict()
	info.Items["stackDepth"] = MShellInt{len(state.stack)}
	types := NewList(0)
	if state.checker != nil {
		for _, t := range state.checker.session.StackTypes() {
			types.Items = append(types.Items, MShellString{t})
		}
	}
	info.Items["stackTypes"] = types
	info.Items["lastLineOk"] = MShellBool{state.lastLineOk}
	exitCode := &Maybe{}
	if state.lastLineExitSet {
		exitCode.obj = MShellInt{state.lastLineExit}
	}
	info.Items["lastExitCode"] = exitCode
	info.Items["lastDurationMs"] = MShellInt{int(state.lastLineDuration.Milliseconds())}
	return info
}

// userPrompt runs the prompt quote the init file set, and returns what it
// asks to paint. It returns false when there is no quote, or when it fails,
// is interrupted, or runs past promptTimeout; then the built-in prompt is
// shown. An error is printed once, not again while the quote keeps failing
// the same way. The terminal is in its normal mode, so Ctrl-C arrives as an
// interrupt.
func (state *TermState) userPrompt(interrupts *atomic.Pointer[evalCancel]) (promptRender, bool) {
	render, ok := state.runPromptQuote(state.evalState.PromptQuote, false, state.userPromptRender, interrupts)
	state.userPromptRender = render
	return render, ok
}

// userRightPrompt runs the right prompt quote the init file set, as
// userPrompt runs the prompt quote. When there is no quote, or it gives
// nothing to use, there is no right prompt.
func (state *TermState) userRightPrompt(interrupts *atomic.Pointer[evalCancel]) promptRender {
	render, _ := state.runPromptQuote(state.evalState.RightPromptQuote, true, state.rightPrompt, interrupts)
	return render
}

// runPromptQuote runs the prompt quote, or the right prompt quote, for
// userPrompt and userRightPrompt. last is what that quote's last prompt
// rendered, which is no longer painted; the new prompt takes its memory.
// While the quote runs, a Ctrl-C seen by the watcher of interrupts stops it.
func (state *TermState) runPromptQuote(quote *MShellQuotation, right bool, last promptRender, interrupts *atomic.Pointer[evalCancel]) (promptRender, bool) {
	what, fallback, lastProblem := "The prompt", "Showing the built-in prompt.", &state.promptProblem
	if right {
		what, fallback, lastProblem = "The right prompt", "Showing no right prompt.", &state.rightPromptProblem
	}
	if quote == nil {
		return promptRender{}, false
	}
	stack := MShellStack{state.promptInfo()}
	cancel := newEvalCancel()
	// A Ctrl-C is for the one quote running. The quote before this one
	// cleared its cancel when it finished.
	if interrupts.Swap(cancel) != nil {
		panic("runPromptQuote: another quote's cancel is still stored")
	}
	run := state.runUserQuote(quote, &stack, state.context, promptTimeout, cancel)
	interrupts.Store(nil)

	// A quote that finished with its list is shown, even if the time ran
	// out as it finished.
	var render promptRender
	problem := run.problem(what, "the shell", promptTimeout)
	if problem == "" && len(stack) != 1 {
		problem = fmt.Sprintf("%s must leave one list on the stack, and left %d values.\n", what, len(stack))
	} else if problem == "" {
		var err error
		if render, err = renderPromptReusing(stack[0], right, last); err != nil {
			problem = err.Error() + ".\n"
		}
	}
	if problem != "" {
		if problem != *lastProblem {
			fmt.Fprint(os.Stderr, terminalSafeText(problem, true))
			fmt.Fprintln(os.Stderr, fallback)
		}
		*lastProblem = problem
		return promptRender{}, false
	}
	*lastProblem = ""
	return render, true
}

// userQuoteRun is how a quote the init file registered ended, when the
// shell ran it.
type userQuoteRun struct {
	result    EvalResult
	err       error
	timedOut  bool
	cancelled bool
	// errOut is what the quote printed as it failed.
	errOut string
}

// runUserQuote runs quote on stack, and stops it, together with any
// command it runs, after timeout or when cancel is requested.
func (state *TermState) runUserQuote(quote *MShellQuotation, stack *MShellStack, context ExecuteContext, timeout time.Duration, cancel *evalCancel) userQuoteRun {
	return state.evalState.runUserQuote(quote, stack, context, state.stdLibDefs, timeout, cancel)
}

// runUserQuote runs a quote with the definitions given, as the TermState
// method does.
func (ev *EvalState) runUserQuote(quote *MShellQuotation, stack *MShellStack, context ExecuteContext, definitions []MShellDefinition, timeout time.Duration, cancel *evalCancel) userQuoteRun {
	var timedOut atomic.Bool
	timer := time.AfterFunc(timeout, func() {
		timedOut.Store(true)
		cancel.request()
	})

	var errOut bytes.Buffer
	ev.cancel, ev.failOut = cancel, &errOut
	result, err := ev.EvaluateQuote(quote, stack, context, definitions)
	ev.cancel, ev.failOut = nil, nil
	timer.Stop()
	return userQuoteRun{result: result, err: err, timedOut: timedOut.Load(), cancelled: cancel.requested.Load(), errOut: errOut.String()}
}

// watchInterrupts requests the cancel stored in running on each Ctrl-C,
// until the function it returns is called. The terminal must be in its
// normal mode, so Ctrl-C arrives as an interrupt. Registering for the
// signal takes tens of microseconds, too long for a key press, so the
// quotes run for one prompt share one watcher: each stores its own cancel
// in running as it starts, and a Ctrl-C stops only the one then running.
func watchInterrupts(running *atomic.Pointer[evalCancel]) func() {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	finished := make(chan struct{})
	go func() {
		for {
			select {
			case <-interrupts:
				if cancel := running.Load(); cancel != nil {
					cancel.request()
				}
			case <-finished:
				return
			}
		}
	}()
	return func() {
		close(finished)
		signal.Stop(interrupts)
	}
}

// failure says why a quote that did not succeed stopped. what names the
// quote, starting a sentence.
func (run userQuoteRun) failure(what string, timeout time.Duration) string {
	switch {
	case run.timedOut:
		return fmt.Sprintf("%s took longer than %s.\n", what, timeoutText(timeout))
	case run.cancelled:
		return what + " was interrupted.\n"
	case run.errOut != "":
		return run.errOut
	default:
		// A command run with '!' stops the quote without a message.
		return fmt.Sprintf("%s stopped with exit code %d.\n", what, run.result.ExitCode)
	}
}

// problem says why a quote run with a time limit gave nothing to use, or
// is "" when it succeeded. what names the quote, starting a sentence, and
// owner is what its exit does not end.
func (run userQuoteRun) problem(what, owner string, timeout time.Duration) string {
	switch {
	case run.err != nil:
		return run.err.Error()
	case run.result.ExitCalled:
		return what + " called 'exit'; that does not end " + owner + ".\n"
	case run.result.Success:
		return ""
	}
	return run.failure(what, timeout)
}

// timeoutText is a time limit in words: "3 seconds", "1 second".
func timeoutText(d time.Duration) string {
	if n := int(d / time.Second); n != 1 {
		return fmt.Sprintf("%d seconds", n)
	}
	return "1 second"
}

// writeWindowTitle sets the terminal's window title with OSC 2. title is
// already sanitized, so it cannot end the sequence early.
func writeWindowTitle(title string) {
	fmt.Fprintf(os.Stdout, "\033]2;%s\033\\", title)
}

// applyCursorShape sets the cursor shape the init file chose, while the
// shell reads a command.
func (state *TermState) applyCursorShape() {
	if shape := state.evalState.CursorShape; shape != "" {
		fmt.Fprint(os.Stdout, cursorShapeSequence(shape))
		state.cursorShapeSet = true
	}
}

// resetCursorShape gives the terminal back its default cursor before a
// command runs or the shell exits, so other programs do not inherit it.
func (state *TermState) resetCursorShape() {
	if state.cursorShapeSet {
		fmt.Fprint(os.Stdout, cursorShapeReset)
		state.cursorShapeSet = false
	}
}

// cancelOnInterrupt stops the running evaluation, when it can be stopped,
// after a process it ran was ended by Ctrl-C. The process had the terminal,
// so the interrupt went to it and not to the shell.
func (state *EvalState) cancelOnInterrupt(exitCode int) {
	if state.cancel != nil && exitCode == -(signalBase+2) {
		state.cancel.request()
	}
}
