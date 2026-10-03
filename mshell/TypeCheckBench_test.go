package main

import (
	"fmt"
	"os"
	"path/filepath"
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
				s.computeDiagnostics("", text)
			}
		})
	}
}

// generatedPrograms reads testdata/generated: programs the soundness
// generator wrote (TypeSoundGen_test.go), denser in types, matches and
// quotes than the test corpus. They are fixed files, so a change to the
// generator does not change what the benchmarks measure.
func generatedPrograms(tb testing.TB) []string {
	tb.Helper()
	paths, err := filepath.Glob("testdata/generated/*.msh")
	if err != nil || len(paths) == 0 {
		tb.Fatalf("no generated programs: %v", err)
	}
	var out []string
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			tb.Fatal(err)
		}
		out = append(out, string(src))
	}
	return out
}

// BenchmarkCoreCheckGenerated checks the generated programs (about 50 KB).
func BenchmarkCoreCheckGenerated(b *testing.B) {
	base := NewCoreBase(nil, nil)
	var files []*MShellFile
	for _, src := range generatedPrograms(b) {
		files = append(files, benchParse(b, src))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, f := range files {
			if _, ok := base.Check(f); !ok {
				b.Fatal("a generated program does not check")
			}
		}
	}
}

// BenchmarkParseGenerated parses the same programs: the language server
// parses before every check.
func BenchmarkParseGenerated(b *testing.B) {
	srcs := generatedPrograms(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, src := range srcs {
			benchParse(b, src)
		}
	}
}
