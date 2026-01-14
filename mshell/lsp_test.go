package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"go.lsp.dev/protocol"
)

func TestHoverRequestForBuiltin(t *testing.T) {
	path := filepath.Join("..", "tests", "stack_ops.msh")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read test document: %v", err)
	}

	lines := strings.Split(string(content), "\n")
	lineIndex := 1
	if len(lines) <= lineIndex {
		t.Fatalf("expected at least %d lines in %s", lineIndex+1, path)
	}

	column := strings.Index(lines[lineIndex], "swap")
	if column < 0 {
		t.Fatalf("expected to find 'swap' in line %d of %s", lineIndex+1, path)
	}

	uri := protocol.DocumentURI("file:///tests/stack_ops.msh")

	clientReader, clientWriter := io.Pipe()
	serverReader, serverWriter := io.Pipe()

	var wg sync.WaitGroup
	var runErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		runErr = RunLSP(clientReader, serverWriter)
		serverWriter.Close()
	}()

	output := bufio.NewReader(serverReader)

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"capabilities": map[string]any{},
		},
	})

	initResp := readLSPResponse(t, output)
	if initResp.Error != nil {
		t.Fatalf("initialize returned error: %+v", initResp.Error)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]any{
			"textDocument": map[string]any{
				"uri":        uri,
				"languageId": "mshell",
				"version":    1,
				"text":       string(content),
			},
		},
	})

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "textDocument/hover",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": lineIndex, "character": column + 1},
		},
	})

	hoverResp := readLSPResponse(t, output)
	if hoverResp.Error != nil {
		t.Fatalf("hover returned error: %+v", hoverResp.Error)
	}

	hoverPayload, err := json.Marshal(hoverResp.Result)
	if err != nil {
		t.Fatalf("failed to marshal hover result: %v", err)
	}

	var hover protocol.Hover
	if err := json.Unmarshal(hoverPayload, &hover); err != nil {
		t.Fatalf("failed to unmarshal hover result: %v", err)
	}

	expected := "```mshell\nswap :: (a b -- b a)\n```\n\nSwap the top two stack items."
	if hover.Contents.Value != expected {
		t.Fatalf("unexpected hover contents: %q", hover.Contents.Value)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "shutdown",
	})

	shutdownResp := readLSPResponse(t, output)
	if shutdownResp.Error != nil {
		t.Fatalf("shutdown returned error: %+v", shutdownResp.Error)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"method":  "exit",
	})

	clientWriter.Close()
	wg.Wait()
	if runErr != nil {
		t.Fatalf("RunLSP returned error: %v", runErr)
	}
}

func TestCompletionForVarRetrieve(t *testing.T) {
	doc := "foo!\nbar!\n@fo"
	uri := protocol.DocumentURI("file:///completion.msh")

	clientReader, clientWriter := io.Pipe()
	serverReader, serverWriter := io.Pipe()

	var wg sync.WaitGroup
	var runErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		runErr = RunLSP(clientReader, serverWriter)
		serverWriter.Close()
	}()

	output := bufio.NewReader(serverReader)

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"capabilities": map[string]any{},
		},
	})

	initResp := readLSPResponse(t, output)
	if initResp.Error != nil {
		t.Fatalf("initialize returned error: %+v", initResp.Error)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]any{
			"textDocument": map[string]any{
				"uri":        uri,
				"languageId": "mshell",
				"version":    1,
				"text":       doc,
			},
		},
	})

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "textDocument/completion",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 2, "character": 3},
		},
	})

	completionResp := readLSPResponse(t, output)
	if completionResp.Error != nil {
		t.Fatalf("completion returned error: %+v", completionResp.Error)
	}

	payload, err := json.Marshal(completionResp.Result)
	if err != nil {
		t.Fatalf("failed to marshal completion result: %v", err)
	}

	var items []protocol.CompletionItem
	if err := json.Unmarshal(payload, &items); err != nil {
		t.Fatalf("failed to unmarshal completion result: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 completion item, got %d", len(items))
	}

	item := items[0]
	if item.Label != "@foo" {
		t.Fatalf("unexpected completion label: %q", item.Label)
	}
	if item.Kind != protocol.CompletionItemKindVariable {
		t.Fatalf("unexpected completion kind: %v", item.Kind)
	}
	if item.TextEdit == nil {
		t.Fatalf("expected completion to include text edit")
	}
	if item.TextEdit.NewText != "@foo" {
		t.Fatalf("unexpected completion new text: %q", item.TextEdit.NewText)
	}
	if item.TextEdit.Range.Start.Line != 2 || item.TextEdit.Range.Start.Character != 0 {
		t.Fatalf("unexpected edit range start: %+v", item.TextEdit.Range.Start)
	}
	if item.TextEdit.Range.End.Line != 2 || item.TextEdit.Range.End.Character != 3 {
		t.Fatalf("unexpected edit range end: %+v", item.TextEdit.Range.End)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "shutdown",
	})

	shutdownResp := readLSPResponse(t, output)
	if shutdownResp.Error != nil {
		t.Fatalf("shutdown returned error: %+v", shutdownResp.Error)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"method":  "exit",
	})

	clientWriter.Close()
	wg.Wait()
	if runErr != nil {
		t.Fatalf("RunLSP returned error: %v", runErr)
	}
}

func TestCompletionForVarRetrieveBareAt(t *testing.T) {
	doc := "hello!\n@"
	uri := protocol.DocumentURI("file:///completion-bare.msh")

	clientReader, clientWriter := io.Pipe()
	serverReader, serverWriter := io.Pipe()

	var wg sync.WaitGroup
	var runErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		runErr = RunLSP(clientReader, serverWriter)
		serverWriter.Close()
	}()

	output := bufio.NewReader(serverReader)

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"capabilities": map[string]any{},
		},
	})

	initResp := readLSPResponse(t, output)
	if initResp.Error != nil {
		t.Fatalf("initialize returned error: %+v", initResp.Error)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]any{
			"textDocument": map[string]any{
				"uri":        uri,
				"languageId": "mshell",
				"version":    1,
				"text":       doc,
			},
		},
	})

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "textDocument/completion",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 1, "character": 1},
		},
	})

	completionResp := readLSPResponse(t, output)
	if completionResp.Error != nil {
		t.Fatalf("completion returned error: %+v", completionResp.Error)
	}

	payload, err := json.Marshal(completionResp.Result)
	if err != nil {
		t.Fatalf("failed to marshal completion result: %v", err)
	}

	var items []protocol.CompletionItem
	if err := json.Unmarshal(payload, &items); err != nil {
		t.Fatalf("failed to unmarshal completion result: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 completion item, got %d", len(items))
	}

	item := items[0]
	if item.Label != "@hello" {
		t.Fatalf("unexpected completion label: %q", item.Label)
	}
	if item.TextEdit == nil {
		t.Fatalf("expected completion to include text edit")
	}
	if item.TextEdit.NewText != "@hello" {
		t.Fatalf("unexpected completion new text: %q", item.TextEdit.NewText)
	}
	if item.TextEdit.Range.Start.Line != 1 || item.TextEdit.Range.Start.Character != 0 {
		t.Fatalf("unexpected edit range start: %+v", item.TextEdit.Range.Start)
	}
	if item.TextEdit.Range.End.Line != 1 || item.TextEdit.Range.End.Character != 1 {
		t.Fatalf("unexpected edit range end: %+v", item.TextEdit.Range.End)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "shutdown",
	})

	shutdownResp := readLSPResponse(t, output)
	if shutdownResp.Error != nil {
		t.Fatalf("shutdown returned error: %+v", shutdownResp.Error)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"method":  "exit",
	})

	clientWriter.Close()
	wg.Wait()
	if runErr != nil {
		t.Fatalf("RunLSP returned error: %v", runErr)
	}
}

func TestPrepareRenameVariable(t *testing.T) {
	doc := "foo!\n@foo\n"
	uri := protocol.DocumentURI("file:///prepare-rename.msh")

	clientReader, clientWriter := io.Pipe()
	serverReader, serverWriter := io.Pipe()

	var wg sync.WaitGroup
	var runErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		runErr = RunLSP(clientReader, serverWriter)
		serverWriter.Close()
	}()

	output := bufio.NewReader(serverReader)

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"capabilities": map[string]any{},
		},
	})

	initResp := readLSPResponse(t, output)
	if initResp.Error != nil {
		t.Fatalf("initialize returned error: %+v", initResp.Error)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]any{
			"textDocument": map[string]any{
				"uri":        uri,
				"languageId": "mshell",
				"version":    1,
				"text":       doc,
			},
		},
	})

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "textDocument/prepareRename",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 1, "character": 1},
		},
	})

	prepareResp := readLSPResponse(t, output)
	if prepareResp.Error != nil {
		t.Fatalf("prepareRename returned error: %+v", prepareResp.Error)
	}

	payload, err := json.Marshal(prepareResp.Result)
	if err != nil {
		t.Fatalf("failed to marshal prepareRename result: %v", err)
	}

	var rng protocol.Range
	if err := json.Unmarshal(payload, &rng); err != nil {
		t.Fatalf("failed to unmarshal prepareRename result: %v", err)
	}

	if rng.Start.Line != 1 || rng.Start.Character != 1 {
		t.Fatalf("unexpected range start: %+v", rng.Start)
	}
	if rng.End.Line != 1 || rng.End.Character != 4 {
		t.Fatalf("unexpected range end: %+v", rng.End)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "shutdown",
	})

	shutdownResp := readLSPResponse(t, output)
	if shutdownResp.Error != nil {
		t.Fatalf("shutdown returned error: %+v", shutdownResp.Error)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"method":  "exit",
	})

	clientWriter.Close()
	wg.Wait()
	if runErr != nil {
		t.Fatalf("RunLSP returned error: %v", runErr)
	}
}

func TestPrepareRenameVariableStore(t *testing.T) {
	doc := "foo!\n@foo\n"
	uri := protocol.DocumentURI("file:///prepare-rename-store.msh")

	clientReader, clientWriter := io.Pipe()
	serverReader, serverWriter := io.Pipe()

	var wg sync.WaitGroup
	var runErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		runErr = RunLSP(clientReader, serverWriter)
		serverWriter.Close()
	}()

	output := bufio.NewReader(serverReader)

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"capabilities": map[string]any{},
		},
	})

	initResp := readLSPResponse(t, output)
	if initResp.Error != nil {
		t.Fatalf("initialize returned error: %+v", initResp.Error)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]any{
			"textDocument": map[string]any{
				"uri":        uri,
				"languageId": "mshell",
				"version":    1,
				"text":       doc,
			},
		},
	})

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "textDocument/prepareRename",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 0, "character": 1},
		},
	})

	prepareResp := readLSPResponse(t, output)
	if prepareResp.Error != nil {
		t.Fatalf("prepareRename returned error: %+v", prepareResp.Error)
	}

	payload, err := json.Marshal(prepareResp.Result)
	if err != nil {
		t.Fatalf("failed to marshal prepareRename result: %v", err)
	}

	var rng protocol.Range
	if err := json.Unmarshal(payload, &rng); err != nil {
		t.Fatalf("failed to unmarshal prepareRename result: %v", err)
	}

	if rng.Start.Line != 0 || rng.Start.Character != 0 {
		t.Fatalf("unexpected range start: %+v", rng.Start)
	}
	if rng.End.Line != 0 || rng.End.Character != 3 {
		t.Fatalf("unexpected range end: %+v", rng.End)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "shutdown",
	})

	shutdownResp := readLSPResponse(t, output)
	if shutdownResp.Error != nil {
		t.Fatalf("shutdown returned error: %+v", shutdownResp.Error)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"method":  "exit",
	})

	clientWriter.Close()
	wg.Wait()
	if runErr != nil {
		t.Fatalf("RunLSP returned error: %v", runErr)
	}
}

func TestPrepareRenameRejectsNonVariable(t *testing.T) {
	doc := "dup\n"
	uri := protocol.DocumentURI("file:///prepare-rename-invalid.msh")

	clientReader, clientWriter := io.Pipe()
	serverReader, serverWriter := io.Pipe()

	var wg sync.WaitGroup
	var runErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		runErr = RunLSP(clientReader, serverWriter)
		serverWriter.Close()
	}()

	output := bufio.NewReader(serverReader)

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"capabilities": map[string]any{},
		},
	})

	initResp := readLSPResponse(t, output)
	if initResp.Error != nil {
		t.Fatalf("initialize returned error: %+v", initResp.Error)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]any{
			"textDocument": map[string]any{
				"uri":        uri,
				"languageId": "mshell",
				"version":    1,
				"text":       doc,
			},
		},
	})

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "textDocument/prepareRename",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 0, "character": 1},
		},
	})

	prepareResp := readLSPResponse(t, output)
	if prepareResp.Error == nil {
		t.Fatalf("expected prepareRename to return error")
	}
	if prepareResp.Error.Code != jsonrpcCodeInvalidParams {
		t.Fatalf("unexpected error code: %+v", prepareResp.Error)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "shutdown",
	})

	shutdownResp := readLSPResponse(t, output)
	if shutdownResp.Error != nil {
		t.Fatalf("shutdown returned error: %+v", shutdownResp.Error)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"method":  "exit",
	})

	clientWriter.Close()
	wg.Wait()
	if runErr != nil {
		t.Fatalf("RunLSP returned error: %v", runErr)
	}
}

func TestRenameGlobalVariable(t *testing.T) {
	doc := "foo!\n@foo\n"
	uri := protocol.DocumentURI("file:///rename-global.msh")

	clientReader, clientWriter := io.Pipe()
	serverReader, serverWriter := io.Pipe()

	var wg sync.WaitGroup
	var runErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		runErr = RunLSP(clientReader, serverWriter)
		serverWriter.Close()
	}()

	output := bufio.NewReader(serverReader)

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"capabilities": map[string]any{},
		},
	})

	initResp := readLSPResponse(t, output)
	if initResp.Error != nil {
		t.Fatalf("initialize returned error: %+v", initResp.Error)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]any{
			"textDocument": map[string]any{
				"uri":        uri,
				"languageId": "mshell",
				"version":    1,
				"text":       doc,
			},
		},
	})

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "textDocument/rename",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 1, "character": 2},
			"newName":      "bar",
		},
	})

	renameResp := readLSPResponse(t, output)
	if renameResp.Error != nil {
		t.Fatalf("rename returned error: %+v", renameResp.Error)
	}

	payload, err := json.Marshal(renameResp.Result)
	if err != nil {
		t.Fatalf("failed to marshal rename result: %v", err)
	}

	var edit protocol.WorkspaceEdit
	if err := json.Unmarshal(payload, &edit); err != nil {
		t.Fatalf("failed to unmarshal rename result: %v", err)
	}

	edits, ok := edit.Changes[uri]
	if !ok {
		t.Fatalf("expected edits for %s, got %#v", uri, edit.Changes)
	}
	if len(edits) != 2 {
		t.Fatalf("expected 2 edits, got %d", len(edits))
	}

	var seenStore, seenRetrieve bool
	for _, e := range edits {
		switch e.Range.Start.Line {
		case 0:
			if e.Range.Start.Character != 0 || e.Range.End.Character != 4 {
				t.Fatalf("unexpected range for store edit: %+v", e.Range)
			}
			if e.NewText != "bar!" {
				t.Fatalf("unexpected new text for store edit: %q", e.NewText)
			}
			seenStore = true
		case 1:
			if e.Range.Start.Character != 0 || e.Range.End.Character != 4 {
				t.Fatalf("unexpected range for retrieve edit: %+v", e.Range)
			}
			if e.NewText != "@bar" {
				t.Fatalf("unexpected new text for retrieve edit: %q", e.NewText)
			}
			seenRetrieve = true
		default:
			t.Fatalf("unexpected edit line %d", e.Range.Start.Line)
		}
	}

	if !seenStore || !seenRetrieve {
		t.Fatalf("missing expected edits: store=%v retrieve=%v", seenStore, seenRetrieve)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "shutdown",
	})

	shutdownResp := readLSPResponse(t, output)
	if shutdownResp.Error != nil {
		t.Fatalf("shutdown returned error: %+v", shutdownResp.Error)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"method":  "exit",
	})

	clientWriter.Close()
	wg.Wait()
	if runErr != nil {
		t.Fatalf("RunLSP returned error: %v", runErr)
	}
}

func TestRenameVariableInsideDefinition(t *testing.T) {
	doc := "foo!\ndef sample ( -- )\n  foo!\n  @foo\nend\n@foo\n"
	uri := protocol.DocumentURI("file:///rename-def.msh")

	clientReader, clientWriter := io.Pipe()
	serverReader, serverWriter := io.Pipe()

	var wg sync.WaitGroup
	var runErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		runErr = RunLSP(clientReader, serverWriter)
		serverWriter.Close()
	}()

	output := bufio.NewReader(serverReader)

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"capabilities": map[string]any{},
		},
	})

	initResp := readLSPResponse(t, output)
	if initResp.Error != nil {
		t.Fatalf("initialize returned error: %+v", initResp.Error)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]any{
			"textDocument": map[string]any{
				"uri":        uri,
				"languageId": "mshell",
				"version":    1,
				"text":       doc,
			},
		},
	})

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "textDocument/rename",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 3, "character": 4},
			"newName":      "baz",
		},
	})

	renameResp := readLSPResponse(t, output)
	if renameResp.Error != nil {
		t.Fatalf("rename returned error: %+v", renameResp.Error)
	}

	payload, err := json.Marshal(renameResp.Result)
	if err != nil {
		t.Fatalf("failed to marshal rename result: %v", err)
	}

	var edit protocol.WorkspaceEdit
	if err := json.Unmarshal(payload, &edit); err != nil {
		t.Fatalf("failed to unmarshal rename result: %v", err)
	}

	edits, ok := edit.Changes[uri]
	if !ok {
		t.Fatalf("expected edits for %s, got %#v", uri, edit.Changes)
	}
	if len(edits) != 2 {
		t.Fatalf("expected 2 edits, got %d", len(edits))
	}

	for _, e := range edits {
		if e.Range.Start.Line != 2 && e.Range.Start.Line != 3 {
			t.Fatalf("unexpected edit line %d", e.Range.Start.Line)
		}
		if e.Range.Start.Character != 2 || e.Range.End.Character != 6 {
			t.Fatalf("unexpected edit range: %+v", e.Range)
		}
		if e.Range.Start.Line == 2 && e.NewText != "baz!" {
			t.Fatalf("unexpected new text for store edit: %q", e.NewText)
		}
		if e.Range.Start.Line == 3 && e.NewText != "@baz" {
			t.Fatalf("unexpected new text for retrieve edit: %q", e.NewText)
		}
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "shutdown",
	})

	shutdownResp := readLSPResponse(t, output)
	if shutdownResp.Error != nil {
		t.Fatalf("shutdown returned error: %+v", shutdownResp.Error)
	}

	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"method":  "exit",
	})

	clientWriter.Close()
	wg.Wait()
	if runErr != nil {
		t.Fatalf("RunLSP returned error: %v", runErr)
	}
}

func sendLSPMessage(t *testing.T, w io.Writer, payload any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := io.WriteString(w, header); err != nil {
		t.Fatalf("failed to write header: %v", err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatalf("failed to write payload: %v", err)
	}
}

func readLSPResponse(t *testing.T, reader *bufio.Reader) responseMessage {
	t.Helper()
	headers := make(map[string]string)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("failed to read response header: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			t.Fatalf("invalid header line: %q", line)
		}
		headers[strings.ToLower(strings.TrimSpace(parts[0]))] = strings.TrimSpace(parts[1])
	}

	lengthStr, ok := headers["content-length"]
	if !ok {
		t.Fatalf("missing Content-Length header")
	}
	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		t.Fatalf("invalid Content-Length value %q: %v", lengthStr, err)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		t.Fatalf("failed to read response payload: %v", err)
	}

	var resp responseMessage
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	return resp
}
