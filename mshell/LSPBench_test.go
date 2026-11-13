package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"go.lsp.dev/protocol"
)

// benchCodeActionDoc is the standard library, opened in the language server.
func benchCodeActionDoc(b *testing.B) (*lspDocument, *MShellFile) {
	b.Helper()
	src, err := os.ReadFile("../lib/std.msh")
	if err != nil {
		b.Fatal(err)
	}
	doc := newLSPDocument(string(src))
	file, err := NewMShellParser(NewLexer(doc.Text, nil)).ParseFile()
	if err != nil {
		b.Fatal(err)
	}
	return doc, file
}

func benchCodeActionParams(line, character uint32) protocol.CodeActionParams {
	pos := protocol.Position{Line: line, Character: character}
	return protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: "file:///std.msh"},
		Range:        protocol.Range{Start: pos, End: pos},
		Context:      protocol.CodeActionContext{Only: []protocol.CodeActionKind{protocol.RefactorRewrite}},
	}
}

// BenchmarkPrefixQuoteAction runs the prefix quote code action alone, as an
// editor does on each cursor move: once on a quote it can rewrite, once off.
func BenchmarkPrefixQuoteAction(b *testing.B) {
	doc, file := benchCodeActionDoc(b)
	cases := []struct {
		name   string
		params protocol.CodeActionParams
		offer  bool
	}{
		// `@count seq (drop @item) map`, cursor on `drop`.
		{"hit", benchCodeActionParams(379, 16), true},
		// The first line of the file.
		{"miss", benchCodeActionParams(0, 0), false},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			if _, ok := prefixQuoteAction(doc, file, c.params); ok != c.offer {
				b.Fatalf("offered %v, want %v", ok, c.offer)
			}
			b.ReportAllocs()
			for b.Loop() {
				prefixQuoteAction(doc, file, c.params)
			}
		})
	}
}

// BenchmarkCodeActionsRefactor is a whole refactor code action request,
// parse included.
func BenchmarkCodeActionsRefactor(b *testing.B) {
	doc, _ := benchCodeActionDoc(b)
	s := &lspServer{documents: map[protocol.DocumentURI]*lspDocument{"file:///std.msh": doc}}
	params := benchCodeActionParams(379, 16)
	b.ReportAllocs()
	for b.Loop() {
		s.codeActions(params)
	}
}

// BenchmarkCodeActionsAutomatic is the request an editor sends as the
// cursor moves to decide whether to show a light bulb: no kind filter, so
// it includes the type checker's quick fixes.
func BenchmarkCodeActionsAutomatic(b *testing.B) {
	s := &lspServer{stdlibDefs: benchStdlib(b), documents: map[protocol.DocumentURI]*lspDocument{}}
	for _, name := range []string{"pathbins", "nodes", "setdiff2way.msh"} {
		src, err := os.ReadFile("../tests/msh-scripts/" + name)
		if err != nil {
			b.Fatal(err)
		}
		doc := newLSPDocument(string(src))
		uri := protocol.DocumentURI("file:///" + name)
		s.documents[uri] = doc
		pos := protocol.Position{Line: uint32(len(doc.Lines) / 2)}
		params := protocol.CodeActionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Range:        protocol.Range{Start: pos, End: pos},
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				s.codeActions(params)
			}
		})
	}
}

// BenchmarkLSPDidChange is what a keystroke costs the event loop: reading
// the client's change and making the document's new version. Its
// diagnostics are worked out off the loop.
func BenchmarkLSPDidChange(b *testing.B) {
	std, err := os.ReadFile("../lib/std.msh")
	if err != nil {
		b.Fatal(err)
	}
	small, err := os.ReadFile("../tests/msh-scripts/setdiff2way.msh")
	if err != nil {
		b.Fatal(err)
	}
	for _, c := range []struct{ name, text string }{
		{"5KB", string(small)},
		{"55KB", string(std)},
		{"220KB", strings.Repeat(string(std), 4)},
	} {
		doc := newLSPDocument(c.text)
		mid := uint32(len(doc.Lines) / 2)
		// Type a character in the middle of the file, then delete it.
		payloads := [2][]byte{
			[]byte(fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didChange","params":{"textDocument":{"uri":"file:///x.msh","version":2},"contentChanges":[{"range":{"start":{"line":%d,"character":0},"end":{"line":%d,"character":0}},"rangeLength":0,"text":"x"}]}}`, mid, mid)),
			[]byte(fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didChange","params":{"textDocument":{"uri":"file:///x.msh","version":3},"contentChanges":[{"range":{"start":{"line":%d,"character":0},"end":{"line":%d,"character":1}},"rangeLength":1,"text":""}]}}`, mid, mid)),
		}
		s := &lspServer{}
		b.Run(c.name, func(b *testing.B) {
			doc := newLSPDocument(c.text)
			b.ReportAllocs()
			i := 0
			for b.Loop() {
				var msg jsonrpcMessage
				if err := json.Unmarshal(payloads[i%2], &msg); err != nil {
					b.Fatal(err)
				}
				var params lspDidChangeParams
				if err := json.Unmarshal(msg.Params, &params); err != nil {
					b.Fatal(err)
				}
				doc = newLSPDocument(s.applyChanges(doc.Text, params.ContentChanges))
				i++
			}
			if i%2 == 0 && doc.Text != c.text {
				b.Fatal("the edits did not undo each other")
			}
		})
	}
}

// BenchmarkLSPRequests serves requests on the standard library as a
// user's script (its definitions renamed, so they are the file's own):
// moving the cursor, hovering a definition, going to its declaration, and
// typing, which makes a new version of the text, its diagnostics, and a
// completion request on it.
func BenchmarkLSPRequests(b *testing.B) {
	src, err := os.ReadFile("../lib/std.msh")
	if err != nil {
		b.Fatal(err)
	}
	text := strings.ReplaceAll(string(src), "def ", "def u")
	s := &lspServer{
		stdlibDefs: benchStdlib(b),
		documents:  map[protocol.DocumentURI]*lspDocument{},
		lexer:      NewLexer("", nil),
		varNames:   map[string]struct{}{},
		envNames:   map[string]struct{}{},
	}
	uri := protocol.DocumentURI("file:///user.msh")
	doc := newLSPDocument(text)
	s.documents[uri] = doc
	// Line 168 is `def uwt (-- [[str]]) sl (wsplit) map  end`.
	if !strings.HasPrefix(doc.Lines[168], "def uwt") {
		b.Fatalf("line 168 is %q", doc.Lines[168])
	}
	at := func(char uint32) protocol.TextDocumentPositionParams {
		return protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: 168, Character: char},
		}
	}
	b.Run("cursorMove", func(b *testing.B) {
		cursor := at(26).Position
		b.ReportAllocs()
		for b.Loop() {
			s.codeActions(protocol.CodeActionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: uri},
				Range:        protocol.Range{Start: cursor, End: cursor},
			})
		}
	})
	b.Run("hover", func(b *testing.B) {
		if _, ok := s.hover(protocol.HoverParams{TextDocumentPositionParams: at(5)}); !ok {
			b.Fatal("no hover")
		}
		b.ReportAllocs()
		for b.Loop() {
			s.hover(protocol.HoverParams{TextDocumentPositionParams: at(5)})
		}
	})
	b.Run("declaration", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			s.declaration(at(5))
		}
	})
	b.Run("keystroke", func(b *testing.B) {
		texts := [2]string{text, strings.Replace(text, "(wsplit)", "(wsplitx)", 1)}
		i := 0
		b.ReportAllocs()
		for b.Loop() {
			d := newLSPDocument(texts[i%2])
			i++
			s.documents[uri] = d
			s.checked(uri, d)
			s.completion(protocol.CompletionParams{TextDocumentPositionParams: at(27)})
		}
	})
}
