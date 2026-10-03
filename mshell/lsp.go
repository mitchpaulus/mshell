package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"
	"unicode/utf8"

	"go.lsp.dev/protocol"
)

const jsonrpcVersion = "2.0"

const (
	jsonrpcCodeParseError     = -32700
	jsonrpcCodeMethodNotFound = -32601
	jsonrpcCodeInvalidParams  = -32602
	jsonrpcCodeInternalError  = -32603
)

var errExitBeforeShutdown = errors.New("exit received before shutdown")

type lspServer struct {
	in           *bufio.Reader
	out          *bufio.Writer
	writeMu      sync.Mutex
	documents    map[protocol.DocumentURI]*lspDocument
	shutdown     bool
	builtins     map[string]*builtinInfo
	lexer        *Lexer
	parser       *MShellParser
	pathBins     IPathBinManager
	varNames     map[string]struct{}
	envNames     map[string]struct{}
	candsBuf     []string
	stdlibDefs   []MShellDefinition
	// startupDecls are the startup files' `type` and `enum` declarations.
	startupDecls []MShellParseItem
	// startupErrs are errors in the startup files that every document
	// shows at its first line: an init file that does not parse, and the
	// startup declarations' errors (CoreBase.StartupErrors).
	startupErrs []string
	// coreBase is the type checker's base, built from the startup files on
	// first use and shared by every check.
	coreBase     *CoreBase
	coreBaseOnce sync.Once
	// startupFiles are the startup files one by one, in load order. A
	// document that is one of them is checked with only the files before it
	// (startupFilesBefore), in a base of its own (prefixBases[n] for the
	// first n files, built on first use).
	startupFiles []lspStartupFile
	prefixMu     sync.Mutex
	prefixBases  map[int]*CoreBase
	builtinSigs  map[string][]string // name -> formatted "(in -- out)" sigs from the type checker
	stdlibHover  map[string][]string // name -> formatted sigs for stdlib defs

	// diagMu guards diag, the diagnostics state of each open document
	// (scheduleDiagnostics).
	diagMu sync.Mutex
	diag   map[protocol.DocumentURI]*lspDiagState
}

// lspDiagState is a document's diagnostics in progress. At most one check
// of a document runs at a time; an edit made meanwhile leaves its text in
// next, and the running check takes it when done, so only the newest text
// is checked again. version counts edits: a result is published only if
// no edit came after the text it checked, and the document is still open.
type lspDiagState struct {
	running bool
	pending bool
	next    string
	version uint64
	closed  bool
}

type lspDocument struct {
	Text  string
	Lines []string
}

func (d *lspDocument) setText(text string) {
	d.Text = text
	lines := d.Lines[:0]
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, text[start:i])
			start = i + 1
		}
	}
	if start <= len(text) {
		lines = append(lines, text[start:])
	}
	d.Lines = lines
}

// LSP positions count UTF-16 code units in a line; tokens carry 1-based
// lines and columns in runes. These convert between the two, reading only
// the line concerned.

// runeCol is the 0-based rune column of an LSP position on its line.
func (d *lspDocument) runeCol(p protocol.Position) int {
	if int(p.Line) >= len(d.Lines) {
		return int(p.Character)
	}
	units, col := uint32(0), 0
	for _, r := range d.Lines[p.Line] {
		if units >= p.Character {
			break
		}
		units += uint32(utf16.RuneLen(r))
		col++
	}
	return col
}

// position is the LSP position of 0-based line and rune column col.
func (d *lspDocument) position(line, col int) protocol.Position {
	if line < 0 {
		line = 0
	}
	if line >= len(d.Lines) {
		return protocol.Position{Line: uint32(line), Character: uint32(max(col, 0))}
	}
	units := uint32(0)
	i := 0
	for _, r := range d.Lines[line] {
		if i >= col {
			break
		}
		units += uint32(utf16.RuneLen(r))
		i++
	}
	return protocol.Position{Line: uint32(line), Character: units}
}

// tokenRange is the LSP range of a token, on the line it starts on.
func (d *lspDocument) tokenRange(tok Token) protocol.Range {
	col := tok.Column - 1
	return protocol.Range{
		Start: d.position(tok.Line-1, col),
		End:   d.position(tok.Line-1, col+utf8.RuneCountInString(tok.Lexeme)),
	}
}

type builtinInfo struct {
	Name        string
	Description string
	Signatures  []string
	Kind        string // optional tag: "builtin", "stdlib", "user"
}

type jsonrpcMessage struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

type responseMessage struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Result  any              `json:"result,omitempty"`
	Error   *responseError   `json:"error,omitempty"`
}

type notificationMessage struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type lspRequestError struct {
	code    int
	message string
}

func (e *lspRequestError) Error() string {
	return e.message
}

func newLSPError(code int, format string, args ...any) *lspRequestError {
	return &lspRequestError{code: code, message: fmt.Sprintf(format, args...)}
}

// RunLSP executes the language server using stdio transport.
func RunLSP(in io.Reader, out io.Writer) error {
	builtins := defaultBuiltinInfo()
	if len(builtins) == 0 {
		logLSP("no builtin hover entries configured")
	}

	lexer := NewLexer("", nil)
	parser := NewMShellParser(lexer)
	pathBins := NewPathBinManager()

	server := &lspServer{
		in:        bufio.NewReader(in),
		out:       bufio.NewWriter(out),
		documents: make(map[protocol.DocumentURI]*lspDocument),
		builtins:  builtins,
		lexer:     lexer,
		parser:    parser,
		pathBins:  pathBins,
		varNames:  make(map[string]struct{}),
		envNames:  make(map[string]struct{}),
	}

	if files, startupErrs, err := loadStartupFilesForLSP(); err != nil {
		logLSP(fmt.Sprintf("type-check diagnostics: stdlib unavailable (%v); proceeding without stdlib sigs", err))
	} else {
		server.startupFiles, server.startupErrs = files, startupErrs
		server.stdlibDefs, server.startupDecls = joinStartupFiles(files)
	}

	server.builtinSigs, server.stdlibHover = buildHoverIndex(server.base(), server.stdlibDefs)

	return server.run()
}

// buildHoverIndex formats the signatures of the builtins and of the
// startup files' definitions, keyed by name. It formats in an overlay, so
// the base stays frozen.
func buildHoverIndex(base *CoreBase, stdlibDefs []MShellDefinition) (map[string][]string, map[string][]string) {
	arena := base.arena.Overlay()
	rel := NewRelations(arena)
	std := make(map[string]bool, len(stdlibDefs))
	for i := range stdlibDefs {
		std[stdlibDefs[i].Name] = true
	}
	builtinSigs := make(map[string][]string)
	stdlibHover := make(map[string][]string, len(stdlibDefs))
	for id, sigs := range base.table.byName {
		if len(sigs) == 0 {
			continue
		}
		name := base.names.Name(NameId(id))
		formatted := make([]string, len(sigs))
		for i := range sigs {
			formatted[i] = formatCoreSig(arena, base.names, rel, &sigs[i])
		}
		if std[name] {
			stdlibHover[name] = formatted
		} else {
			builtinSigs[name] = formatted
		}
	}
	// A word typed partly by the table and partly by the walker (the dict
	// form of urlEncode) shows both.
	for name, sigs := range coreWalkerSigs {
		for _, s := range sigs {
			if !slices.Contains(builtinSigs[name], s) {
				builtinSigs[name] = append(builtinSigs[name], s)
			}
		}
	}
	return builtinSigs, stdlibHover
}

// lspStartupFile is one startup file's definitions and declarations.
type lspStartupFile struct {
	path  string
	defs  []MShellDefinition
	decls []MShellParseItem
}

// loadStartupForLSP reads the startup files for their definitions and
// declarations, as a script run from the command line sees them: the
// standard library (honoring MSHSTDLIB), and the user's init file (honoring
// MSHINIT) if it is there and parses. Bodies are not evaluated; the checker
// needs only the signatures and declarations.
func loadStartupForLSP() ([]MShellDefinition, []MShellParseItem, error) {
	files, _, err := loadStartupFilesForLSP()
	defs, decls := joinStartupFiles(files)
	return defs, decls, err
}

// joinStartupFiles puts files' definitions and declarations together, in
// load order.
func joinStartupFiles(files []lspStartupFile) ([]MShellDefinition, []MShellParseItem) {
	var defs []MShellDefinition
	var decls []MShellParseItem
	for _, f := range files {
		defs = append(defs, f.defs...)
		decls = append(decls, f.decls...)
	}
	return defs, decls
}

// loadStartupFilesForLSP is loadStartupForLSP, file by file, also giving
// the error of an init file that does not parse; the server goes on
// without its definitions, and shows the error on every document, as the
// command line fails on it. The init file is listed either way, so a
// document that is the init file is still known as one.
func loadStartupFilesForLSP() ([]lspStartupFile, []string, error) {
	stdlibSpec, initSpec, err := getStartupFileSpecs(startupLoadOptions{
		version:           mshellVersion,
		allowEnvOverrides: true,
	})
	if err != nil {
		return nil, nil, err
	}
	source, err := os.ReadFile(stdlibSpec.path)
	if err != nil {
		return nil, nil, err
	}
	parsed, err := parseMShellInput(string(source), &TokenFile{stdlibSpec.path})
	if err != nil {
		return nil, nil, err
	}
	files := []lspStartupFile{{path: stdlibSpec.path, defs: parsed.Definitions, decls: declarationItems(parsed.Items)}}
	var startupErrs []string
	if source, err := os.ReadFile(initSpec.path); err == nil {
		init := lspStartupFile{path: initSpec.path}
		if parsed, err := parseMShellInput(string(source), &TokenFile{initSpec.path}); err == nil {
			init.defs, init.decls = parsed.Definitions, declarationItems(parsed.Items)
		} else {
			logLSP(fmt.Sprintf("init file %s does not parse (%v); proceeding without it", initSpec.path, err))
			startupErrs = append(startupErrs, fmt.Sprintf("the init file %s does not parse: %v", initSpec.path, err))
		}
		files = append(files, init)
	}
	return files, startupErrs, nil
}

func (s *lspServer) run() error {
	for {
		payload, err := s.readMessage()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		if len(payload) == 0 {
			continue
		}

		var msg jsonrpcMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			_ = s.sendParseError(err)
			continue
		}

		shouldExit, handleErr := s.handleMessage(&msg)
		if handleErr != nil {
			if msg.ID != nil {
				_ = s.sendErrorResponse(msg.ID, jsonrpcCodeInternalError, handleErr.Error())
			} else {
				logLSP(fmt.Sprintf("error handling %s: %v", msg.Method, handleErr))
			}
		}

		if shouldExit {
			return handleErr
		}
	}
}

func (s *lspServer) readMessage() ([]byte, error) {
	contentLength := 0
	lengthSet := false

	for {
		line, err := s.in.ReadString('\n')
		if err != nil {
			return nil, err
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}

		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "content-length:") {
			value := strings.TrimSpace(line[len("Content-Length:"):])
			length, convErr := strconv.Atoi(value)
			if convErr != nil {
				return nil, fmt.Errorf("invalid Content-Length: %w", convErr)
			}
			contentLength = length
			lengthSet = true
		}
	}

	if !lengthSet {
		return nil, errors.New("missing Content-Length header")
	}

	if contentLength < 0 {
		return nil, fmt.Errorf("negative Content-Length: %d", contentLength)
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(s.in, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// handleMessage routes a single JSON-RPC request/notification to the appropriate handler.
// The bool return indicates whether the server should exit after processing the message.
// The error return propagates any failure that should terminate the LSP event loop.
func (s *lspServer) handleMessage(msg *jsonrpcMessage) (bool, error) {
	switch msg.Method {
	case "initialize":
		if msg.ID == nil {
			logLSP("initialize request missing id")
			return false, nil
		}
		var params protocol.InitializeParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			_ = s.sendErrorResponse(msg.ID, jsonrpcCodeInvalidParams, fmt.Sprintf("invalid initialize params: %v", err))
			return false, nil
		}
		result := protocol.InitializeResult{
			Capabilities: protocol.ServerCapabilities{
				TextDocumentSync:  protocol.TextDocumentSyncKindFull,
				HoverProvider:     true,
				CodeActionProvider: true,
				CompletionProvider: &protocol.CompletionOptions{
					TriggerCharacters: []string{"@", "$"},
				},
				RenameProvider: &protocol.RenameOptions{PrepareProvider: true},
			},
			ServerInfo: &protocol.ServerInfo{
				Name:    "mshell",
				Version: mshellVersion,
			},
		}
		return false, s.sendResult(msg.ID, result)
	case "initialized":
		return false, nil
	case "shutdown":
		if msg.ID != nil {
			if err := s.sendResult(msg.ID, nil); err != nil {
				return false, err
			}
		}
		s.shutdown = true
		return false, nil
	case "exit":
		if s.shutdown {
			return true, nil
		}
		return true, errExitBeforeShutdown
	case "textDocument/didOpen":
		var params protocol.DidOpenTextDocumentParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			logLSP(fmt.Sprintf("invalid didOpen params: %v", err))
			return false, nil
		}
		s.updateDocument(params.TextDocument.URI, params.TextDocument.Text)
		return false, nil
	case "textDocument/didChange":
		var params protocol.DidChangeTextDocumentParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			logLSP(fmt.Sprintf("invalid didChange params: %v", err))
			return false, nil
		}
		if len(params.ContentChanges) == 0 {
			return false, nil
		}
		change := params.ContentChanges[len(params.ContentChanges)-1]
		s.updateDocument(params.TextDocument.URI, change.Text)
		return false, nil
	case "textDocument/didClose":
		var params protocol.DidCloseTextDocumentParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			logLSP(fmt.Sprintf("invalid didClose params: %v", err))
			return false, nil
		}
		delete(s.documents, params.TextDocument.URI)
		s.closeDiagnostics(params.TextDocument.URI)
		// Clear any diagnostics the client was showing for this doc.
		_ = s.writeNotification("textDocument/publishDiagnostics", protocol.PublishDiagnosticsParams{
			URI:         params.TextDocument.URI,
			Diagnostics: []protocol.Diagnostic{},
		})
		return false, nil
	case "textDocument/hover":
		if msg.ID == nil {
			logLSP("hover request missing id")
			return false, nil
		}
		var params protocol.HoverParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			_ = s.sendErrorResponse(msg.ID, jsonrpcCodeInvalidParams, fmt.Sprintf("invalid hover params: %v", err))
			return false, nil
		}
		hover, ok := s.hover(params)
		if !ok {
			return false, s.sendResult(msg.ID, nil)
		}
		return false, s.sendResult(msg.ID, hover)
	case "textDocument/completion":
		if msg.ID == nil {
			logLSP("completion request missing id")
			return false, nil
		}
		var params protocol.CompletionParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			_ = s.sendErrorResponse(msg.ID, jsonrpcCodeInvalidParams, fmt.Sprintf("invalid completion params: %v", err))
			return false, nil
		}
		items, ok := s.completion(params)
		if !ok {
			return false, s.sendResult(msg.ID, []protocol.CompletionItem{})
		}
		return false, s.sendResult(msg.ID, items)
	case "textDocument/codeAction":
		if msg.ID == nil {
			logLSP("codeAction request missing id")
			return false, nil
		}
		var params protocol.CodeActionParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			_ = s.sendErrorResponse(msg.ID, jsonrpcCodeInvalidParams, fmt.Sprintf("invalid codeAction params: %v", err))
			return false, nil
		}
		return false, s.sendResult(msg.ID, s.codeActions(params))
	case "textDocument/prepareRename":
		if msg.ID == nil {
			logLSP("prepareRename request missing id")
			return false, nil
		}
		var params protocol.PrepareRenameParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			_ = s.sendErrorResponse(msg.ID, jsonrpcCodeInvalidParams, fmt.Sprintf("invalid prepareRename params: %v", err))
			return false, nil
		}
		rng, err := s.prepareRename(params)
		if err != nil {
			var lspErr *lspRequestError
			if errors.As(err, &lspErr) {
				return false, s.sendErrorResponse(msg.ID, lspErr.code, lspErr.message)
			}
			return false, s.sendErrorResponse(msg.ID, jsonrpcCodeInternalError, err.Error())
		}
		return false, s.sendResult(msg.ID, rng)
	case "textDocument/rename":
		if msg.ID == nil {
			logLSP("rename request missing id")
			return false, nil
		}
		var params protocol.RenameParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			_ = s.sendErrorResponse(msg.ID, jsonrpcCodeInvalidParams, fmt.Sprintf("invalid rename params: %v", err))
			return false, nil
		}
		edit, err := s.rename(params)
		if err != nil {
			var lspErr *lspRequestError
			if errors.As(err, &lspErr) {
				return false, s.sendErrorResponse(msg.ID, lspErr.code, lspErr.message)
			}
			return false, s.sendErrorResponse(msg.ID, jsonrpcCodeInternalError, err.Error())
		}
		return false, s.sendResult(msg.ID, edit)
	default:
		if msg.ID != nil {
			_ = s.sendErrorResponse(msg.ID, jsonrpcCodeMethodNotFound, fmt.Sprintf("method %q not found", msg.Method))
		}
		return false, nil
	}
}

// sourceFixAll is the code action kind for fixing every error in a file.
const sourceFixAll protocol.CodeActionKind = "source.fixAll"

func (s *lspServer) codeActions(params protocol.CodeActionParams) []protocol.CodeAction {
	actions := []protocol.CodeAction{}
	doc, ok := s.documents[params.TextDocument.URI]
	if !ok {
		return actions
	}
	parser := NewMShellParser(NewLexer(doc.Text, nil))
	file, err := parser.ParseFile()
	if err != nil {
		return actions
	}
	actions = append(actions, s.typeFixActions(doc, file, params)...)
	if codeActionKindRequested(params.Context.Only, protocol.RefactorRewrite) {
		if a, ok := quoteLiteralsAction(doc, file, params); ok {
			actions = append(actions, a)
		}
	}
	return actions
}

// typeFixActions offers the fixes the core checker attaches to its errors:
// each one whose error is in the requested range, as a quick fix, and all
// of them at once.
func (s *lspServer) typeFixActions(doc *lspDocument, file *MShellFile, params protocol.CodeActionParams) []protocol.CodeAction {
	quick := codeActionKindRequested(params.Context.Only, protocol.QuickFix)
	all := codeActionKindRequested(params.Context.Only, sourceFixAll)
	if !quick && !all {
		return nil
	}
	uri := params.TextDocument.URI
	errs, arena, names := s.coreErrors(uri, file)
	var actions []protocol.CodeAction
	var every []protocol.TextEdit
	for _, e := range errs {
		if e.Fix.Kind == FixNone {
			continue
		}
		edit := typeFixEdit(doc, e.Fix)
		if all {
			every = append(every, edit)
		}
		if !quick {
			continue
		}
		if diag := typeErrorToDiagnostic(doc, e, arena, names); rangesOverlap(diag.Range, params.Range) {
			actions = append(actions, protocol.CodeAction{
				Title:       e.Fix.Title,
				Kind:        protocol.QuickFix,
				Diagnostics: []protocol.Diagnostic{diag},
				IsPreferred: true,
				Edit:        &protocol.WorkspaceEdit{Changes: map[protocol.DocumentURI][]protocol.TextEdit{uri: {edit}}},
			})
		}
	}
	if all && len(every) > 0 {
		actions = append(actions, protocol.CodeAction{
			Title: "Fix every `new` mark in this file",
			Kind:  sourceFixAll,
			Edit:  &protocol.WorkspaceEdit{Changes: map[protocol.DocumentURI][]protocol.TextEdit{uri: every}},
		})
	}
	return actions
}

// typeFixEdit is the text edit of a fix. Each position reads one line
// only, so fixing every mark of a long file is linear in its size.
func typeFixEdit(doc *lspDocument, f TypeFix) protocol.TextEdit {
	start := doc.position(f.At.Line-1, f.At.Column-1)
	if f.Kind == FixDelete {
		return protocol.TextEdit{Range: protocol.Range{Start: start, End: doc.position(f.Until.Line-1, f.Until.Column-1)}}
	}
	return protocol.TextEdit{Range: protocol.Range{Start: start, End: start}, NewText: f.Text}
}

// rangesOverlap reports whether two ranges share a position; a range that
// is a single position overlaps one that contains it.
func rangesOverlap(a, b protocol.Range) bool {
	before := func(x, y protocol.Position) bool {
		return x.Line < y.Line || x.Line == y.Line && x.Character < y.Character
	}
	return !before(a.End, b.Start) && !before(b.End, a.Start)
}

// quoteLiteralsAction offers to quote every bare word in the innermost
// list literal at the cursor.
func quoteLiteralsAction(doc *lspDocument, file *MShellFile, params protocol.CodeActionParams) (protocol.CodeAction, bool) {
	cursor, ok := lspPositionToRuneOffset(doc.Text, params.Range.Start)
	if !ok {
		return protocol.CodeAction{}, false
	}
	lists := collectRuntimeLists(file)

	var selected *MShellParseList
	for _, list := range lists {
		start := list.StartToken.Start
		end := list.EndToken.Start + utf8.RuneCountInString(list.EndToken.Lexeme)
		if cursor < start || cursor >= end {
			continue
		}
		if selected == nil || start > selected.StartToken.Start {
			selected = list
		}
	}
	if selected == nil {
		return protocol.CodeAction{}, false
	}

	literals := collectListLiterals(selected)
	if len(literals) == 0 {
		return protocol.CodeAction{}, false
	}
	edits := make([]protocol.TextEdit, 0, len(literals))
	for _, tok := range literals {
		edits = append(edits, protocol.TextEdit{
			Range:   tokenLSPRange(doc.Text, tok),
			NewText: "'" + tok.Lexeme + "'",
		})
	}

	return protocol.CodeAction{
		Title: "Quote all literals in list",
		Kind:  protocol.RefactorRewrite,
		Edit: &protocol.WorkspaceEdit{
			Changes: map[protocol.DocumentURI][]protocol.TextEdit{
				params.TextDocument.URI: edits,
			},
		},
	}, true
}

func collectRuntimeLists(file *MShellFile) []*MShellParseList {
	lists := make([]*MShellParseList, 0)
	collectRuntimeListsFromItems(&lists, file.Items)
	for i := range file.Definitions {
		collectRuntimeListsFromItems(&lists, file.Definitions[i].Items)
	}
	return lists
}

func collectRuntimeListsFromItems(dst *[]*MShellParseList, items []MShellParseItem) {
	for _, item := range items {
		switch v := item.(type) {
		case *MShellParseList:
			*dst = append(*dst, v)
			collectRuntimeListsFromItems(dst, v.Items)
		case *MShellParseDict:
			for _, kv := range v.Items {
				collectRuntimeListsFromItems(dst, kv.Value)
			}
		case *MShellParseQuote:
			collectRuntimeListsFromItems(dst, v.Items)
		case *MShellParseFormatString:
			for _, interpolation := range v.Interpolations {
				collectRuntimeListsFromItems(dst, interpolation)
			}
		case *MShellParsePrefixQuote:
			collectRuntimeListsFromItems(dst, v.Items)
		case *MShellParseIfBlock:
			collectRuntimeListsFromItems(dst, v.IfBody)
			for _, elseIf := range v.ElseIfs {
				collectRuntimeListsFromItems(dst, elseIf.Condition)
				collectRuntimeListsFromItems(dst, elseIf.Body)
			}
			collectRuntimeListsFromItems(dst, v.ElseBody)
		case *MShellParseMatchBlock:
			for _, arm := range v.Arms {
				collectRuntimeListsFromItems(dst, arm.Body)
			}
		case *MShellParseGrid:
			for _, row := range v.Rows {
				collectRuntimeListsFromItems(dst, row)
			}
		case *MShellIndexerList:
			collectRuntimeListsFromItems(dst, v.Indexers)
		}
	}
}

func collectListLiterals(list *MShellParseList) []Token {
	literals := make([]Token, 0)
	var collect func(*MShellParseList)
	collect = func(current *MShellParseList) {
		for _, item := range current.Items {
			switch v := item.(type) {
			case Token:
				if v.Type == LITERAL {
					literals = append(literals, v)
				}
			case *MShellParseList:
				collect(v)
			}
		}
	}
	collect(list)
	return literals
}

func codeActionKindRequested(only []protocol.CodeActionKind, action protocol.CodeActionKind) bool {
	if len(only) == 0 {
		return true
	}
	for _, requested := range only {
		if action == requested || strings.HasPrefix(string(action), string(requested)+".") {
			return true
		}
	}
	return false
}

func lspPositionToRuneOffset(text string, position protocol.Position) (int, bool) {
	line := uint32(0)
	character := uint32(0)
	runes := []rune(text)
	for offset, r := range runes {
		if line == position.Line && character == position.Character {
			return offset, true
		}
		if r == '\n' {
			if line == position.Line {
				return 0, false
			}
			line++
			character = 0
			continue
		}
		character += uint32(utf16.RuneLen(r))
	}
	if line == position.Line && character == position.Character {
		return len(runes), true
	}
	return 0, false
}

func tokenLSPRange(text string, tok Token) protocol.Range {
	start := runeOffsetToLSPPosition(text, tok.Start)
	end := runeOffsetToLSPPosition(text, tok.Start+utf8.RuneCountInString(tok.Lexeme))
	return protocol.Range{Start: start, End: end}
}

func runeOffsetToLSPPosition(text string, target int) protocol.Position {
	line := uint32(0)
	character := uint32(0)
	for offset, r := range []rune(text) {
		if offset >= target {
			break
		}
		if r == '\n' {
			line++
			character = 0
			continue
		}
		character += uint32(utf16.RuneLen(r))
	}
	return protocol.Position{Line: line, Character: character}
}

func (s *lspServer) sendResult(id *json.RawMessage, result any) error {
	if id == nil {
		return nil
	}
	if result == nil {
		result = json.RawMessage("null")
	}
	resp := responseMessage{
		JSONRPC: jsonrpcVersion,
		ID:      id,
		Result:  result,
	}
	return s.writeMessage(resp)
}

func (s *lspServer) sendErrorResponse(id *json.RawMessage, code int, message string) error {
	if id == nil {
		return nil
	}
	resp := responseMessage{
		JSONRPC: jsonrpcVersion,
		ID:      id,
		Error: &responseError{
			Code:    code,
			Message: message,
		},
	}
	return s.writeMessage(resp)
}

func (s *lspServer) sendParseError(err error) error {
	id := json.RawMessage("null")
	resp := responseMessage{
		JSONRPC: jsonrpcVersion,
		ID:      &id,
		Error: &responseError{
			Code:    jsonrpcCodeParseError,
			Message: fmt.Sprintf("invalid JSON: %v", err),
		},
	}
	return s.writeMessage(resp)
}

func (s *lspServer) writeMessage(resp responseMessage) error {
	payload, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	return s.writePayload(payload)
}

func (s *lspServer) writeNotification(method string, params any) error {
	n := notificationMessage{JSONRPC: jsonrpcVersion, Method: method, Params: params}
	payload, err := json.Marshal(n)
	if err != nil {
		return err
	}
	return s.writePayload(payload)
}

func (s *lspServer) writePayload(payload []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := fmt.Fprintf(s.out, "Content-Length: %d\r\n\r\n", len(payload)); err != nil {
		return err
	}
	if _, err := s.out.Write(payload); err != nil {
		return err
	}
	return s.out.Flush()
}

func (s *lspServer) updateDocument(uri protocol.DocumentURI, text string) {
	doc, exists := s.documents[uri]
	if !exists {
		doc = &lspDocument{}
		s.documents[uri] = doc
	}
	doc.setText(text)
	s.scheduleDiagnostics(uri, doc.Text)
}

// scheduleDiagnostics checks text off the event loop, so it keeps serving
// requests; the write side is mutex-guarded so the notification
// interleaves safely with response writes. Checks of one document do not
// overlap and are not queued: a check running when an edit arrives is
// followed by one check of the newest text, and its own result, now out
// of date, is dropped.
func (s *lspServer) scheduleDiagnostics(uri protocol.DocumentURI, text string) {
	s.diagMu.Lock()
	defer s.diagMu.Unlock()
	if s.diag == nil {
		s.diag = map[protocol.DocumentURI]*lspDiagState{}
	}
	st := s.diag[uri]
	if st == nil {
		st = &lspDiagState{}
		s.diag[uri] = st
	}
	st.version++
	st.closed = false
	if st.running {
		st.pending, st.next = true, text
		return
	}
	st.running = true
	go s.diagnosticsLoop(uri, st, text, st.version)
}

// closeDiagnostics stops a closed document's diagnostics: a check still
// running publishes nothing.
func (s *lspServer) closeDiagnostics(uri protocol.DocumentURI) {
	s.diagMu.Lock()
	defer s.diagMu.Unlock()
	if st := s.diag[uri]; st != nil {
		st.closed, st.pending, st.next = true, false, ""
		st.version++
		if !st.running {
			delete(s.diag, uri)
		}
	}
}

func (s *lspServer) diagnosticsLoop(uri protocol.DocumentURI, st *lspDiagState, text string, version uint64) {
	for {
		diags, ok := s.safeDiagnostics(uri, text)
		s.diagMu.Lock()
		current := version == st.version && !st.closed
		if current && ok {
			// Published under the lock, so a later check of this
			// document cannot publish first.
			s.publishDiagnostics(uri, diags)
		}
		if !st.pending {
			st.running = false
			if st.closed && s.diag[uri] == st {
				delete(s.diag, uri)
			}
			s.diagMu.Unlock()
			return
		}
		text, version = st.next, st.version
		st.pending, st.next = false, ""
		s.diagMu.Unlock()
	}
}

// safeDiagnostics is computeDiagnostics, reporting a panic in the checker
// instead of ending the server.
func (s *lspServer) safeDiagnostics(uri protocol.DocumentURI, text string) (diags []protocol.Diagnostic, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			logLSP(fmt.Sprintf("diagnostics panicked: %v\n%s", r, debug.Stack()))
			diags, ok = nil, false
		}
	}()
	return s.computeDiagnostics(uri, text), true
}

// publishDiagnosticsFor parses the document text and runs the static
// type checker, converting any parse or type errors into LSP
// diagnostics and sending a textDocument/publishDiagnostics
// notification. An empty diagnostic list clears prior diagnostics on
// the client. Runs on its own goroutine; it builds a private parser
// so it doesn't race with handlers using s.parser.
func (s *lspServer) publishDiagnosticsFor(uri protocol.DocumentURI, text string) {
	s.publishDiagnostics(uri, s.computeDiagnostics(uri, text))
}

func (s *lspServer) publishDiagnostics(uri protocol.DocumentURI, diags []protocol.Diagnostic) {
	if diags == nil {
		diags = []protocol.Diagnostic{}
	}
	params := protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diags,
	}
	if err := s.writeNotification("textDocument/publishDiagnostics", params); err != nil {
		logLSP(fmt.Sprintf("publishDiagnostics write failed: %v", err))
	}
}

// base returns the type checker's base, built once per server: each check
// is an overlay of it, since diagnostics run on every edit and may run
// concurrently.
func (s *lspServer) base() *CoreBase {
	s.coreBaseOnce.Do(func() { s.coreBase = NewCoreBase(s.stdlibDefs, s.startupDecls) })
	return s.coreBase
}

// baseFor returns the base a document is checked with, and the startup
// errors it shows. A document that is one of the startup files is the
// program, not a startup file: it is checked with only the startup files
// before it, as the command line runs it, so its own definitions are not
// also loaded as the startup file's.
func (s *lspServer) baseFor(uri protocol.DocumentURI) (*CoreBase, []string) {
	paths := make([]string, len(s.startupFiles))
	for i, f := range s.startupFiles {
		paths[i] = f.path
	}
	n := startupFilesBefore(documentPath(uri), paths...)
	if n == len(paths) {
		return s.base(), s.startupErrs
	}
	s.prefixMu.Lock()
	defer s.prefixMu.Unlock()
	if b := s.prefixBases[n]; b != nil {
		return b, nil
	}
	if s.prefixBases == nil {
		s.prefixBases = map[int]*CoreBase{}
	}
	defs, decls := joinStartupFiles(s.startupFiles[:n])
	b := NewCoreBase(defs, decls)
	s.prefixBases[n] = b
	return b, nil
}

// documentPath is the file a document URI names, or "" for one that is
// not a file.
func documentPath(uri protocol.DocumentURI) string {
	u, err := url.Parse(string(uri))
	if err != nil || u.Scheme != "file" {
		return ""
	}
	p := u.Path
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:] // /C:/x is C:/x
	}
	return filepath.FromSlash(p)
}

// coreErrors checks file, the text of the document uri.
func (s *lspServer) coreErrors(uri protocol.DocumentURI, file *MShellFile) ([]TypeError, *TypeArena, *NameTable) {
	base, _ := s.baseFor(uri)
	return base.Errors(file)
}

func (s *lspServer) computeDiagnostics(uri protocol.DocumentURI, text string) []protocol.Diagnostic {
	lexer := NewLexer(text, nil)
	parser := NewMShellParser(lexer)
	file, parseErr := parser.ParseFile()
	if parseErr != nil {
		doc := &lspDocument{}
		doc.setText(text)
		return []protocol.Diagnostic{parseErrorToDiagnostic(doc, parseErr)}
	}

	var diags []protocol.Diagnostic
	base, startupErrs := s.baseFor(uri)
	for _, msg := range append(startupErrs[:len(startupErrs):len(startupErrs)], base.StartupErrors()...) {
		diags = append(diags, protocol.Diagnostic{
			Range:    protocol.Range{End: protocol.Position{Character: 1}},
			Severity: protocol.DiagnosticSeverityError,
			Source:   "mshell",
			Message:  msg,
		})
	}
	base.diagnose(file, func(errs []TypeError, arena *TypeArena, names *NameTable) {
		if len(errs) == 0 {
			return
		}
		doc := &lspDocument{}
		doc.setText(text)
		for _, e := range errs {
			diags = append(diags, typeErrorToDiagnostic(doc, e, arena, names))
		}
	})
	return diags
}

func typeErrorToDiagnostic(doc *lspDocument, e TypeError, arena *TypeArena, names *NameTable) protocol.Diagnostic {
	line, col := max(e.Pos.Line-1, 0), max(e.Pos.Column-1, 0)
	n := utf8.RuneCountInString(e.Pos.Lexeme)
	if n == 0 {
		n = 1
	}
	start, end := doc.position(line, col), doc.position(line, col+n)
	if end == start {
		end.Character++
	}
	severity := protocol.DiagnosticSeverityError
	if e.Severity == SeverityInfo {
		severity = protocol.DiagnosticSeverityInformation
	}
	return protocol.Diagnostic{
		Range:    protocol.Range{Start: start, End: end},
		Severity: severity,
		Source:   "mshell",
		Message:  stripErrorPrefix(e.Format(arena, names)),
	}
}

// stripErrorPrefix removes the "type error at line X, column Y: " (or
// "type info at line ...") prefix that TypeError.Format adds. LSP
// clients already render the location from the diagnostic range, so
// the prefix is redundant.
func stripErrorPrefix(formatted string) string {
	if !strings.HasPrefix(formatted, "type error at line ") &&
		!strings.HasPrefix(formatted, "type info at line ") {
		return formatted
	}
	if idx := strings.Index(formatted, ": "); idx >= 0 {
		return formatted[idx+2:]
	}
	return formatted
}

func parseErrorToDiagnostic(doc *lspDocument, err error) protocol.Diagnostic {
	msg := err.Error()
	line, col := 0, 0
	// Many parser errors begin with "line:col:" style. Best-effort
	// extract; fall back to (0,0) on miss so the client still
	// renders the message somewhere.
	if l, c, ok := parseLineColPrefix(msg); ok {
		line, col = max(l-1, 0), max(c-1, 0)
	}
	start, end := doc.position(line, col), doc.position(line, col+1)
	if end == start {
		end.Character++
	}
	return protocol.Diagnostic{
		Range: protocol.Range{Start: start, End: end},
		Severity: protocol.DiagnosticSeverityError,
		Source:   "mshell",
		Message:  msg,
	}
}

func parseLineColPrefix(msg string) (int, int, bool) {
	// Accept either "<line>:<col>:" or "line N, column M" forms.
	if i := strings.Index(msg, ":"); i > 0 {
		if l, err := strconv.Atoi(msg[:i]); err == nil {
			rest := msg[i+1:]
			if j := strings.Index(rest, ":"); j > 0 {
				if c, err := strconv.Atoi(rest[:j]); err == nil {
					return l, c, true
				}
			}
		}
	}
	if i := strings.Index(msg, "line "); i >= 0 {
		rest := msg[i+5:]
		var l int
		n, _ := fmt.Sscanf(rest, "%d", &l)
		if n == 1 {
			if j := strings.Index(rest, "column "); j >= 0 {
				var c int
				m, _ := fmt.Sscanf(rest[j+7:], "%d", &c)
				if m == 1 {
					return l, c, true
				}
			}
			return l, 0, true
		}
	}
	return 0, 0, false
}

func (s *lspServer) hover(params protocol.HoverParams) (*protocol.Hover, bool) {
	doc, ok := s.documents[params.TextDocument.URI]
	if !ok {
		return nil, false
	}

	word, wordRange := doc.wordAt(params.Position)
	if word == "" {
		return nil, false
	}

	info := s.resolveHover(word, doc)
	if info == nil {
		return nil, false
	}

	content := buildHoverContent(info)
	if content == "" {
		return nil, false
	}

	hover := &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: content,
		},
	}
	rng := wordRange
	hover.Range = &rng
	return hover, true
}

// resolveHover collects everything we know about `word` and returns a
// merged builtinInfo. Resolution order:
//   1. Hardcoded entries (carry human descriptions).
//   2. Typed builtins from the type-checker table.
//   3. Stdlib definitions.
//   4. Definitions in the current file.
//   5. Bare BuiltInList membership (last-resort tag).
// Hardcoded descriptions are preferred over auto-formatted ones; typed
// signatures are preferred over hardcoded ones since they carry generic
// type variables.
func (s *lspServer) resolveHover(word string, doc *lspDocument) *builtinInfo {
	hard := s.builtins[word]
	typedSigs := s.builtinSigs[word]

	if hard != nil || len(typedSigs) > 0 {
		info := &builtinInfo{Name: word, Kind: "builtin"}
		if len(typedSigs) > 0 {
			info.Signatures = typedSigs
		} else if hard != nil {
			info.Signatures = hard.Signatures
		}
		if hard != nil {
			info.Description = hard.Description
		}
		return info
	}

	if sigs, ok := s.stdlibHover[word]; ok {
		return &builtinInfo{Name: word, Signatures: sigs, Kind: "stdlib"}
	}

	if sigs := s.lookupInFileDefSig(word, doc); len(sigs) > 0 {
		return &builtinInfo{Name: word, Signatures: sigs, Kind: "user-defined"}
	}

	if _, ok := BuiltInList[word]; ok {
		return &builtinInfo{Name: word, Kind: "builtin"}
	}

	return nil
}

// lookupInFileDefSig parses the current document and returns formatted
// signatures for any top-level definition named `name`. Returns nil if
// the document fails to parse or no matching def exists.
func (s *lspServer) lookupInFileDefSig(name string, doc *lspDocument) []string {
	return s.inFileDefSigs(doc.Text)[name]
}

func (s *lspServer) completion(params protocol.CompletionParams) ([]protocol.CompletionItem, bool) {
	doc, ok := s.documents[params.TextDocument.URI]
	if !ok {
		return nil, false
	}

	lexer := s.lexer
	lexer.resetInput(doc.Text)
	prevAllow := lexer.allowUnterminatedString
	lexer.allowUnterminatedString = true
	lexer.emitWhitespace = false
	lexer.emitComments = false
	defer func() {
		lexer.allowUnterminatedString = prevAllow
		lexer.emitWhitespace = false
		lexer.emitComments = false
		lexer.resetInput("")
	}()

	clear(s.varNames)
	clear(s.envNames)
	positionLine := int(params.Position.Line)
	positionChar := doc.runeCol(params.Position)
	var (
		varPrefix             string
		varFound              bool
		varToken              Token
		envPrefix             string
		envFound              bool
		envToken              Token
		literalPrefix         string
		literalFound          bool
		literalInListFirstPos bool
		literalToken          Token
		prevWasDef            bool
	)
	listStack := make([]bool, 0)
	defNames := make(map[string]struct{})

	for {
		tok := lexer.scanToken()
		if tok.Type == EOF {
			break
		}

		switch tok.Type {
		case LEFT_SQUARE_BRACKET:
			if len(listStack) > 0 && !listStack[len(listStack)-1] {
				listStack[len(listStack)-1] = true
			}
			listStack = append(listStack, false)
			prevWasDef = false
			continue
		case RIGHT_SQUARE_BRACKET:
			if len(listStack) > 0 {
				listStack = listStack[:len(listStack)-1]
			}
			prevWasDef = false
			continue
		}

		if tok.Type == VARSTORE {
			if len(tok.Lexeme) > 1 {
				name := tok.Lexeme[:len(tok.Lexeme)-1]
				s.varNames[name] = struct{}{}
			}
		}

		if tok.Type == VARRETRIEVE && tokenContainsPosition(tok, positionLine, positionChar) {
			varPrefix = completionPrefix(tok, positionChar)
			varToken = tok
			varFound = true
		}

		if tok.Type == ENVRETREIVE || tok.Type == ENVSTORE || tok.Type == ENVCHECK {
			atCursor := tokenContainsPosition(tok, positionLine, positionChar)
			// Only retrieves (`$NAME`) trigger completion; store/check
			// tokens carry a trailing `!`/`?` that the edit range would
			// clobber. Names from all forms still feed the candidate set.
			if atCursor && tok.Type == ENVRETREIVE {
				// The token under the cursor is the partial name being
				// typed; don't fold it into the in-file name set.
				envPrefix = completionPrefix(tok, positionChar)
				envToken = tok
				envFound = true
			} else if name := envVarName(tok); name != "" {
				s.envNames[name] = struct{}{}
			}
		}

		if tok.Type == LITERAL {
			inListFirstPos := len(listStack) > 0 && !listStack[len(listStack)-1]
			if tokenContainsPosition(tok, positionLine, positionChar) {
				literalPrefix = literalCompletionPrefix(tok, positionChar)
				literalToken = tok
				literalFound = true
				literalInListFirstPos = inListFirstPos
			}
			if prevWasDef {
				defNames[tok.Lexeme] = struct{}{}
			}
		}

		if len(listStack) > 0 && !listStack[len(listStack)-1] {
			listStack[len(listStack)-1] = true
		}

		prevWasDef = tok.Type == DEF
	}

	if varFound {
		return s.completeVariable(doc, varToken, varPrefix), true
	}

	if envFound {
		return s.completeEnv(doc, envToken, envPrefix), true
	}

	// A lone `$` lexes as a literal (not an env token until a name
	// follows); treat it as an empty-prefix env completion trigger.
	if literalFound && literalToken.Lexeme == "$" {
		return s.completeEnv(doc, literalToken, ""), true
	}

	if literalFound && literalPrefix != "" {
		if literalInListFirstPos {
			return s.completeListFirstLiteral(doc, literalToken, literalPrefix), true
		}
		return s.completeWord(literalToken, literalPrefix, doc, defNames), true
	}

	return []protocol.CompletionItem{}, true
}

// completeVariable returns @-prefixed completions for the variables
// collected during the lexer walk.
func (s *lspServer) completeVariable(doc *lspDocument, varToken Token, varPrefix string) []protocol.CompletionItem {
	candidates := s.candsBuf[:0]
	for name := range s.varNames {
		if strings.HasPrefix(name, varPrefix) {
			candidates = append(candidates, name)
		}
	}

	sort.Strings(candidates)
	editRange := doc.tokenRange(varToken)
	items := make([]protocol.CompletionItem, 0, len(candidates))
	for _, name := range candidates {
		label := "@" + name
		items = append(items, protocol.CompletionItem{
			Label: label,
			Kind:  protocol.CompletionItemKindVariable,
			TextEdit: &protocol.TextEdit{
				Range:   editRange,
				NewText: label,
			},
		})
	}

	s.candsBuf = candidates
	return items
}

// envVarName extracts the environment variable name from an ENVRETREIVE,
// ENVSTORE, or ENVCHECK token. ENVRETREIVE is `$NAME`, while ENVSTORE is
// `$NAME!` and ENVCHECK is `$NAME?`; the leading `$` and any trailing
// sigil are stripped.
func envVarName(tok Token) string {
	lexeme := tok.Lexeme
	if len(lexeme) < 2 || lexeme[0] != '$' {
		return ""
	}
	name := lexeme[1:]
	switch tok.Type {
	case ENVSTORE, ENVCHECK:
		name = name[:len(name)-1]
	}
	return name
}

// completeEnv returns `$`-prefixed completions for environment
// variables. Candidates are the actual process environment plus any env
// names already referenced in the current file (so a `$FOO!` write
// suggests `$FOO` later even if it is not yet exported to this process).
func (s *lspServer) completeEnv(doc *lspDocument, envToken Token, envPrefix string) []protocol.CompletionItem {
	candidates := s.candsBuf[:0]
	seen := make(map[string]struct{})

	consider := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		if !strings.HasPrefix(name, envPrefix) {
			return
		}
		seen[name] = struct{}{}
		candidates = append(candidates, name)
	}

	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		consider(name)
	}
	for name := range s.envNames {
		consider(name)
	}

	sort.Strings(candidates)
	editRange := doc.tokenRange(envToken)
	items := make([]protocol.CompletionItem, 0, len(candidates))
	for _, name := range candidates {
		label := "$" + name
		items = append(items, protocol.CompletionItem{
			Label: label,
			Kind:  protocol.CompletionItemKindVariable,
			TextEdit: &protocol.TextEdit{
				Range:   editRange,
				NewText: label,
			},
		})
	}

	s.candsBuf = candidates
	return items
}

// completeListFirstLiteral returns PATH-binary completions when the
// cursor sits on the first literal inside a `[ ... ]` (the typical
// argv position).
func (s *lspServer) completeListFirstLiteral(doc *lspDocument, literalToken Token, literalPrefix string) []protocol.CompletionItem {
	if s.pathBins == nil {
		return []protocol.CompletionItem{}
	}
	matches := s.pathBins.Matches(literalPrefix)
	if len(matches) == 0 {
		return []protocol.CompletionItem{}
	}

	editRange := doc.tokenRange(literalToken)
	items := make([]protocol.CompletionItem, 0, len(matches))
	for _, match := range matches {
		items = append(items, protocol.CompletionItem{
			Label: match,
			Kind:  protocol.CompletionItemKindFunction,
			TextEdit: &protocol.TextEdit{
				Range:   editRange,
				NewText: match,
			},
		})
	}
	return items
}

// completeWord offers callable names (in-file defs, stdlib defs, typed
// builtins, plus BuiltInList membership) for a literal at the cursor
// outside of a list-argv context. Items are deduplicated by label,
// with higher-priority sources winning on collision.
func (s *lspServer) completeWord(literalToken Token, prefix string, doc *lspDocument, inFileDefNames map[string]struct{}) []protocol.CompletionItem {
	// Priority high → low: in-file → stdlib → typed builtin → BuiltInList.
	// Higher-priority sources overwrite later ones via `seen`.
	type cand struct {
		label  string
		detail string
		kind   protocol.CompletionItemKind
	}
	seen := make(map[string]cand)

	add := func(label, detail string, kind protocol.CompletionItemKind) {
		if !strings.HasPrefix(label, prefix) {
			return
		}
		if _, exists := seen[label]; exists {
			return
		}
		seen[label] = cand{label: label, detail: detail, kind: kind}
	}

	// In-file defs: resolve sigs via the parser when possible; fall
	// back to the lexer-collected name set when parsing fails.
	inFileSigs := s.inFileDefSigs(doc.Text)
	if len(inFileSigs) > 0 {
		for name, sigs := range inFileSigs {
			detail := ""
			if len(sigs) > 0 {
				detail = name + " :: " + sigs[0]
			}
			add(name, detail, protocol.CompletionItemKindFunction)
		}
	}
	for name := range inFileDefNames {
		add(name, "", protocol.CompletionItemKindFunction)
	}

	for name, sigs := range s.stdlibHover {
		detail := ""
		if len(sigs) > 0 {
			detail = name + " :: " + sigs[0]
		}
		add(name, detail, protocol.CompletionItemKindFunction)
	}

	for name, sigs := range s.builtinSigs {
		detail := ""
		if len(sigs) > 0 {
			detail = name + " :: " + sigs[0]
		}
		add(name, detail, protocol.CompletionItemKindFunction)
	}

	for name := range BuiltInList {
		add(name, "", protocol.CompletionItemKindFunction)
	}

	if len(seen) == 0 {
		return []protocol.CompletionItem{}
	}

	labels := make([]string, 0, len(seen))
	for label := range seen {
		labels = append(labels, label)
	}
	sort.Strings(labels)

	editRange := doc.tokenRange(literalToken)
	items := make([]protocol.CompletionItem, 0, len(labels))
	for _, label := range labels {
		c := seen[label]
		items = append(items, protocol.CompletionItem{
			Label:  c.label,
			Detail: c.detail,
			Kind:   c.kind,
			TextEdit: &protocol.TextEdit{
				Range:   editRange,
				NewText: c.label,
			},
		})
	}
	return items
}

// inFileDefSigs parses the document and returns a name → formatted
// signatures map for every top-level definition. Returns nil on parse
// failure so callers can fall back to lexer-collected names.
func (s *lspServer) inFileDefSigs(text string) map[string][]string {
	s.parser.ResetInput(text)
	file, err := s.parser.ParseFile()
	if err != nil || file == nil {
		return nil
	}
	if len(file.Definitions) == 0 {
		return nil
	}
	// The file's own type declarations are declared first, so its
	// signatures may name them.
	c := s.base().newChecker()
	c.declareAll(file.Items, map[string]Token{})
	out := make(map[string][]string, len(file.Definitions))
	for i := range file.Definitions {
		def := &file.Definitions[i]
		sig := newCoreSig(c.arena, c.res.resolveSig(def.Inputs, def.Outputs))
		out[def.Name] = append(out[def.Name], formatCoreSig(c.arena, c.names, c.rel, &sig))
	}
	return out
}

func tokenEditRange(tok Token) protocol.Range {
	line := uint32(tok.Line - 1)
	startChar := uint32(tok.Column - 1)
	endChar := startChar + uint32(utf8.RuneCountInString(tok.Lexeme))
	return protocol.Range{
		Start: protocol.Position{Line: line, Character: startChar},
		End:   protocol.Position{Line: line, Character: endChar},
	}
}

type renameTarget struct {
	token renameTok
	scope []renameTok
	name  string
}

// renameTok is a token that may name a variable: a store, a load, or a
// name a match pattern binds (binding).
type renameTok struct {
	Token
	binding bool
}

// varName is the variable a token names, or "".
func (t renameTok) varName() string {
	if t.binding {
		return t.Lexeme
	}
	return variableNameFromToken(t.Token)
}

// nameRange is the range of the name inside the token: without `@` or `!`.
func (d *lspDocument) nameRange(t renameTok) protocol.Range {
	col := t.Column - 1
	n := utf8.RuneCountInString(t.Lexeme)
	switch {
	case t.binding:
	case t.Type == VARRETRIEVE:
		col, n = col+1, n-1
	case t.Type == VARSTORE:
		n--
	}
	return protocol.Range{Start: d.position(t.Line-1, col), End: d.position(t.Line-1, col+n)}
}

func (s *lspServer) findRenameTarget(doc *lspDocument, position protocol.Position) (*renameTarget, error) {
	parser := s.parser
	parser.ResetInput(doc.Text)

	file, err := parser.ParseFile()
	if err != nil {
		return nil, newLSPError(jsonrpcCodeInternalError, "failed to parse document: %v", err)
	}

	scopes := make([][]renameTok, 0, len(file.Definitions)+1)
	scopes = append(scopes, collectScopeTokens(file.Items))
	for _, def := range file.Definitions {
		scopes = append(scopes, collectScopeTokens(def.Items))
	}

	line := int(position.Line)
	character := doc.runeCol(position)

	for _, scope := range scopes {
		for _, tok := range scope {
			if !tokenContainsPosition(tok.Token, line, character) {
				continue
			}
			name := tok.varName()
			if name == "" {
				continue
			}
			return &renameTarget{token: tok, scope: scope, name: name}, nil
		}
	}

	return nil, newLSPError(jsonrpcCodeInvalidParams, "rename is only supported on variables")
}

func (s *lspServer) prepareRename(params protocol.PrepareRenameParams) (*protocol.Range, error) {
	doc, ok := s.documents[params.TextDocument.URI]
	if !ok {
		return nil, newLSPError(jsonrpcCodeInvalidParams, "document not found")
	}

	target, err := s.findRenameTarget(doc, params.Position)
	if err != nil {
		return nil, err
	}
	rng := doc.nameRange(target.token)
	return &rng, nil
}

func (s *lspServer) rename(params protocol.RenameParams) (*protocol.WorkspaceEdit, error) {
	doc, ok := s.documents[params.TextDocument.URI]
	if !ok {
		return nil, newLSPError(jsonrpcCodeInvalidParams, "document not found")
	}

	if !validVariableName(params.NewName) {
		return nil, newLSPError(jsonrpcCodeInvalidParams, "new name %q is not a valid variable identifier", params.NewName)
	}

	target, err := s.findRenameTarget(doc, params.Position)
	if err != nil {
		return nil, err
	}

	edits := make([]protocol.TextEdit, 0, len(target.scope))
	for _, tok := range target.scope {
		if tok.varName() != target.name {
			continue
		}
		newText := params.NewName
		if !tok.binding {
			var ok bool
			if newText, ok = replacementTextForToken(tok.Token, params.NewName); !ok {
				return nil, newLSPError(jsonrpcCodeInternalError, "unable to build replacement for token at %d:%d", tok.Line, tok.Column)
			}
		}
		edits = append(edits, protocol.TextEdit{Range: doc.tokenRange(tok.Token), NewText: newText})
	}

	if len(edits) == 0 {
		return nil, newLSPError(jsonrpcCodeInternalError, "no occurrences found to rename")
	}

	edit := &protocol.WorkspaceEdit{
		Changes: map[protocol.DocumentURI][]protocol.TextEdit{
			params.TextDocument.URI: edits,
		},
	}

	return edit, nil
}

func collectScopeTokens(items []MShellParseItem) []renameTok {
	tokens := make([]renameTok, 0)
	collectTokensFromItems(&tokens, items)
	return tokens
}

// collectTokensFromItems collects the variable stores and loads in items,
// in every nested body, and the names match patterns bind. Definitions are
// parsed at the top level and their bodies are collected separately, so a
// rename stays in one scope.
func collectTokensFromItems(dst *[]renameTok, items []MShellParseItem) {
	for _, item := range items {
		switch v := item.(type) {
		case Token:
			if v.Type == VARSTORE || v.Type == VARRETRIEVE {
				*dst = append(*dst, renameTok{Token: v})
			}
		case *MShellParseList:
			collectTokensFromItems(dst, v.Items)
		case *MShellParseDict:
			for _, kv := range v.Items {
				collectTokensFromItems(dst, kv.Value)
			}
		case *MShellParseQuote:
			collectTokensFromItems(dst, v.Items)
		case *MShellParsePrefixQuote:
			collectTokensFromItems(dst, v.Items)
		case *MShellParseFormatString:
			for _, interpolation := range v.Interpolations {
				collectTokensFromItems(dst, interpolation)
			}
		case *MShellIndexerList:
			collectTokensFromItems(dst, v.Indexers)
		case MShellVarstoreList:
			for _, t := range v.VarStores {
				*dst = append(*dst, renameTok{Token: t})
			}
		case *MShellParseIfBlock:
			collectTokensFromItems(dst, v.IfBody)
			for _, ei := range v.ElseIfs {
				collectTokensFromItems(dst, ei.Condition)
				collectTokensFromItems(dst, ei.Body)
			}
			collectTokensFromItems(dst, v.ElseBody)
		case *MShellParseMatchBlock:
			for _, arm := range v.Arms {
				collectPatternBindings(dst, arm.Pattern)
				collectTokensFromItems(dst, arm.Body)
			}
		case *MShellParseGrid:
			if v.GridMeta != nil {
				collectTokensFromItems(dst, []MShellParseItem{v.GridMeta})
			}
			for _, row := range v.Rows {
				collectTokensFromItems(dst, row)
			}
		}
	}
}

// collectPatternBindings collects the names a match pattern binds: a bare
// word that is not one of the pattern words, in the pattern or in the list
// and dict patterns inside it.
func collectPatternBindings(dst *[]renameTok, pattern []MShellParseItem) {
	for _, item := range pattern {
		switch v := item.(type) {
		case Token:
			if v.Type == LITERAL && !patternWords[v.Lexeme] && !strings.HasPrefix(v.Lexeme, "...") {
				*dst = append(*dst, renameTok{Token: v, binding: true})
			}
		case *MShellParseList:
			collectPatternBindings(dst, v.Items)
		case *MShellParseDict:
			for _, kv := range v.Items {
				collectPatternBindings(dst, kv.Value)
			}
		}
	}
}

func tokenContainsPosition(tok Token, line, character int) bool {
	if tok.Line-1 != line {
		return false
	}
	start := tok.Column - 1
	length := utf8.RuneCountInString(tok.Lexeme)
	end := start + length
	return character >= start && character <= end
}

func variableNameFromToken(tok Token) string {
	switch tok.Type {
	case VARSTORE:
		runes := []rune(tok.Lexeme)
		if len(runes) == 0 {
			return ""
		}
		return string(runes[:len(runes)-1])
	case VARRETRIEVE:
		runes := []rune(tok.Lexeme)
		if len(runes) == 0 {
			return ""
		}
		return string(runes[1:])
	default:
		return ""
	}
}

func replacementTextForToken(tok Token, name string) (string, bool) {
	switch tok.Type {
	case VARSTORE:
		return name + "!", true
	case VARRETRIEVE:
		return "@" + name, true
	default:
		return "", false
	}
}

func validVariableName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !isAllowedLiteral(r) {
			return false
		}
	}
	return true
}

func completionPrefix(token Token, positionChar int) string {
	startChar := token.Column - 1
	offset := positionChar - startChar
	if offset <= 1 {
		return ""
	}

	runes := []rune(token.Lexeme)
	if offset > len(runes) {
		offset = len(runes)
	}

	return string(runes[1:offset])
}

func literalCompletionPrefix(token Token, positionChar int) string {
	startChar := token.Column - 1
	offset := positionChar - startChar
	if offset <= 0 {
		return ""
	}

	runes := []rune(token.Lexeme)
	if offset > len(runes) {
		offset = len(runes)
	}

	return string(runes[:offset])
}

func (d *lspDocument) wordAt(pos protocol.Position) (string, protocol.Range) {
	lineIdx := int(pos.Line)
	if lineIdx < 0 || lineIdx >= len(d.Lines) {
		return "", protocol.Range{}
	}

	line := d.Lines[lineIdx]
	runes := []rune(line)
	if len(runes) == 0 {
		return "", protocol.Range{}
	}

	col := d.runeCol(pos)

	if col > len(runes) {
		return "", protocol.Range{}
	}
	if col == len(runes) {
		col--
	}
	if col < 0 || col >= len(runes) {
		return "", protocol.Range{}
	}

	if !isAllowedLiteral(runes[col]) {
		if col > 0 && isAllowedLiteral(runes[col-1]) {
			col--
		} else {
			return "", protocol.Range{}
		}
	}

	start := col
	for start > 0 && isAllowedLiteral(runes[start-1]) {
		start--
	}

	end := col + 1
	for end < len(runes) && isAllowedLiteral(runes[end]) {
		end++
	}

	word := string(runes[start:end])
	rng := protocol.Range{Start: d.position(lineIdx, start), End: d.position(lineIdx, end)}
	return word, rng
}

func buildHoverContent(info *builtinInfo) string {
	if info == nil {
		return ""
	}

	var builder strings.Builder
	if len(info.Signatures) > 0 {
		builder.WriteString("```mshell\n")
		for idx, sig := range info.Signatures {
			builder.WriteString(info.Name)
			sig = strings.TrimSpace(sig)
			if sig != "" {
				builder.WriteString(" :: ")
				builder.WriteString(sig)
			}
			if idx+1 < len(info.Signatures) {
				builder.WriteRune('\n')
			}
		}
		builder.WriteString("\n```")
	} else if info.Kind != "" {
		builder.WriteString("```mshell\n")
		builder.WriteString(info.Name)
		builder.WriteString("\n```")
	}

	if info.Kind != "" {
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString("_")
		builder.WriteString(info.Kind)
		builder.WriteString("_")
	}

	if info.Description != "" {
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString(info.Description)
	}

	return builder.String()
}

func defaultBuiltinInfo() map[string]*builtinInfo {
	return map[string]*builtinInfo{
		"dup": {
			Name:        "dup",
			Description: "Duplicate the top stack item.",
			Signatures:  []string{"(a -- a a)"},
		},
		"swap": {
			Name:        "swap",
			Description: "Swap the top two stack items.",
			Signatures:  []string{"(a b -- b a)"},
		},
		"len": {
			Name:        "len",
			Description: "Return the length of a string or list.",
			Signatures: []string{
				"([a] -- int)",
				"(str -- int)",
			},
		},
		"read": {
			Name:        "read",
			Description: "Read a line from stdin, leaving the line and success flag on the stack.",
			Signatures:  []string{"(-- str bool)"},
		},
		"prompt": {
			Name:        "prompt",
			Description: "Write a prompt to the controlling TTY and read a line from the controlling TTY.",
			Signatures:  []string{"(str -- str)"},
		},
		"stdin": {
			Name:        "stdin",
			Description: "Read stdin into a string.",
			Signatures:  []string{"(-- str)"},
		},
		"stack": {
			Name:        "stack",
			Description: "Print the stack at the current location.",
			Signatures:  []string{"(-- )"},
		},
	}
}

func logLSP(message string) {
	fmt.Fprintf(os.Stderr, "mshell lsp: %s\n", message)
}
