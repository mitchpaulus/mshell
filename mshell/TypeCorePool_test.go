package main

import (
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestCoreCheckPoolMatchesFresh checks every program of the test corpus
// and the generated programs through the base's pool of reused checkers,
// in a shuffled order, several times over, and compares each result with a
// new checker's. A reused checker that kept anything from an earlier check
// (a declaration, a variable, a cached relation, a type id) shows up as a
// different result.
func TestCoreCheckPoolMatchesFresh(t *testing.T) {
	var paths []string
	for _, dir := range []string{"../tests/success", "../tests/typecheck_fail", "testdata/generated"} {
		ps, err := filepath.Glob(filepath.Join(dir, "*.msh"))
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, ps...)
	}
	base := NewCoreBase(benchStdlib(t), nil)
	type program struct {
		path string
		file *MShellFile
		want []string
		ok   bool
	}
	var progs []program
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		file, err := NewMShellParser(NewLexer(string(src), nil)).ParseFile()
		if err != nil {
			continue
		}
		diags, arena, names := base.Diagnostics(file)
		want, ok := base.formatCheck(diags, arena, names)
		progs = append(progs, program{p, file, want, ok})
	}
	if len(progs) < 100 {
		t.Fatalf("only %d programs", len(progs))
	}
	rng := rand.New(rand.NewSource(1))
	for pass := range 3 {
		rng.Shuffle(len(progs), func(i, j int) { progs[i], progs[j] = progs[j], progs[i] })
		for _, p := range progs {
			got, ok := base.Check(p.file)
			if ok != p.ok || !slices.Equal(got, p.want) {
				t.Fatalf("pass %d, %s: reused checker gives %v %q, a new one %v %q", pass, p.path, ok, got, p.ok, p.want)
			}
		}
	}
}
