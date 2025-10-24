package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
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
	in        *bufio.Reader
	out       *bufio.Writer
	documents map[protocol.DocumentURI]*lspDocument
	shutdown  bool
	builtins  map[string]*builtinInfo
	lexer     *Lexer
	varNames  map[string]struct{}
	candsBuf  []string
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

type builtinInfo struct {
	Name        string
	Description string
	Signatures  []string
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
	Result  any      `json:"result,omitempty"`
	Error   *responseError   `json:"error,omitempty"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// RunLSP executes the language server using stdio transport.
func RunLSP(args []string, in io.Reader, out io.Writer) error {
	for _, arg := range args {
		if arg == "--stdio" {
			continue
		}
		return fmt.Errorf("unsupported LSP option %q", arg)
	}

	builtins := defaultBuiltinInfo()
	if len(builtins) == 0 {
		logLSP("no builtin hover entries configured")
	}

	server := &lspServer{
		in:        bufio.NewReader(in),
		out:       bufio.NewWriter(out),
		documents: make(map[protocol.DocumentURI]*lspDocument),
		builtins:  builtins,
		lexer:     NewLexer("", nil),
		varNames:  make(map[string]struct{}),
	}

	return server.run()
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
				TextDocumentSync: protocol.TextDocumentSyncKindFull,
				HoverProvider:    true,
				CompletionProvider: &protocol.CompletionOptions{
					TriggerCharacters: []string{"@"},
				},
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
	default:
		if msg.ID != nil {
			_ = s.sendErrorResponse(msg.ID, jsonrpcCodeMethodNotFound, fmt.Sprintf("method %q not found", msg.Method))
		}
		return false, nil
	}
}

func (s *lspServer) sendResult(id *json.RawMessage, result any) error {
	if id == nil {
		return nil
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

	info := s.builtins[word]
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

func (s *lspServer) completion(params protocol.CompletionParams) ([]protocol.CompletionItem, bool) {
	doc, ok := s.documents[params.TextDocument.URI]
	if !ok {
		return nil, false
	}

	lexer := s.lexer
	lexer.resetInput(doc.Text)
	prevAllow := lexer.allowUnterminatedString
	lexer.allowUnterminatedString = true
	defer func() {
		lexer.allowUnterminatedString = prevAllow
		lexer.emitWhitespace = false
		lexer.emitComments = false
		lexer.resetInput("")
	}()

	clear(s.varNames)
	positionLine := int(params.Position.Line)
	positionChar := int(params.Position.Character)
	var (
		prefix        string
		found         bool
		retrieveToken Token
	)

	for {
		tok := lexer.scanToken()
		if tok.Type == EOF {
			break
		}

		if tok.Type == VARSTORE {
			if len(tok.Lexeme) > 1 {
				name := tok.Lexeme[:len(tok.Lexeme)-1]
				s.varNames[name] = struct{}{}
			}
			continue
		}

		if tok.Type != VARRETRIEVE {
			continue
		}

		tokenLine := tok.Line - 1
		if tokenLine != positionLine {
			continue
		}

		startChar := tok.Column - 1
		length := utf8.RuneCountInString(tok.Lexeme)
		endChar := startChar + length
		if positionChar < startChar || positionChar > endChar {
			continue
		}

		prefix = completionPrefix(tok, positionChar)
		retrieveToken = tok
		found = true
	}

	if !found {
		return []protocol.CompletionItem{}, true
	}

	candidates := s.candsBuf[:0]
	for name := range s.varNames {
		if strings.HasPrefix(name, prefix) {
			candidates = append(candidates, name)
		}
	}

	sort.Strings(candidates)
	line := uint32(retrieveToken.Line - 1)
	startChar := uint32(retrieveToken.Column - 1)
	endChar := startChar + uint32(utf8.RuneCountInString(retrieveToken.Lexeme))
	editRange := protocol.Range{
		Start: protocol.Position{Line: line, Character: startChar},
		End:   protocol.Position{Line: line, Character: endChar},
	}
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

	return items, true
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

	col := max(0, int(pos.Character))

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
	rng := protocol.Range{
		Start: protocol.Position{Line: pos.Line, Character: uint32(start)},
		End:   protocol.Position{Line: pos.Line, Character: uint32(end)},
	}
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
		"stdin": {
			Name:        "stdin",
			Description: "Read stdin into a string.",
			Signatures:  []string{"(-- str)"},
		},
		".s": {
			Name:        ".s",
			Description: "Print the stack at the current location.",
			Signatures:  []string{"(-- )"},
		},
	}
}

func logLSP(message string) {
	fmt.Fprintf(os.Stderr, "mshell lsp: %s\n", message)
}
