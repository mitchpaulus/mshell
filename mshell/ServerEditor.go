//go:build linux || darwin

package main

// The line editor's side of msh server (ai/gui-terminal.typ, "The line
// editor"): the editor config, and the requests for what only the engine
// can decide.
//
// The frontend edits the line itself, with the Rust port of the editor
// (gui/msh-editor). The engine sends it the bindings in force and the
// abbreviations that can expand, as editor_config, at startup and whenever a
// request changed them, before that request's reply or block_finished. A
// quotation in the config is an id; the frontend sends it back to have it
// run.
//
// Frontend to engine (u32 request ids are the frontend's; replies carry
// them back):
//
//	0x04 binding_call  u32 request, u32 version, u32 quote, u8 mode,
//	                   u8 key count, u32 keys..., str before, after (rest)
//	0x05 abbr_call     u32 request, u32 version, u32 quote, u8 has command,
//	                   str word, str command, line (rest)
//	0x06 regex_match   u32 request, u32 version, u32 abbr, word (rest)
//	0x07 is_program    u32 request, word (rest)
//
// Engine to frontend:
//
//	0x86 editor_config u32 version, then the bindings and abbreviations
//	                   (encodeEditorConfig)
//	0x87 call_result   u32 request, u8 status, then by status:
//	                   0 no: declined, no match, not a program
//	                   1 yes: binding_call: u16 op count and the ops;
//	                     abbr_call: the text (rest)
//	                   2 failed: the problem, to show (rest)
//	                   3 stale: the config version has changed; resolve again
//
// A str is a u32 length and the bytes. An op is a u8 EditOp member number
// (Bindings.go's editOpKind) and its payload: a str for editInsert and
// editMessage, two for editSetLine, a u32 quote for editRun and
// editRunOps.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	serverMsgBindingCall = 0x04
	serverMsgAbbrCall    = 0x05
	serverMsgRegexMatch  = 0x06
	serverMsgIsProgram   = 0x07

	serverMsgEditorConfig = 0x86
	serverMsgCallResult   = 0x87
)

const (
	callNo     = 0
	callYes    = 1
	callFailed = 2
	callStale  = 3
)

// serverEditor is what the engine last sent of the editor config: its
// version, its bytes, and what its ids name.
type serverEditor struct {
	version uint32
	sent    []byte
	quotes  []*MShellQuotation
	abbrs   []*abbrEntry
}

// encodeEditorConfig writes the bindings in force and the abbreviations
// that can expand, oldest first, and gives the quotations and
// abbreviations the ids name:
//
//	u32 binding count, then each: u8 mode, u32 within ms, u32 idleBefore ms,
//	  u8 key count, u32 keys..., u8 action (0 ops: u16 count, ops;
//	  1 bindingFunc: u32 quote)
//	u32 abbreviation count, then each: u8 regex, str word, u8 position
//	  (0 command, 1 anywhere, 2 argOf: u16 count, strs), u8 expansion
//	  (0 text: str; 1 abbrFunc: u32 quote), str cursor marker
func encodeEditorConfig(ev *EvalState) ([]byte, []*MShellQuotation, []*abbrEntry) {
	var quotes []*MShellQuotation
	quoteID := func(q *MShellQuotation) uint32 {
		quotes = append(quotes, q)
		return uint32(len(quotes) - 1)
	}
	le := binary.LittleEndian
	var b []byte
	var inForce []*bindingEntry
	for _, e := range ev.shellKeymap().entries {
		if *e.inertReason() == "" {
			inForce = append(inForce, e)
		}
	}
	b = le.AppendUint32(b, uint32(len(inForce)))
	for _, e := range inForce {
		b = append(b, byte(e.mode))
		b = le.AppendUint32(b, uint32(e.within/time.Millisecond))
		b = le.AppendUint32(b, uint32(e.idleBefore/time.Millisecond))
		b = append(b, byte(len(e.keys)))
		for _, k := range e.keys {
			b = le.AppendUint32(b, uint32(k))
		}
		if e.fn != nil {
			b = append(b, 1)
			b = le.AppendUint32(b, quoteID(e.fn))
			continue
		}
		b = append(b, 0)
		b = le.AppendUint16(b, uint16(len(e.ops)))
		for _, op := range e.ops {
			b = appendEditOp(b, op, quoteID)
		}
	}
	var abbrs []*abbrEntry
	for _, e := range ev.Abbrs {
		if e.inert == "" {
			abbrs = append(abbrs, e)
		}
	}
	b = le.AppendUint32(b, uint32(len(abbrs)))
	for _, e := range abbrs {
		regex := byte(0)
		if e.isRegex {
			regex = 1
		}
		b = append(b, regex)
		b = appendStr(b, e.word)
		b = append(b, byte(e.pos))
		if e.pos == abbrPosArgOf {
			b = le.AppendUint16(b, uint16(len(e.argOf)))
			for _, c := range e.argOf {
				b = appendStr(b, c)
			}
		}
		if e.fn != nil {
			b = append(b, 1)
			b = le.AppendUint32(b, quoteID(e.fn))
		} else {
			b = append(b, 0)
			b = appendStr(b, e.text)
		}
		b = appendStr(b, e.marker)
	}
	return b, quotes, abbrs
}

func appendEditOp(b []byte, op editOp, quoteID func(*MShellQuotation) uint32) []byte {
	b = append(b, byte(op.kind))
	switch op.kind {
	case opInsert, opMessage:
		b = appendStr(b, op.a)
	case opSetLine:
		b = appendStr(b, op.a)
		b = appendStr(b, op.b)
	case opRun, opRunOps:
		b = binary.LittleEndian.AppendUint32(b, quoteID(op.quote))
	}
	return b
}

// sendConfigIfChanged sends the editor config when it differs from what the
// frontend has.
func (s *server) sendConfigIfChanged(state *TermState) {
	config, quotes, abbrs := encodeEditorConfig(&state.evalState)
	if s.editor.sent != nil && bytes.Equal(config, s.editor.sent) {
		return
	}
	s.editor.version++
	s.editor.sent, s.editor.quotes, s.editor.abbrs = config, quotes, abbrs
	s.out.send(serverMsgEditorConfig, binary.LittleEndian.AppendUint32(nil, s.editor.version), config)
}

// serverCall is a request from the frontend's editor.
type serverCall struct {
	kind    byte
	payload []byte
}

// reader reads a request's fields in order; a short request is a protocol
// error.
type serverReader struct {
	s   *server
	b   []byte
	bad bool
}

func (r *serverReader) take(n int) []byte {
	if r.bad || len(r.b) < n {
		r.bad = true
		return make([]byte, n)
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}

func (r *serverReader) u8() byte    { return r.take(1)[0] }
func (r *serverReader) u32() uint32 { return binary.LittleEndian.Uint32(r.take(4)) }
func (r *serverReader) str() string { return string(r.take(int(r.u32()))) }
func (r *serverReader) rest() string {
	v := r.b
	r.b = nil
	return string(v)
}

// handleCall answers one request, after sending the config if the request
// changed it.
func (s *server) handleCall(state *TermState, call serverCall) {
	r := &serverReader{s: s, b: call.payload}
	request := r.u32()
	var status byte
	var result []byte
	// The ops a quote returned are encoded after the config is sent: their
	// quotes get ids in the table of the config the frontend then has.
	var program []editOp
	switch call.kind {
	case serverMsgBindingCall:
		version, quote, mode := r.u32(), r.u32(), r.u8()
		keys := make([]KeyCode, r.u8())
		for i := range keys {
			keys[i] = KeyCode(r.u32())
		}
		before := r.str()
		after := r.rest()
		if r.bad || int(mode) >= int(editModeCount) {
			s.protocolError("malformed binding_call")
		}
		if version != s.editor.version || int(quote) >= len(s.editor.quotes) {
			status = callStale
			break
		}
		ops, some, problem := state.callBindingFunc(s.editor.quotes[quote], "The quote of a binding", before, after, keys, editMode(mode))
		switch {
		case problem != "":
			status, result = callFailed, []byte(problem)
		case !some:
			status = callNo
		default:
			// Ops after one that ends the line are not run.
			for i, op := range ops {
				if op.kind.ends() && i < len(ops)-1 {
					ops = ops[:i+1]
					break
				}
			}
			status, program = callYes, ops
		}
	case serverMsgAbbrCall:
		version, quote, hasCommand := r.u32(), r.u32(), r.u8() != 0
		word, command := r.str(), r.str()
		line := r.rest()
		if r.bad {
			s.protocolError("malformed abbr_call")
		}
		if version != s.editor.version || int(quote) >= len(s.editor.quotes) {
			status = callStale
			break
		}
		text, ok, problem := state.callAbbrFunc(s.editor.quotes[quote], "The quote of an abbreviation", word, command, hasCommand, line)
		switch {
		case problem != "":
			status, result = callFailed, []byte(problem)
		case !ok:
			status = callNo
		default:
			status, result = callYes, []byte(text)
		}
	case serverMsgRegexMatch:
		version, abbr := r.u32(), r.u32()
		word := r.rest()
		if r.bad {
			s.protocolError("malformed regex_match")
		}
		if version != s.editor.version || int(abbr) >= len(s.editor.abbrs) || !s.editor.abbrs[abbr].isRegex {
			status = callStale
			break
		}
		if s.editor.abbrs[abbr].matches(word) {
			status = callYes
		}
	case serverMsgEdit:
		status, result = s.handleEdit(state, request, r)
	case serverMsgIsProgram:
		word := r.rest()
		if r.bad {
			s.protocolError("malformed is_program")
		}
		if state.isProgramWord(word) {
			status = callYes
		}
	default:
		s.protocolError(fmt.Sprintf("unknown request 0x%02x", call.kind))
	}
	// A quotation can add bindings or abbreviations: the frontend switches
	// to the new config before it handles the keys queued behind this
	// reply.
	s.sendConfigIfChanged(state)
	if call.kind == serverMsgBindingCall && status == callYes {
		result = binary.LittleEndian.AppendUint16(nil, uint16(len(program)))
		for _, op := range program {
			// An editRun's quote gets the next id of the config just sent.
			result = appendEditOp(result, op, func(q *MShellQuotation) uint32 {
				s.editor.quotes = append(s.editor.quotes, q)
				return uint32(len(s.editor.quotes) - 1)
			})
		}
	}
	fixed := binary.LittleEndian.AppendUint32(nil, request)
	s.out.send(serverMsgCallResult, append(fixed, status), result)
}

// The prompt, and the edits only the engine can make (completion).
//
//	0x08 edit    u32 request, u8 EditOp member (editComplete, editCompleteBack,
//	             editCompleteNext, editCompletePrev, editCompleteLeft,
//	             editCompleteRight, editCompleteAccept), u32 cursor, line (rest).
//	             The reply (call_result 1): u32 cursor, u8 cycling, i32 the
//	             candidate selected (-1: none), u8 bell, u16 candidate count,
//	             strs, then the line (rest). The engine keeps the completion
//	             cycle between requests, as terminal msh does.
//	0x88 prompt  u8 has title, str title, then the prompt as text with SGR
//	             and OSC 8 escapes and \r\n line breaks (rest): what setPrompt's
//	             quotation gave, or the built-in prompt. Sent at startup and
//	             before each block_finished.

const (
	serverMsgEdit   = 0x07 + 1
	serverMsgPrompt = 0x88
)

// sendPrompt runs the prompt as terminal msh does before it reads a line,
// and sends it.
func (s *server) sendPrompt(state *TermState) {
	render, user := state.userPrompt()
	if !user {
		cwd, err := os.Getwd()
		if len(state.homeDir) > 0 && strings.HasPrefix(cwd, state.homeDir) {
			cwd = "~" + cwd[len(state.homeDir):]
		}
		render = defaultPrompt(cwd, err == nil, len(state.stack))
	}
	var text strings.Builder
	next := 0
	for i := 0; i <= len(render.Text); i++ {
		for ; next < len(render.Styles) && int(render.Styles[next].Offset) <= i; next++ {
			text.WriteString(render.Styles[next].Seq)
		}
		if i == len(render.Text) {
			break
		}
		if render.Text[i] == '\n' {
			text.WriteString("\r")
		}
		text.WriteByte(render.Text[i])
	}
	text.WriteString("\x1b[0m")
	fixed := []byte{0}
	if render.HasTitle {
		fixed[0] = 1
	}
	fixed = appendStr(fixed, render.Title)
	s.out.send(serverMsgPrompt, fixed, []byte(text.String()))
}

// handleEdit runs a completion op on the frontend's line, in the session's
// completion state.
func (s *server) handleEdit(state *TermState, request uint32, r *serverReader) (byte, []byte) {
	op, cursor := editOpKind(r.u8()), r.u32()
	line := r.rest()
	if r.bad {
		s.protocolError("malformed edit")
	}
	state.currentCommand = SourceText(line)
	state.index = graphemeCeil(state.currentCommand, ByteOffset(min(int(cursor), len(line))))
	state.bellToReply, state.bellRang = true, false
	switch op {
	case opComplete, opCompleteBack:
		direction := 1
		if op == opCompleteBack {
			direction = -1
		}
		if state.tabCycleActive && len(state.tabCycleMatches) > 0 {
			state.cycleTabCompletion(direction)
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
	default:
		return callFailed, []byte(fmt.Sprintf("edit: %d is not a completion op", op))
	}
	le := binary.LittleEndian
	b := le.AppendUint32(nil, uint32(state.index))
	cycling := byte(0)
	if state.tabCycleActive && len(state.tabCycleMatches) > 0 {
		cycling = 1
	}
	b = append(b, cycling)
	b = le.AppendUint32(b, uint32(int32(state.tabCycleIndex)))
	bell := byte(0)
	if state.bellRang {
		bell = 1
	}
	b = append(b, bell)
	matches := state.tabCycleMatches
	if cycling == 0 {
		matches = nil
	}
	b = le.AppendUint16(b, uint16(len(matches)))
	for _, m := range matches {
		b = appendStr(b, m)
	}
	return callYes, append(b, state.currentCommand...)
}

// editRun and editRunOps: the frontend runs a binding's quote as a block.
//
//	0x09 run_quote  u64 block id, u32 version, u32 quote, u8 with EditState
//	                (editRunOps), str before, after (rest). Answered like run:
//	                block_started, output, block_finished.
//	0x89 ops        u64 block id, u16 count, ops: what an editRunOps quote
//	                returned, sent before its block_finished, for the
//	                frontend to run on the line.

const (
	serverMsgRunQuote = 0x09
	serverMsgOps      = 0x89
)

// quoteRun reads a run_quote request.
func (s *server) quoteRun(call serverCall) serverRun {
	r := &serverReader{s: s, b: call.payload}
	id := uint64(r.u32()) | uint64(r.u32())<<32
	version, quote, withState := r.u32(), r.u32(), r.u8() != 0
	before := r.str()
	after := r.rest()
	if r.bad || id == 0 {
		s.protocolError("malformed run_quote")
	}
	run := serverRun{id: id, withState: withState, before: before, after: after}
	if version != s.editor.version || int(quote) >= len(s.editor.quotes) {
		run.staleQuote = true
	} else {
		run.quote = s.editor.quotes[quote]
	}
	return run
}

// runQuote runs an editRun or editRunOps quote as terminal msh does
// (runBindingQuote): in the foreground, with the terminal, no time limit.
// It returns whether the quote exited the shell, the block's exit code,
// and for editRunOps the ops the quote returned.
func (s *server) runQuote(state *TermState, run serverRun) (bool, int, []editOp) {
	if run.staleQuote {
		fmt.Fprintln(os.Stderr, "The binding changed before its quote ran; press its keys again.")
		return false, 1, nil
	}
	ev := &state.evalState
	stack := MShellStack{}
	if run.withState {
		info := NewDict()
		info.Items["before"] = MShellString{run.before}
		info.Items["after"] = MShellString{run.after}
		info.Items["keys"] = NewList(0)
		info.Items["mode"] = ev.builtinEnumValue("modeInsert")
		stack = append(stack, info)
	}
	ev.clearLastExitCode()
	result, err := ev.EvaluateQuote(run.quote, &stack, state.context, state.stdLibDefs)
	ev.printAbbrWarnings()
	var program []editOp
	switch {
	case err != nil:
		fmt.Fprintln(os.Stderr, terminalSafeText(err.Error(), true))
	case result.ExitCalled:
		return true, result.ExitCode, nil
	case !result.Success:
	case run.withState:
		var problem string
		program, _, problem = stackProgram(stack, "The quote of editRunOps", "Maybe[[EditOp]]", parseEditOps)
		fmt.Fprint(os.Stderr, terminalSafeText(problem, true))
	case len(stack) > 0:
		fmt.Fprintf(os.Stderr, "The quote of editRun left %d values on the stack; they were dropped.\n", len(stack))
	}
	ok := err == nil && result.Success
	return false, lineExitStatus(ok, ev.lastExitCode, ev.ranProcess), program
}

// sendOps sends what an editRunOps quote returned, with ids for any quotes
// in the config's table.
func (s *server) sendOps(block uint64, program []editOp) {
	b := binary.LittleEndian.AppendUint64(nil, block)
	b = binary.LittleEndian.AppendUint16(b, uint16(len(program)))
	for _, op := range program {
		b = appendEditOp(b, op, func(q *MShellQuotation) uint32 {
			s.editor.quotes = append(s.editor.quotes, q)
			return uint32(len(s.editor.quotes) - 1)
		})
	}
	s.out.send(serverMsgOps, b, nil)
}
