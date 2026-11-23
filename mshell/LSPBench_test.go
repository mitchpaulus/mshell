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

// benchLine is the 0-based line of doc that holds text. The lines of the
// standard library move as it is edited, so a benchmark finds its line by
// the text on it.
func benchLine(b *testing.B, doc *lspDocument, text string) uint32 {
	b.Helper()
	for i, line := range doc.Lines {
		if strings.Contains(line, text) {
			return uint32(i)
		}
	}
	b.Fatalf("no line holds %q", text)
	return 0
}

// benchPrefixQuoteLine is a line of the standard library with a quote the
// prefix quote code action rewrites, `drop` at character 16.
const benchPrefixQuoteLine = "    @count seq (drop @item) map"

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
		// Cursor on `drop`.
		{"hit", benchCodeActionParams(benchLine(b, doc, benchPrefixQuoteLine), 16), true},
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

// BenchmarkIffToIfAction runs the iff code action alone, as an editor does
// on each cursor move: on an `iff` of the standard library and off one, and
// on the last of 10000 at the top level of a file, where the time must not
// grow with the size of the file.
func BenchmarkIffToIfAction(b *testing.B) {
	doc, file := benchCodeActionDoc(b)
	const call = "( ['auto' 'always' 'never'] ) ( __rgDefaultOptions ) iff"
	hit := benchLine(b, doc, call)
	const n = 10000
	flat := newLSPDocument(strings.Repeat("true (1 drop) (2 drop) iff\n", n))
	flatFile, err := flat.parsed()
	if err != nil {
		b.Fatal(err)
	}
	cases := []struct {
		name   string
		doc    *lspDocument
		file   *MShellFile
		params protocol.CodeActionParams
		offer  bool
	}{
		{"hit", doc, file, benchCodeActionParams(hit, uint32(strings.Index(doc.Lines[hit], call)+len(call)-1)), true},
		// The first line of the file.
		{"miss", doc, file, benchCodeActionParams(0, 0), false},
		{"hit/10000", flat, flatFile, benchCodeActionParams(n-1, 24), true},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			if _, ok := iffToIfAction(c.doc, c.file, c.params); ok != c.offer {
				b.Fatalf("offered %v, want %v", ok, c.offer)
			}
			b.ReportAllocs()
			for b.Loop() {
				iffToIfAction(c.doc, c.file, c.params)
			}
		})
	}
}

// BenchmarkCodeActionsRefactor is a whole refactor code action request,
// parse included.
func BenchmarkCodeActionsRefactor(b *testing.B) {
	doc, _ := benchCodeActionDoc(b)
	s := &lspServer{documents: map[protocol.DocumentURI]*lspDocument{"file:///std.msh": doc}}
	params := benchCodeActionParams(benchLine(b, doc, benchPrefixQuoteLine), 16)
	b.ReportAllocs()
	for b.Loop() {
		s.codeActions(params)
	}
}

// BenchmarkQuoteLiteralsActions is a refactor code action request with the
// cursor in a list of bare words, some of them builtins: a short list, and
// 1000 words on ten to a line and all on one line, where the time must not
// grow with the square of the line's length.
func BenchmarkQuoteLiteralsActions(b *testing.B) {
	list := func(n, perLine int) string {
		var sb strings.Builder
		sb.WriteString("[")
		for i := range n {
			switch {
			case i == 0:
			case perLine > 0 && i%perLine == 0:
				sb.WriteString("\n ")
			default:
				sb.WriteString(" ")
			}
			if i%5 == 1 {
				sb.WriteString("sort")
			} else {
				fmt.Fprintf(&sb, "word%04d", i)
			}
		}
		sb.WriteString("]\n")
		return sb.String()
	}
	for _, c := range []struct {
		name       string
		n, perLine int
	}{{"10", 10, 0}, {"1000/lines", 1000, 10}, {"1000/oneLine", 1000, 0}} {
		uri := protocol.DocumentURI("file:///list.msh")
		s := &lspServer{stdlibDefs: benchStdlib(b), documents: map[protocol.DocumentURI]*lspDocument{uri: newLSPDocument(list(c.n, c.perLine))}}
		params := benchCodeActionParams(0, 1)
		params.TextDocument.URI = uri
		b.Run(c.name, func(b *testing.B) {
			if got := s.codeActions(params); len(got) != 2 {
				b.Fatalf("%d actions, want 2", len(got))
			}
			b.ReportAllocs()
			for b.Loop() {
				s.codeActions(params)
			}
		})
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
	line := benchLine(b, doc, "def uwt (-- [[str]]) sl (wsplit) map  end")
	at := func(char uint32) protocol.TextDocumentPositionParams {
		return protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     protocol.Position{Line: line, Character: char},
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
