package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// Benchmarks for the type checker alone: files are parsed up front, so
// only the check is timed. The corpus benchmarks are in TypeCore_test.go.

func benchParse(tb testing.TB, src string) *MShellFile {
	tb.Helper()
	file, err := NewMShellParser(NewLexer(src, nil)).ParseFile()
	if err != nil {
		tb.Fatalf("parse error: %v", err)
	}
	return file
}

func benchStdlib(tb testing.TB) []MShellDefinition {
	tb.Helper()
	src, err := os.ReadFile("../lib/std.msh")
	if err != nil {
		tb.Fatal(err)
	}
	return benchParse(tb, string(src)).Definitions
}

// benchShapes are single top-level lines, repeated n times to show how
// checking time grows with program length.
var benchShapes = map[string]string{
	"tokens": "1 2 + drop",
	"list":   "[1 2 3] len drop",
	"cmd":    "[ls -l foo] drop",
	"dict":   "{a: 1, b: \"x\"} drop",
	"fmt":    "$\"{1}-{\"a\"}\" drop",
	"nested": "[[1 2] [3]] len drop",
	"vars":   "1 v%d! @v%d drop",
	"quote":  "[1 2] (1 +) map drop",
	"if":     "1 2 < if 1 else 2 end drop",
	"match":  "\"x\" match \"x\" : \"a\", _ : \"b\", end drop",
	"def":    "def f%d (int -- int) 1 + end 1 f%d drop",
}

func BenchmarkTypeCheckScaling(b *testing.B) {
	base := NewCoreBase(benchStdlib(b), nil)
	for _, name := range []string{"tokens", "list", "cmd", "dict", "fmt", "nested", "vars", "quote", "if", "match", "def"} {
		for _, n := range []int{500, 1000, 2000, 4000} {
			var sb strings.Builder
			for i := 0; i < n; i++ {
				line := benchShapes[name]
				if strings.Contains(line, "%d") {
					line = fmt.Sprintf(line, i, i)
				}
				sb.WriteString(line)
				sb.WriteByte('\n')
			}
			file := benchParse(b, sb.String())
			b.Run(fmt.Sprintf("%s/%d", name, n), func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					base.Check(file)
				}
			})
		}
	}
}

// BenchmarkLSPDiagnostics times the language server's per-edit diagnostics
// pass, parse included, on real scripts of a few sizes.
func BenchmarkLSPDiagnostics(b *testing.B) {
	s := &lspServer{stdlibDefs: benchStdlib(b)}
	for _, name := range []string{"pathbins", "nodes", "setdiff2way.msh"} {
		src, err := os.ReadFile("../tests/msh-scripts/" + name)
		if err != nil {
			b.Fatal(err)
		}
		text := string(src)
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				s.computeDiagnostics(text)
			}
		})
	}
}
