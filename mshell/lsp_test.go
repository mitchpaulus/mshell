package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"

	"go.lsp.dev/protocol"
)

func TestHoverRequestForBuiltin(t *testing.T) {
	// Self-contained fixture so this test does not depend on the layout of
	// any shared script in tests/success. Hover over the `swap` token.
	content := "1 2 swap\n"
	lineIndex := 0
	column := strings.Index(content, "swap")
	if column < 0 {
		t.Fatalf("expected to find 'swap' in test document")
	}

	uri := protocol.DocumentURI("file:///stack_ops.msh")

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
				"text":       content,
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

	expected := "```mshell\nswap :: (a b -- b a)\n```\n\n_builtin_\n\nSwap the top two stack items."
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

func TestCompletionForEnvRetrieve(t *testing.T) {
	// A real process env var and an env var only referenced in the
	// file should both be offered, filtered by the typed prefix.
	t.Setenv("MSHELL_LSP_REAL", "1")
	doc := "1 $MSHELL_LSP_FILE!\n$MSHELL_LSP_"
	uri := protocol.DocumentURI("file:///completion-env.msh")

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

	// Cursor at end of `$MSHELL_LSP_` on line 1 (0-based).
	sendLSPMessage(t, clientWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "textDocument/completion",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 1, "character": 12},
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

	labels := make([]string, 0, len(items))
	for _, item := range items {
		labels = append(labels, item.Label)
		if item.Kind != protocol.CompletionItemKindVariable {
			t.Fatalf("unexpected completion kind for %q: %v", item.Label, item.Kind)
		}
		if item.TextEdit == nil || item.TextEdit.NewText != item.Label {
			t.Fatalf("unexpected text edit for %q: %+v", item.Label, item.TextEdit)
		}
	}

	want := []string{"$MSHELL_LSP_FILE", "$MSHELL_LSP_REAL"}
	if len(labels) != len(want) {
		t.Fatalf("expected labels %v, got %v", want, labels)
	}
	for i, label := range want {
		if labels[i] != label {
			t.Fatalf("expected labels %v, got %v", want, labels)
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

func TestCompletionForBinaryListFirstItem(t *testing.T) {
	tmpDir := t.TempDir()
	baseName := "lspbin"
	fileName := baseName
	if runtime.GOOS == "windows" {
		fileName = baseName + ".cmd"
	}
	binPath := filepath.Join(tmpDir, fileName)
	content := []byte("")
	if runtime.GOOS == "windows" {
		content = []byte("@echo off\r\n")
	}
	if err := os.WriteFile(binPath, content, 0644); err != nil {
		t.Fatalf("failed to write temp binary: %v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(binPath, 0755); err != nil {
			t.Fatalf("failed to chmod temp binary: %v", err)
		}
	}

	oldPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", tmpDir); err != nil {
		t.Fatalf("failed to set PATH: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
	})

	doc := "[ " + baseName
	uri := protocol.DocumentURI("file:///completion-bin.msh")

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
			"position": map[string]any{
				"line":      0,
				"character": len([]rune(doc)),
			},
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

	found := false
	for _, item := range items {
		if item.Label != fileName {
			continue
		}
		found = true
		if item.TextEdit == nil {
			t.Fatalf("expected completion to include text edit")
		}
		if item.TextEdit.NewText != fileName {
			t.Fatalf("unexpected completion new text: %q", item.TextEdit.NewText)
		}
		break
	}
	if !found {
		t.Fatalf("expected completion to include %q, got %v", fileName, items)
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

func TestCompletionWordOutsideLists(t *testing.T) {
	doc := "def addOne (int -- int) 1 + end\n1 addO"
	uri := protocol.DocumentURI("file:///completion-word.msh")

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
		"params":  map[string]any{"capabilities": map[string]any{}},
	})
	if r := readLSPResponse(t, output); r.Error != nil {
		t.Fatalf("initialize returned error: %+v", r.Error)
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
			"position":     map[string]any{"line": 1, "character": 6},
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
		t.Fatalf("expected 1 completion item for 'addO', got %d: %+v", len(items), items)
	}
	if items[0].Label != "addOne" {
		t.Fatalf("expected label 'addOne', got %q", items[0].Label)
	}
	if !strings.Contains(items[0].Detail, "(int -- int)") {
		t.Fatalf("expected detail to include sig, got %q", items[0].Detail)
	}
	if items[0].TextEdit == nil || items[0].TextEdit.NewText != "addOne" {
		t.Fatalf("expected TextEdit replacing with 'addOne', got %+v", items[0].TextEdit)
	}

	sendLSPMessage(t, clientWriter, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "shutdown"})
	if r := readLSPResponse(t, output); r.Error != nil {
		t.Fatalf("shutdown returned error: %+v", r.Error)
	}
	sendLSPMessage(t, clientWriter, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	clientWriter.Close()
	wg.Wait()
	if runErr != nil {
		t.Fatalf("RunLSP returned error: %v", runErr)
	}
}

func TestCompletionWordIncludesBuiltinAndStdlib(t *testing.T) {
	doc := "chu"
	uri := protocol.DocumentURI("file:///completion-word-multi.msh")

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
		"params":  map[string]any{"capabilities": map[string]any{}},
	})
	if r := readLSPResponse(t, output); r.Error != nil {
		t.Fatalf("initialize returned error: %+v", r.Error)
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
			"position":     map[string]any{"line": 0, "character": 3},
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

	labels := map[string]bool{}
	for _, it := range items {
		labels[it.Label] = true
	}
	// `chunk` and `chunkAcc` are in the stdlib; both should appear.
	if !labels["chunk"] {
		t.Fatalf("expected `chunk` in completions, got %+v", labels)
	}
	if !labels["chunkAcc"] {
		t.Fatalf("expected `chunkAcc` in completions, got %+v", labels)
	}

	sendLSPMessage(t, clientWriter, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "shutdown"})
	if r := readLSPResponse(t, output); r.Error != nil {
		t.Fatalf("shutdown returned error: %+v", r.Error)
	}
	sendLSPMessage(t, clientWriter, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	clientWriter.Close()
	wg.Wait()
	if runErr != nil {
		t.Fatalf("RunLSP returned error: %v", runErr)
	}
}

func TestBuildHoverIndexCoversTypedBuiltinsAndStdlib(t *testing.T) {
	stdlibDefs, _, err := loadStartupForLSP()
	if err != nil {
		t.Skipf("stdlib not available in test environment: %v", err)
	}

	builtinSigs, stdlibHover := buildHoverIndex(NewCoreBase(stdlibDefs, nil), stdlibDefs)

	// `over` is typed by the checker's walker, not its table, and is not in
	// defaultBuiltinInfo, so it exercises the walker's hover signatures.
	if sigs, ok := builtinSigs["over"]; !ok || len(sigs) == 0 {
		t.Fatalf("expected `over` in builtinSigs, got %v", sigs)
	} else if !strings.Contains(sigs[0], "--") {
		t.Fatalf("expected `over` sig to contain stack effect, got %q", sigs[0])
	}

	// A table entry is written as a def would declare it, `new` included.
	if sigs := builtinSigs["parseJson"]; len(sigs) == 0 || !strings.Contains(sigs[0], "-- new Json") {
		t.Fatalf("expected `parseJson` to give a new Json, got %v", sigs)
	}

	if sigs, ok := stdlibHover["chunk"]; !ok || len(sigs) == 0 {
		t.Fatalf("expected `chunk` in stdlibHover, got %v", sigs)
	} else if !strings.Contains(sigs[0], "--") {
		t.Fatalf("expected `chunk` sig to contain stack effect, got %q", sigs[0])
	}
}

func TestHoverForInFileUserDef(t *testing.T) {
	doc := "def addOne (int -- int)\n    1 +\nend\n\n5 addOne\n"
	uri := protocol.DocumentURI("file:///in-file-def.msh")

	addOneCol := strings.Index("5 addOne", "addOne")
	if addOneCol < 0 {
		t.Fatalf("expected to find addOne reference")
	}

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
		"params":  map[string]any{"capabilities": map[string]any{}},
	})
	if r := readLSPResponse(t, output); r.Error != nil {
		t.Fatalf("initialize returned error: %+v", r.Error)
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
		"method":  "textDocument/hover",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":     map[string]any{"line": 4, "character": addOneCol + 1},
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

	if !strings.Contains(hover.Contents.Value, "addOne :: (int -- int)") {
		t.Fatalf("expected in-file def hover to include signature, got %q", hover.Contents.Value)
	}
	if !strings.Contains(hover.Contents.Value, "user-defined") {
		t.Fatalf("expected in-file def hover to be tagged user-defined, got %q", hover.Contents.Value)
	}

	sendLSPMessage(t, clientWriter, map[string]any{"jsonrpc": "2.0", "id": 3, "method": "shutdown"})
	if r := readLSPResponse(t, output); r.Error != nil {
		t.Fatalf("shutdown returned error: %+v", r.Error)
	}
	sendLSPMessage(t, clientWriter, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	clientWriter.Close()
	wg.Wait()
	if runErr != nil {
		t.Fatalf("RunLSP returned error: %v", runErr)
	}
}

func TestCodeActionQuotesAllLiteralsInInnermostList(t *testing.T) {
	uri := protocol.DocumentURI("file:///quote-list.msh")
	doc := "[outer arg [echo café😀 \"already quoted\" @value 3] tail]"
	server := &lspServer{
		documents: map[protocol.DocumentURI]*lspDocument{
			uri: newLSPDocument(doc),
		},
	}

	actions := server.codeActions(protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 24},
			End:   protocol.Position{Line: 0, Character: 24},
		},
	})

	if len(actions) != 1 {
		t.Fatalf("expected one code action, got %d", len(actions))
	}
	if actions[0].Title != "Quote all literals in list" {
		t.Fatalf("unexpected code action title: %q", actions[0].Title)
	}
	if actions[0].Kind != protocol.RefactorRewrite {
		t.Fatalf("unexpected code action kind: %q", actions[0].Kind)
	}
	if actions[0].Edit == nil {
		t.Fatal("expected code action to contain an edit")
	}

	edits := actions[0].Edit.Changes[uri]
	if len(edits) != 2 {
		t.Fatalf("expected two edits for the inner list, got %+v", edits)
	}
	if edits[0].NewText != "'echo'" || edits[1].NewText != "'café😀'" {
		t.Fatalf("unexpected edits: %+v", edits)
	}
	if edits[1].Range.Start.Character != 17 || edits[1].Range.End.Character != 23 {
		t.Fatalf("unexpected UTF-16 range for café😀: %+v", edits[1].Range)
	}
}

func TestCodeActionQuotesNestedLiteralsFromOuterList(t *testing.T) {
	uri := protocol.DocumentURI("file:///quote-outer-list.msh")
	doc := "[outer [inner value] tail]"
	server := &lspServer{
		documents: map[protocol.DocumentURI]*lspDocument{
			uri: newLSPDocument(doc),
		},
	}

	actions := server.codeActions(protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 6},
			End:   protocol.Position{Line: 0, Character: 6},
		},
	})

	if len(actions) != 1 || actions[0].Edit == nil {
		t.Fatalf("expected one code action with edits, got %+v", actions)
	}
	edits := actions[0].Edit.Changes[uri]
	want := []string{"'outer'", "'inner'", "'value'", "'tail'"}
	if len(edits) != len(want) {
		t.Fatalf("expected %d edits, got %+v", len(want), edits)
	}
	for i := range want {
		if edits[i].NewText != want[i] {
			t.Fatalf("edit %d = %q, want %q", i, edits[i].NewText, want[i])
		}
	}
}

// prefixQuoteResult runs the prefix quote code action at line and character
// of doc, and gives the text after its edit, or false if it is not offered.
func prefixQuoteResult(t *testing.T, doc string, line, character uint32) (string, bool) {
	t.Helper()
	uri := protocol.DocumentURI("file:///prefix-quote.msh")
	opened := newLSPDocument(doc)
	server := &lspServer{
		documents: map[protocol.DocumentURI]*lspDocument{uri: opened},
	}
	pos := protocol.Position{Line: line, Character: character}
	actions := server.codeActions(protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range:        protocol.Range{Start: pos, End: pos},
		Context:      protocol.CodeActionContext{Only: []protocol.CodeActionKind{protocol.RefactorRewrite}},
	})
	for _, action := range actions {
		if action.Title != "Convert to prefix quote" {
			continue
		}
		if action.Kind != protocol.RefactorRewrite {
			t.Fatalf("unexpected code action kind: %q", action.Kind)
		}
		edits := action.Edit.Changes[uri]
		if len(edits) != 1 {
			t.Fatalf("expected one edit, got %+v", edits)
		}
		start, ok1 := lspPositionToRuneOffset(doc, edits[0].Range.Start)
		end, ok2 := lspPositionToRuneOffset(doc, edits[0].Range.End)
		if !ok1 || !ok2 {
			t.Fatalf("edit range outside document: %+v", edits[0].Range)
		}
		runes := []rune(doc)
		return string(runes[:start]) + edits[0].NewText + string(runes[end:]), true
	}
	return "", false
}

func TestCodeActionPrefixQuote(t *testing.T) {
	tests := []struct {
		name      string
		doc       string
		line      uint32
		character uint32
		want      string
	}{
		{
			name:      "single line",
			doc:       "[1 2 3] ( 2 * ) map wl",
			character: 10,
			want:      "[1 2 3] map. 2 * end wl",
		},
		{
			name:      "cursor on the word",
			doc:       "[1 2 3] (2 *) map",
			character: 15,
			want:      "[1 2 3] map. 2 * end",
		},
		{
			name:      "cursor just after the word",
			doc:       "[1 2 3] (2 *) map",
			character: 17,
			want:      "[1 2 3] map. 2 * end",
		},
		{
			name:      "empty quote",
			doc:       "() myDef",
			character: 0,
			want:      "myDef. end",
		},
		{
			name:      "innermost quote",
			doc:       "@xs ((5 >) filter) map",
			character: 6,
			want:      "@xs (filter. 5 > end) map",
		},
		{
			name:      "outer quote",
			doc:       "@xs ((5 >) filter) map",
			character: 4,
			want:      "@xs map. (5 >) filter end",
		},
		{
			name:      "unicode before the quote",
			doc:       "\"café😀\" (wl) each",
			character: 10,
			want:      "\"café😀\" each. wl end",
		},
		{
			name: "multiline already on own lines",
			doc: "def f ([str] -- )\n" +
				"    @xs (\n" +
				"        @x wl\n" +
				"    ) each\n" +
				"end",
			line:      2,
			character: 8,
			want: "def f ([str] -- )\n" +
				"    @xs each.\n" +
				"        @x wl\n" +
				"    end\n" +
				"end",
		},
		{
			name: "multiline with body next to the parens",
			doc: "@xs (@x wl\n" +
				"    @y wl) each",
			line:      1,
			character: 4,
			want: "@xs each.\n" +
				"    @x wl\n" +
				"    @y wl\n" +
				"end",
		},
		{
			name: "multiline keeps the body's indentation",
			doc: "(a\n" +
				"b) wl",
			want: "wl.\n" +
				"a\n" +
				"b\n" +
				"end",
		},
		{
			name: "multiline indents with tabs after a tab",
			doc: "\t(a\n" +
				"\t) wl",
			character: 1,
			want: "\twl.\n" +
				"\t\ta\n" +
				"\tend",
		},
		{
			name: "multiline string in the body is unchanged",
			doc: "(\"x\n" +
				"  y\") wl",
			want: "wl.\n" +
				"  \"x\n" +
				"  y\"\n" +
				"end",
		},
		{
			name:      "word on the next line",
			doc:       "(\n  1\n)\nmap",
			line:      3,
			character: 1,
			want:      "map.\n  1\nend",
		},
		{
			name:      "in a definition",
			doc:       "def f (-- [int])\n  [1] (2 *) map\nend",
			line:      1,
			character: 7,
			want:      "def f (-- [int])\n  [1] map. 2 * end\nend",
		},
		{
			name:      "in an if body",
			doc:       "true if\n  [1] (2 *) map\nend",
			line:      1,
			character: 7,
			want:      "true if\n  [1] map. 2 * end\nend",
		},
		{
			name:      "in an else body",
			doc:       "true if 1 else\n  [1] (2 *) map\nend",
			line:      1,
			character: 7,
			want:      "true if 1 else\n  [1] map. 2 * end\nend",
		},
		{
			name:      "in a match arm",
			doc:       "1 match\n  1 : [1] (2 *) map\nend",
			line:      1,
			character: 11,
			want:      "1 match\n  1 : [1] map. 2 * end\nend",
		},
		{
			name:      "in a dict value",
			doc:       "{ \"a\": [1] (2 *) map }",
			character: 13,
			want:      "{ \"a\": [1] map. 2 * end }",
		},
		{
			name:      "in a list",
			doc:       "[ [1] (2 *) map ]",
			character: 7,
			want:      "[ [1] map. 2 * end ]",
		},
		{
			name:      "in a prefix quote",
			doc:       "[[1]] map.\n  (2 *) map\nend",
			line:      1,
			character: 3,
			want:      "[[1]] map.\n  map. 2 * end\nend",
		},
		{
			name:      "multiline with unicode before the paren",
			doc:       "\"😀\" (\n  wl\n) each",
			line:      1,
			character: 2,
			want:      "\"😀\" each.\n  wl\nend",
		},
		{
			name:      "word after the closing paren on the next line",
			doc:       "(1)\n  map",
			line:      1,
			character: 3,
			want:      "map. 1 end",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := prefixQuoteResult(t, tc.doc, tc.line, tc.character)
			if !ok {
				t.Fatal("expected a prefix quote code action")
			}
			if got != tc.want {
				t.Fatalf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestCodeActionPrefixQuoteUnavailable(t *testing.T) {
	tests := []struct {
		name      string
		doc       string
		line      uint32
		character uint32
	}{
		{name: "quote at the end", doc: "(1 2)", character: 1},
		{name: "quote followed by a string", doc: "(1 2) 'map'", character: 1},
		{name: "quote followed by a keyword", doc: "(1 2) if 1 end", character: 1},
		{name: "quote followed by a quote", doc: "(1) (2) map", character: 1},
		{name: "cursor outside", doc: "1 (2) map", character: 0},
		{name: "comment before the word", doc: "(1) # note\nmap", character: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got, ok := prefixQuoteResult(t, tc.doc, tc.line, tc.character); ok {
				t.Fatalf("expected no prefix quote code action, got %q", got)
			}
		})
	}
}

func TestCodeActionUnavailableOutsideListOrForQuickFix(t *testing.T) {
	uri := protocol.DocumentURI("file:///no-quote-list.msh")
	doc := "word [echo arg]"
	server := &lspServer{
		documents: map[protocol.DocumentURI]*lspDocument{
			uri: newLSPDocument(doc),
		},
	}

	outside := server.codeActions(protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 2},
			End:   protocol.Position{Line: 0, Character: 2},
		},
	})
	if len(outside) != 0 {
		t.Fatalf("expected no action outside a list, got %+v", outside)
	}

	quickFixOnly := server.codeActions(protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 8},
			End:   protocol.Position{Line: 0, Character: 8},
		},
		Context: protocol.CodeActionContext{Only: []protocol.CodeActionKind{protocol.QuickFix}},
	})
	if len(quickFixOnly) != 0 {
		t.Fatalf("expected no refactor for a quick-fix-only request, got %+v", quickFixOnly)
	}
}

func TestCodeActionOnlyTargetsParsedListValues(t *testing.T) {
	uri := protocol.DocumentURI("file:///parsed-lists.msh")
	doc := strings.Join([]string{
		"type CustomType = str",
		"type CustomTypes = [CustomType]",
		"def f ([CustomType] -- [CustomType])",
		"    [] match",
		"        [head ...tail] : [echo arg],",
		"        _ : [],",
		"    end",
		"end",
	}, "\n")
	server := &lspServer{
		documents: map[protocol.DocumentURI]*lspDocument{
			uri: newLSPDocument(doc),
		},
	}

	positionOf := func(needle string) protocol.Position {
		offset := strings.Index(doc, needle)
		if offset < 0 {
			t.Fatalf("expected to find %q in document", needle)
		}
		return runeOffsetToLSPPosition(doc, len([]rune(doc[:offset])))
	}
	actionsAt := func(needle string) []protocol.CodeAction {
		position := positionOf(needle)
		return server.codeActions(protocol.CodeActionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Range: protocol.Range{
				Start: position,
				End:   position,
			},
		})
	}

	if actions := actionsAt("[CustomType]"); len(actions) != 0 {
		t.Fatalf("expected no action in a type declaration, got %+v", actions)
	}
	if actions := actionsAt("[CustomType] --"); len(actions) != 0 {
		t.Fatalf("expected no action in a definition signature, got %+v", actions)
	}
	if actions := actionsAt("[head ...tail]"); len(actions) != 0 {
		t.Fatalf("expected no action in a list pattern, got %+v", actions)
	}

	actions := actionsAt("[echo arg]")
	if len(actions) != 1 || actions[0].Edit == nil {
		t.Fatalf("expected an action in the match-arm list value, got %+v", actions)
	}
	edits := actions[0].Edit.Changes[uri]
	if len(edits) != 2 || edits[0].NewText != "'echo'" || edits[1].NewText != "'arg'" {
		t.Fatalf("unexpected list-value edits: %+v", edits)
	}
}

func TestCodeActionDoesNotCrossIntoOtherParseConstructs(t *testing.T) {
	uri := protocol.DocumentURI("file:///list-children.msh")
	doc := "[(danger) { 'key': risky } [inner arg] outer]"
	server := &lspServer{
		documents: map[protocol.DocumentURI]*lspDocument{
			uri: newLSPDocument(doc),
		},
	}

	actions := server.codeActions(protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 1},
			End:   protocol.Position{Line: 0, Character: 1},
		},
	})
	if len(actions) != 1 || actions[0].Edit == nil {
		t.Fatalf("expected one code action with edits, got %+v", actions)
	}
	edits := actions[0].Edit.Changes[uri]
	want := []string{"'inner'", "'arg'", "'outer'"}
	if len(edits) != len(want) {
		t.Fatalf("expected %d edits, got %+v", len(want), edits)
	}
	for i := range want {
		if edits[i].NewText != want[i] {
			t.Fatalf("edit %d = %q, want %q", i, edits[i].NewText, want[i])
		}
	}
}

func TestCodeActionUnavailableForUnclosedList(t *testing.T) {
	uri := protocol.DocumentURI("file:///unclosed-list.msh")
	doc := "[echo arg"
	server := &lspServer{
		documents: map[protocol.DocumentURI]*lspDocument{
			uri: newLSPDocument(doc),
		},
	}

	actions := server.codeActions(protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 5},
			End:   protocol.Position{Line: 0, Character: 5},
		},
	})
	if len(actions) != 0 {
		t.Fatalf("expected no action without a parsed list node, got %+v", actions)
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
	// Skip server-originated notifications (no ID) so tests that
	// expect a specific response don't trip over diagnostics.
	if resp.ID == nil {
		return readLSPResponse(t, reader)
	}
	return resp
}

// The errors about `new` on def outputs come with quick fixes, and one
// action fixes them all.
func TestCodeActionFixesNewMarks(t *testing.T) {
	uri := protocol.DocumentURI("file:///new-marks.msh")
	doc := "def a ( -- [int]) [1] end\ndef b ( -- new int) 1 end\n"
	server := &lspServer{documents: map[protocol.DocumentURI]*lspDocument{uri: newLSPDocument(doc)}}
	at := func(line, char uint32) protocol.CodeActionParams {
		return protocol.CodeActionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Range:        protocol.Range{Start: protocol.Position{Line: line, Character: char}, End: protocol.Position{Line: line, Character: char}},
			Context:      protocol.CodeActionContext{Only: []protocol.CodeActionKind{protocol.QuickFix}},
		}
	}

	actions := server.codeActions(at(0, 12))
	if len(actions) != 1 || actions[0].Title != "Mark this output `new`" {
		t.Fatalf("expected the add-new fix, got %+v", actions)
	}
	edit := actions[0].Edit.Changes[uri][0]
	if edit.NewText != "new " || edit.Range.Start != (protocol.Position{Line: 0, Character: 11}) || edit.Range.End != edit.Range.Start {
		t.Fatalf("unexpected edit %+v", edit)
	}

	actions = server.codeActions(at(1, 11))
	if len(actions) != 1 || actions[0].Title != "Remove `new`" {
		t.Fatalf("expected the remove-new fix, got %+v", actions)
	}
	edit = actions[0].Edit.Changes[uri][0]
	if edit.NewText != "" || edit.Range.Start != (protocol.Position{Line: 1, Character: 11}) || edit.Range.End != (protocol.Position{Line: 1, Character: 15}) {
		t.Fatalf("unexpected edit %+v", edit)
	}

	all := at(0, 0)
	all.Context.Only = []protocol.CodeActionKind{sourceFixAll}
	actions = server.codeActions(all)
	if len(actions) != 1 || len(actions[0].Edit.Changes[uri]) != 2 {
		t.Fatalf("expected one fix-all action with two edits, got %+v", actions)
	}
}

// A word the table types in one form and the walker in another (the dict
// form of urlEncode) shows both on hover.
func TestHoverIndexMergesWalkerForms(t *testing.T) {
	sigs, _ := buildHoverIndex(NewCoreBase(nil, nil), nil)
	got := strings.Join(sigs["urlEncode"], " | ")
	for _, want := range []string{"(str -- str)", "({str: UrlEncodable} -- str)"} {
		if !strings.Contains(got, want) {
			t.Errorf("urlEncode hover is %q, missing %q", got, want)
		}
	}
	if len(sigs["swap"]) != 1 {
		t.Errorf("swap hover has %d forms, want 1: %v", len(sigs["swap"]), sigs["swap"])
	}
}

// TestDiagnosticsNewestTextOnly sends many edits at once: the checks of a
// document do not overlap, the last publish is for the newest text, and a
// document closed while its check runs gets nothing from that check.
func TestDiagnosticsNewestTextOnly(t *testing.T) {
	var buf bytes.Buffer
	s := &lspServer{out: bufio.NewWriter(&buf), documents: map[protocol.DocumentURI]*lspDocument{}}
	uri := protocol.DocumentURI("file:///edits.msh")
	for i := range 50 {
		text := "1 wl\n"
		if i%2 == 0 || i == 49 {
			text = "1 \"a\" +\n"
		}
		s.updateDocument(uri, text)
	}
	closed := protocol.DocumentURI("file:///closed.msh")
	line := "1 2 + wl [1 2 3] (1 +) map len wl\n"
	s.updateDocument(closed, strings.Repeat(line, 8000)+"1 \"a\" +\n")
	s.closeDiagnostics(closed)
	for {
		s.diagMu.Lock()
		busy := false
		for _, st := range s.diag {
			busy = busy || st.running
		}
		s.diagMu.Unlock()
		if !busy {
			break
		}
		time.Sleep(time.Millisecond)
	}
	var last []protocol.Diagnostic
	n := 0
	for _, part := range strings.Split(buf.String(), "Content-Length: ")[1:] {
		body := part[strings.Index(part, "\r\n\r\n")+4:]
		var msg struct {
			Params protocol.PublishDiagnosticsParams `json:"params"`
		}
		if err := json.Unmarshal([]byte(body), &msg); err != nil {
			t.Fatal(err)
		}
		if msg.Params.URI == closed {
			t.Fatalf("a closed document got diagnostics: %+v", msg.Params.Diagnostics)
		}
		last = msg.Params.Diagnostics
		n++
	}
	if len(last) != 1 {
		t.Fatalf("the last publish is not for the newest text: %+v", last)
	}
	if n >= 50 {
		t.Fatalf("%d publishes for 50 edits made at once; the checks were not coalesced", n)
	}
}

// renameEdits renames the variable at (line, char) in text and gives each
// edit as "line:start-end=text".
func renameEdits(t *testing.T, text string, line, char uint32, name string) []string {
	t.Helper()
	uri := protocol.DocumentURI("file:///rename.msh")
	doc := newLSPDocument(text)
	s := &lspServer{documents: map[protocol.DocumentURI]*lspDocument{uri: doc}}
	edit, err := s.rename(protocol.RenameParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: line, Character: char},
		},
		NewName: name,
	})
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	var out []string
	for _, e := range edit.Changes[uri] {
		out = append(out, fmt.Sprintf("%d:%d-%d=%s", e.Range.Start.Line, e.Range.Start.Character, e.Range.End.Character, e.NewText))
	}
	return out
}

// TestRenameReachesEveryBody renames through if arms, match arm bindings
// and bodies, `=>` bindings and prefix quotes, and counts columns in UTF-16
// code units as LSP does.
func TestRenameReachesEveryBody(t *testing.T) {
	cases := []struct {
		text       string
		line, char uint32
		want       string
	}{
		{"1 n!\ntrue if @n wl end\n@n wl\n", 0, 2, "[0:2-4=m! 1:8-10=@m 2:0-2=@m]"},
		{"[1 2] k!\n[3] map. @k len + end drop\n", 0, 6, "[0:6-8=m! 1:9-11=@m]"},
		{"[1 2] => [a b]\n@a @b + wl\n", 1, 1, "[0:10-11=m 1:0-2=@m]"},
		{"5 just match just n : @n wl, none : end\n", 0, 23, "[0:18-19=m 0:22-24=@m]"},
		{"\"😀\" n! @n wl\n", 0, 9, "[0:5-7=m! 0:8-10=@m]"},
	}
	for _, c := range cases {
		got := fmt.Sprint(renameEdits(t, c.text, c.line, c.char, "m"))
		if got != c.want {
			t.Errorf("%q: got %s, want %s", c.text, got, c.want)
		}
	}
}

// TestDiagnosticColumnsUTF16 puts a diagnostic after characters outside the
// basic plane at the UTF-16 position of its token.
func TestDiagnosticColumnsUTF16(t *testing.T) {
	s := &lspServer{}
	diags := s.computeDiagnostics("", "\"😀😀😀\" 1 +\n")
	if len(diags) != 1 {
		t.Fatalf("diagnostics: %+v", diags)
	}
	if r := diags[0].Range; r.Start.Character != 11 || r.End.Character != 12 {
		t.Fatalf("range %+v, want characters 11-12", r)
	}
}

// TestTypeExprParseErrorPosition places a parse error inside a type
// expression at its token, not at the top of the file, for each form that
// names where the type expression was.
func TestTypeExprParseErrorPosition(t *testing.T) {
	cases := []string{
		"type T = [\n  int,\n]\n",
		"def f (\n  [int,] -- int) end\n",
		"1 as [\n  int,]\n",
	}
	for _, src := range cases {
		diags := (&lspServer{}).computeDiagnostics("", src)
		if len(diags) == 0 {
			t.Fatalf("%q: no diagnostics", src)
		}
		if r := diags[0].Range; r.Start.Line != 1 {
			t.Fatalf("%q: range %+v (%s), want line 1", src, r, diags[0].Message)
		}
	}
}

// TestDiagnosticsShowStartupErrors shows an error in the startup files'
// declarations on every document, as the command line fails every script
// on it.
func TestDiagnosticsShowStartupErrors(t *testing.T) {
	parsed, err := parseMShellInput("type U = [int] | [str]\n", &TokenFile{"init.msh"})
	if err != nil {
		t.Fatal(err)
	}
	s := &lspServer{startupDecls: declarationItems(parsed.Items), startupErrs: []string{"the init file x does not parse"}}
	diags := s.computeDiagnostics("", "1 wl\n")
	if len(diags) != 2 || !strings.Contains(diags[0].Message, "does not parse") || !strings.Contains(diags[1].Message, "init.msh") {
		t.Fatalf("diagnostics: %+v", diags)
	}
}

// TestDiagnosticsStartupFileItself: a document that is one of the startup
// files is checked with only the files before it, as the command line runs
// it, so its definitions do not collide with themselves. Any other document
// still sees every startup file, and a collision names the file.
func TestDiagnosticsStartupFileItself(t *testing.T) {
	dir := t.TempDir()
	stdPath, initPath := filepath.Join(dir, "std.msh"), filepath.Join(dir, "init.msh")
	stdText := "def stdOne ( -- int) 1 end\n"
	initText := "def initOne ( -- int) stdOne end\ntype InitT = int\n5 as InitT initOne + wl\n"
	var files []lspStartupFile
	for _, f := range []struct{ path, text string }{{stdPath, stdText}, {initPath, initText}} {
		if err := os.WriteFile(f.path, []byte(f.text), 0o644); err != nil {
			t.Fatal(err)
		}
		parsed, err := parseMShellInput(f.text, &TokenFile{f.path})
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, lspStartupFile{path: f.path, defs: parsed.Definitions, decls: declarationItems(parsed.Items)})
	}
	s := &lspServer{startupFiles: files}
	s.stdlibDefs, s.startupDecls = joinStartupFiles(files)
	uri := func(p string) protocol.DocumentURI { return protocol.DocumentURI("file://" + filepath.ToSlash(p)) }

	if diags := s.computeDiagnostics(uri(initPath), initText); len(diags) != 0 {
		t.Fatalf("the init file itself: %+v", diags)
	}
	if diags := s.computeDiagnostics(uri(stdPath), stdText); len(diags) != 0 {
		t.Fatalf("the standard library itself: %+v", diags)
	}
	other := filepath.Join(dir, "other.msh")
	diags := s.computeDiagnostics(uri(other), "def initOne ( -- int) 2 end\n")
	if len(diags) != 1 || !strings.Contains(diags[0].Message, initPath) {
		t.Fatalf("another document: %+v", diags)
	}
	if diags := s.computeDiagnostics(uri(other), "initOne stdOne + wl\n"); len(diags) != 0 {
		t.Fatalf("another document using the startup files: %+v", diags)
	}
}

// TestDiagnosticsStartupDefErrors: an error in a startup def's body shows
// where the def is called, not on every document, and in the startup file
// itself when it is open.
func TestDiagnosticsStartupDefErrors(t *testing.T) {
	dir := t.TempDir()
	initPath := filepath.Join(dir, "init.msh")
	initText := "def badBody (int -- int) \"oops\" end\ndef good ( -- int) 1 end\n"
	if err := os.WriteFile(initPath, []byte(initText), 0o644); err != nil {
		t.Fatal(err)
	}
	parsed, err := parseMShellInput(initText, &TokenFile{initPath})
	if err != nil {
		t.Fatal(err)
	}
	files := []lspStartupFile{{path: initPath, defs: parsed.Definitions}}
	s := &lspServer{startupFiles: files}
	s.stdlibDefs, s.startupDecls = joinStartupFiles(files)
	uri := func(p string) protocol.DocumentURI { return protocol.DocumentURI("file://" + filepath.ToSlash(p)) }
	other := filepath.Join(dir, "other.msh")
	if diags := s.computeDiagnostics(uri(other), "good wl\n"); len(diags) != 0 {
		t.Fatalf("a document that does not call it: %+v", diags)
	}
	if diags := s.computeDiagnostics(uri(other), "5 badBody wl\n"); len(diags) != 1 || !strings.Contains(diags[0].Message, "its body has a type error") {
		t.Fatalf("a document that calls it: %+v", diags)
	}
	if diags := s.computeDiagnostics(uri(initPath), initText); len(diags) != 1 || diags[0].Range.Start.Line != 0 {
		t.Fatalf("the init file itself: %+v", diags)
	}
}

// TestDiagnosticsAfterStartupTopLevel: a document is checked after the
// startup files' top-level code, as the command line checks a script: it
// may use the variables and the stack init.msh leaves. A startup file
// whose top level does not check shows on every document, as the command
// line then runs no script.
func TestDiagnosticsAfterStartupTopLevel(t *testing.T) {
	dir := t.TempDir()
	initPath := filepath.Join(dir, "init.msh")
	server := func(initText string) *lspServer {
		parsed, err := parseMShellInput(initText, &TokenFile{initPath})
		if err != nil {
			t.Fatal(err)
		}
		files := []lspStartupFile{{path: initPath, defs: parsed.Definitions, decls: declarationItems(parsed.Items), top: startupTopLevel(parsed)}}
		s := &lspServer{startupFiles: files}
		s.stdlibDefs, s.startupDecls = joinStartupFiles(files)
		return s
	}
	uri := protocol.DocumentURI("file://" + filepath.ToSlash(filepath.Join(dir, "other.msh")))

	s := server("5 base!\n\"left\"\n")
	if diags := s.computeDiagnostics(uri, "@base 1 + wl wl\n"); len(diags) != 0 {
		t.Fatalf("a document using init.msh's variable and stack: %+v", diags)
	}
	if diags := s.computeDiagnostics(uri, "@base \"a\" + wl\n"); len(diags) != 1 {
		t.Fatalf("a document misusing init.msh's variable: %+v", diags)
	}

	s = server("5 base!\n1 \"a\" +\n")
	diags := s.computeDiagnostics(uri, "1 wl\n")
	if len(diags) != 1 || !strings.Contains(diags[0].Message, "top-level code does not check") || !strings.Contains(diags[0].Message, initPath) {
		t.Fatalf("init.msh's top level does not check: %+v", diags)
	}
	if diags := s.computeDiagnostics(uri, "@base wl\n"); len(diags) != 2 {
		t.Fatalf("init.msh's top level did not run, so its variable is unset: %+v", diags)
	}
}

// declarationAt is the locations goto declaration gives at line:char of
// text, as "uri line:start-end".
func declarationAt(t *testing.T, s *lspServer, uri protocol.DocumentURI, text string, line, char uint32) []string {
	t.Helper()
	doc := newLSPDocument(text)
	s.documents = map[protocol.DocumentURI]*lspDocument{uri: doc}
	var out []string
	for _, loc := range s.declaration(protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Position:     protocol.Position{Line: line, Character: char},
	}) {
		out = append(out, fmt.Sprintf("%s %d:%d-%d", loc.URI, loc.Range.Start.Line, loc.Range.Start.Character, loc.Range.End.Character))
	}
	return out
}

// TestDeclarationInDocument: goto declaration on a type name, an enum
// name, an enum member and a definition goes to its declaration in the
// document, counting columns in UTF-16 code units; a word in a string or
// a builtin type has none.
func TestDeclarationInDocument(t *testing.T) {
	uri := protocol.DocumentURI("file:///decl.msh")
	text := "type P = {a: Shape}\n" +
		"enum Shape = circle float | dot end\n" +
		"enum Box[a] = box [a] | empty end\n" +
		"def area (Box[int] Shape -- P)\n" +
		"    match circle r : 1, Shape s : 2, end\n" +
		"end\n" +
		"\"😀\" 2.0 circle area \"Shape\" as Json\n"
	cases := []struct {
		line, char uint32
		want       string
	}{
		{0, 14, "file:///decl.msh 1:5-10"},  // Shape in a type body
		{3, 10, "file:///decl.msh 2:5-8"},   // Box in Box[int]
		{3, 20, "file:///decl.msh 1:5-10"},  // Shape in a signature
		{3, 28, "file:///decl.msh 0:5-6"},   // P in a signature
		{4, 12, "file:///decl.msh 1:13-19"}, // circle as a pattern
		{4, 26, "file:///decl.msh 1:5-10"},  // Shape as a type pattern
		{6, 10, "file:///decl.msh 1:13-19"}, // circle after a non-BMP rune
		{6, 16, "file:///decl.msh 3:4-8"},   // area, the definition
		{6, 23, ""},                         // Shape in a string
		{6, 33, ""},                         // the builtin Json
	}
	s := &lspServer{}
	for _, c := range cases {
		got := strings.Join(declarationAt(t, s, uri, text, c.line, c.char), ", ")
		if got != c.want {
			t.Errorf("%d:%d: got %q, want %q", c.line, c.char, got, c.want)
		}
	}
}

// TestDeclarationInStartupFile: a name declared in a startup file goes to
// that file, unless the document is that file or one before it.
func TestDeclarationInStartupFile(t *testing.T) {
	dir := t.TempDir()
	stdPath, initPath := filepath.Join(dir, "std.msh"), filepath.Join(dir, "init.msh")
	stdText := "def stdOne ( -- int) 1 end\n"
	initText := "# é\nenum Color = red | green end\ntype Pt = {px: int}\n"
	var files []lspStartupFile
	for _, f := range []struct{ path, text string }{{stdPath, stdText}, {initPath, initText}} {
		if err := os.WriteFile(f.path, []byte(f.text), 0o644); err != nil {
			t.Fatal(err)
		}
		parsed, err := parseMShellInput(f.text, &TokenFile{f.path})
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, lspStartupFile{path: f.path, defs: parsed.Definitions, decls: declarationItems(parsed.Items), doc: newLSPDocument(f.text)})
	}
	s := &lspServer{startupFiles: files}
	other := fileURI(filepath.Join(dir, "other.msh"))
	text := "green stdOne { px: 1 } as Pt Color\n"
	for _, c := range []struct {
		char uint32
		want string
	}{
		{1, string(fileURI(initPath)) + " 1:19-24"},
		{8, string(fileURI(stdPath)) + " 0:4-10"},
		{26, string(fileURI(initPath)) + " 2:5-7"},
		{29, string(fileURI(initPath)) + " 1:5-10"},
	} {
		got := strings.Join(declarationAt(t, s, other, text, 0, c.char), ", ")
		if got != c.want {
			t.Errorf("0:%d: got %q, want %q", c.char, got, c.want)
		}
	}
	// The standard library is checked without the init file, so the init
	// file's declarations are not its own.
	if got := declarationAt(t, s, fileURI(stdPath), "green\n", 0, 1); len(got) != 0 {
		t.Errorf("the standard library itself: %v", got)
	}
}

// lspPositionToRuneOffset is the rune offset in text of an LSP position.
// It reads the whole text, so the server does not use it; tests check edits
// with it.
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

// runeOffsetToLSPPosition is the LSP position of a rune offset in text.
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

func changeAt(sl, sc, el, ec uint32, text string) lspContentChange {
	return lspContentChange{Range: &protocol.Range{
		Start: protocol.Position{Line: sl, Character: sc},
		End:   protocol.Position{Line: el, Character: ec},
	}, Text: text}
}

func TestApplyChanges(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		changes []lspContentChange
		want    string
	}{
		{"insert", "ab\ncd", []lspContentChange{changeAt(1, 1, 1, 1, "X")}, "ab\ncXd"},
		{"delete across lines", "ab\ncd\nef", []lspContentChange{changeAt(0, 1, 2, 1, "")}, "af"},
		{"replace with lines", "ab", []lspContentChange{changeAt(0, 1, 0, 2, "1\n2\n")}, "a1\n2\n"},
		{"insert at the start", "ab", []lspContentChange{changeAt(0, 0, 0, 0, "X")}, "Xab"},
		{"insert at the end", "ab\n", []lspContentChange{changeAt(1, 0, 1, 0, "X")}, "ab\nX"},
		{"into an empty text", "", []lspContentChange{changeAt(0, 0, 0, 0, "X")}, "X"},
		{"UTF-16 columns", "😀a😀b", []lspContentChange{changeAt(0, 3, 0, 5, "")}, "😀ab"},
		{"carriage return stays", "ab\r\ncd", []lspContentChange{changeAt(0, 2, 0, 2, "X")}, "abX\r\ncd"},
		{"past the line end", "ab\ncd", []lspContentChange{changeAt(0, 9, 0, 9, "X")}, "abX\ncd"},
		{"past the last line", "ab", []lspContentChange{changeAt(5, 0, 5, 0, "X")}, "abX"},
		{"each on the text before", "abc", []lspContentChange{changeAt(0, 0, 0, 1, "xy"), changeAt(0, 2, 0, 3, "")}, "xyc"},
		{"whole text", "abc", []lspContentChange{{Text: "new"}}, "new"},
		{"whole text then a range", "abc", []lspContentChange{{Text: "new"}, changeAt(0, 0, 0, 1, "N")}, "New"},
	}
	s := &lspServer{}
	for _, c := range cases {
		if got := s.applyChanges(c.text, c.changes); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestApplyChangesRandom makes random edits, in batches, and checks each
// against the same edit made on runes.
func TestApplyChangesRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	pieces := []string{"a", "b", " ", "\n", "\r\n", "é", "😀", "(", "end"}
	randText := func(n int) string {
		var b strings.Builder
		for range n {
			b.WriteString(pieces[rng.Intn(len(pieces))])
		}
		return b.String()
	}
	// randPos is a valid position in text.
	randPos := func(text string) protocol.Position {
		runes := []rune(text)
		return runeOffsetToLSPPosition(text, rng.Intn(len(runes)+1))
	}
	s := &lspServer{}
	text := randText(40)
	for i := range 5000 {
		want := text
		var batch []lspContentChange
		for range 1 + rng.Intn(3) {
			a, b := randPos(want), randPos(want)
			ao, _ := lspPositionToRuneOffset(want, a)
			bo, _ := lspPositionToRuneOffset(want, b)
			if bo < ao {
				a, b, ao, bo = b, a, bo, ao
			}
			insert := randText(rng.Intn(4))
			runes := []rune(want)
			want = string(runes[:ao]) + insert + string(runes[bo:])
			batch = append(batch, lspContentChange{Range: &protocol.Range{Start: a, End: b}, Text: insert})
		}
		got := s.applyChanges(text, batch)
		if got != want {
			t.Fatalf("edit %d: %+v on %q: got %q, want %q", i, batch, text, got, want)
		}
		text = got
		if len(text) > 400 {
			text = randText(40)
		}
	}
}

// TestDidChangeIncremental sends a document's edits as the client does:
// ranges, and a change to the whole text with no range.
func TestDidChangeIncremental(t *testing.T) {
	s := &lspServer{out: bufio.NewWriter(io.Discard), documents: map[protocol.DocumentURI]*lspDocument{}}
	send := func(method, params string) {
		t.Helper()
		if _, err := s.handleMessage(&jsonrpcMessage{Method: method, Params: json.RawMessage(params)}); err != nil {
			t.Fatal(err)
		}
	}
	uri := "file:///inc.msh"
	send("textDocument/didOpen", `{"textDocument":{"uri":"`+uri+`","languageId":"mshell","version":1,"text":"1 wl\n2 wl\n"}}`)
	send("textDocument/didChange", `{"textDocument":{"uri":"`+uri+`","version":2},"contentChanges":[`+
		`{"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":1}},"text":"20"},`+
		`{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"text":"0 wl\n"}]}`)
	if got := s.documents[protocol.DocumentURI(uri)].Text; got != "0 wl\n1 wl\n20 wl\n" {
		t.Fatalf("after ranges: %q", got)
	}
	send("textDocument/didChange", `{"textDocument":{"uri":"`+uri+`","version":3},"contentChanges":[{"text":"3 wl\n"}]}`)
	if got := s.documents[protocol.DocumentURI(uri)].Text; got != "3 wl\n" {
		t.Fatalf("after the whole text: %q", got)
	}
}

// TestRequestsWhileDiagnosticsRun serves requests on a document while its
// diagnostics are worked out on their goroutine, as the server does; run
// with -race, it shows the two share the document's parse and check
// safely.
func TestRequestsWhileDiagnosticsRun(t *testing.T) {
	s := &lspServer{out: bufio.NewWriter(io.Discard), documents: map[protocol.DocumentURI]*lspDocument{}}
	uri := protocol.DocumentURI("file:///busy.msh")
	for i := range 100 {
		text := fmt.Sprintf("def f (int -- int) %d + end\n[1 2] (f) map wl\n[a b] wl\n1 \"a\" +\n", i)
		s.updateDocument(uri, text)
		for line := range uint32(4) {
			pos := protocol.Position{Line: line, Character: 8}
			s.codeActions(protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: uri},
				Range:        protocol.Range{Start: pos, End: pos},
			})
			s.hover(protocol.HoverParams{TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: uri}, Position: protocol.Position{Line: 1, Character: 8},
			}})
			s.declaration(protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: uri}, Position: protocol.Position{Line: 1, Character: 8},
			})
		}
	}
	for {
		s.diagMu.Lock()
		busy := s.diag[uri] != nil && s.diag[uri].running
		s.diagMu.Unlock()
		if !busy {
			break
		}
		time.Sleep(time.Millisecond)
	}
}
