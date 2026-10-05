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
// Text items are sanitized and style items take no cells, so the shell
// lays the prompt out exactly, as it does command text.

// builtinDeclSource declares the types of the prompt builtins. They are
// part of the binary: the type checker and the runtime declare them before
// any file, and no file can declare their names again.
const builtinDeclSource = `
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

type PromptInfo = {stackDepth: int, stackTypes: [str], lastLineOk: bool, lastExitCode: Maybe[int], lastDurationMs: int}
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
	Title    string
	HasTitle bool
}

var base16Index = map[string]int{
	"baseBlack": 0, "baseRed": 1, "baseGreen": 2, "baseYellow": 3,
	"baseBlue": 4, "baseMagenta": 5, "baseCyan": 6, "baseWhite": 7,
}

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
// The shape "" is the terminal's default.
func cursorShapeSequence(shape string) string {
	return "\033[" + strconv.Itoa(cursorShapeParam[shape]) + " q"
}

func sgr(params ...int) string {
	parts := make([]string, len(params))
	for i, p := range params {
		parts[i] = strconv.Itoa(p)
	}
	return "\033[" + strings.Join(parts, ";") + "m"
}

// renderPrompt turns the list a prompt quote returned into a prompt to
// paint, or says what is wrong with it.
func renderPrompt(obj MShellObject) (promptRender, error) {
	list, ok := obj.(*MShellList)
	if !ok {
		return promptRender{}, fmt.Errorf("the prompt must return a list of PromptItem values, not a %s", obj.TypeName())
	}
	var r promptRender
	var text strings.Builder
	style := func(seq string, bg bgEffect) {
		r.Styles = append(r.Styles, promptStyle{Offset: ByteOffset(text.Len()), Seq: seq, Bg: bg})
	}
	for i, item := range list.Items {
		e, ok := item.(*MShellEnum)
		if !ok || e.EnumName != "PromptItem" {
			return promptRender{}, fmt.Errorf("item %d of the prompt is a %s, not a PromptItem", i, item.TypeName())
		}
		bad := func(format string, args ...any) error {
			return fmt.Errorf("item %d of the prompt, '%s': %s", i, e.Member, fmt.Sprintf(format, args...))
		}
		switch e.Member {
		case "promptText":
			s, err := payloadString(e, 0)
			if err != nil {
				return promptRender{}, bad("%s", err)
			}
			text.WriteString(terminalSafeText(s, false))
		case "promptNewline":
			text.WriteByte('\n')
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
				style(sgr(38, 2, rgb[0], rgb[1], rgb[2]), bgKeep)
			} else {
				style(sgr(48, 2, rgb[0], rgb[1], rgb[2]), bgSet)
			}
		case "setFgColor256", "setBgColor256":
			n, err := payloadByte(e, 0)
			if err != nil {
				return promptRender{}, bad("%s", err)
			}
			if e.Member == "setFgColor256" {
				style(sgr(38, 5, n), bgKeep)
			} else {
				style(sgr(48, 5, n), bgSet)
			}
		case "setFgColorBase16", "setBgColorBase16":
			color, ok1 := payloadMember(e, 0, "Base16Color")
			bright, ok2 := payloadMember(e, 1, "BaseBrightness")
			if !ok1 || !ok2 {
				return promptRender{}, bad("takes a Base16Color and a BaseBrightness")
			}
			n := base16Index[color]
			if bright == "baseBright" {
				n += 60
			}
			if e.Member == "setFgColorBase16" {
				style(sgr(30+n), bgKeep)
			} else {
				style(sgr(40+n), bgSet)
			}
		case "setFgDefault":
			style(sgr(39), bgKeep)
		case "setBgDefault":
			style(sgr(49), bgClear)
		case "setTextAttr", "clearTextAttr":
			attr, ok := payloadMember(e, 0, "TextAttribute")
			if !ok {
				return promptRender{}, bad("takes a TextAttribute")
			}
			if e.Member == "setTextAttr" {
				style(sgr(textAttrOn[attr]), bgKeep)
			} else {
				style(sgr(textAttrOff[attr]), bgKeep)
			}
		case "resetStyle":
			style(sgr(0), bgClear)
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
			style("\033]8;;"+url+"\033\\", bgKeep)
			text.WriteString(terminalSafeText(label, false))
			style("\033]8;;\033\\", bgKeep)
		default:
			return promptRender{}, bad("unknown prompt item")
		}
	}
	r.Text = SourceText(text.String())
	return r, nil
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
func (state *TermState) userPrompt() (promptRender, bool) {
	ev := &state.evalState
	quote := ev.PromptQuote
	if quote == nil {
		return promptRender{}, false
	}
	stack := MShellStack{state.promptInfo()}

	cancel := newEvalCancel()
	var timedOut atomic.Bool
	timer := time.AfterFunc(promptTimeout, func() {
		timedOut.Store(true)
		cancel.request()
	})
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	finished := make(chan struct{})
	go func() {
		select {
		case <-interrupts:
			cancel.request()
		case <-finished:
		}
	}()

	var errOut bytes.Buffer
	ev.cancel, ev.failOut = cancel, &errOut
	result, err := ev.EvaluateQuote(quote, &stack, state.context, state.stdLibDefs)
	ev.cancel, ev.failOut = nil, nil
	close(finished)
	signal.Stop(interrupts)
	timer.Stop()

	// A quote that finished with its list is shown, even if the time ran
	// out as it finished.
	var problem string
	var render promptRender
	switch {
	case err != nil:
		problem = err.Error()
	case result.ExitCalled:
		problem = "The prompt called 'exit'; that does not end the shell.\n"
	case result.Success && len(stack) == 1:
		render, err = renderPrompt(stack[0])
		if err != nil {
			problem = err.Error() + ".\n"
		}
	case result.Success:
		problem = fmt.Sprintf("The prompt must leave one list on the stack, and left %d values.\n", len(stack))
	case timedOut.Load():
		problem = fmt.Sprintf("The prompt took longer than %d seconds.\n", int(promptTimeout/time.Second))
	case cancel.requested.Load():
		problem = "The prompt was interrupted.\n"
	default:
		problem = errOut.String()
		if problem == "" {
			// A command run with '!' stops the quote without a message.
			problem = fmt.Sprintf("The prompt stopped with exit code %d.\n", result.ExitCode)
		}
	}
	if problem != "" {
		if problem != state.promptProblem {
			fmt.Fprint(os.Stderr, terminalSafeText(problem, true))
			fmt.Fprintln(os.Stderr, "Showing the built-in prompt.")
		}
		state.promptProblem = problem
		return promptRender{}, false
	}
	state.promptProblem = ""
	return render, true
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
		fmt.Fprint(os.Stdout, cursorShapeSequence(""))
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
