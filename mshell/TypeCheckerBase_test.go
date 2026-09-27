package main

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
)

// TestCheckerBaseMatchesFreshChecks checks every test script from one
// shared CheckerBase on several goroutines at once, as the language server
// does, and requires the same diagnostics as checking each from scratch.
// Run with -race to also catch a checker writing into the shared base.
func TestCheckerBaseMatchesFreshChecks(t *testing.T) {
	std := benchStdlib(t)
	var files []*MShellFile
	for _, pattern := range []string{"../tests/success/*.msh", "../tests/typecheck_fail/*.msh", "../tests/msh-scripts/*"} {
		paths, _ := filepath.Glob(pattern)
		for _, p := range paths {
			src, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			file, err := NewMShellParser(NewLexer(string(src), nil)).ParseFile()
			if err != nil {
				continue
			}
			files = append(files, file)
		}
	}

	// Checked the way every check was set up before CheckerBase existed.
	want := make([][]string, len(files))
	for i, f := range files {
		c := NewChecker(NewTypeArena(), NewNameTable())
		c.RegisterStdlibSigs(std)
		c.CheckProgram(f)
		want[i] = severeErrors(c)
	}

	base := NewCheckerBase(std)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i, f := range files {
				c := base.NewChecker()
				c.CheckProgram(f)
				if got := severeErrors(c); !slices.Equal(got, want[i]) {
					t.Errorf("file %d: shared base gave %q, fresh check gave %q", i, got, want[i])
				}
			}
		}()
	}
	wg.Wait()
}

func severeErrors(c *Checker) []string {
	var out []string
	for _, e := range c.errors {
		if e.Severity == SeverityError {
			out = append(out, e.Format(c.arena, c.names))
		}
	}
	return out
}

// TestCheckerBaseOverloadsStayPerChecker adds an overload to the same name
// in two checkers from one base, with the base's slice given spare room,
// and requires each to see only its own.
func TestCheckerBaseOverloadsStayPerChecker(t *testing.T) {
	b := NewCheckerBase(nil)
	id := b.c.names.Intern("dup")
	sigs := b.c.nameBuiltins[id]
	b.c.nameBuiltins[id] = append(make([]QuoteSig, 0, len(sigs)+4), sigs...)

	c1, c2 := b.NewChecker(), b.NewChecker()
	c1.nameBuiltins[id] = append(c1.nameBuiltins[id], QuoteSig{Outputs: []TypeId{TidInt}})
	c2.nameBuiltins[id] = append(c2.nameBuiltins[id], QuoteSig{Outputs: []TypeId{TidStr}})

	last := func(c *Checker) TypeId {
		s := c.nameBuiltins[id]
		return s[len(s)-1].Outputs[0]
	}
	if last(c1) != TidInt || last(c2) != TidStr || len(b.c.nameBuiltins[id]) != len(sigs) {
		t.Fatalf("overloads leaked between checkers: c1 %d, c2 %d, base has %d", last(c1), last(c2), len(b.c.nameBuiltins[id]))
	}
}
