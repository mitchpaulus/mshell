package main

import (
	"os"
	"path/filepath"
	"testing"
)

// startupBenchFiles parses std.msh and the init file at MSHBENCHINIT (or the
// user's init.msh for this version), as the startup files load. A missing
// init file is an empty one.
func startupBenchFiles(tb testing.TB) (std, init *MShellFile) {
	tb.Helper()
	src, err := os.ReadFile("../lib/std.msh")
	if err != nil {
		tb.Fatal(err)
	}
	std, err = parseMShellInput(string(src), &TokenFile{"std.msh"})
	if err != nil {
		tb.Fatal(err)
	}
	path := os.Getenv("MSHBENCHINIT")
	if path == "" {
		dir, _ := getStartupConfigDir()
		path = filepath.Join(dir, mshellVersion, "init.msh")
	}
	src, err = os.ReadFile(path)
	if err != nil {
		return std, &MShellFile{}
	}
	init, err = parseMShellInput(string(src), &TokenFile{path})
	if err != nil {
		tb.Fatal(err)
	}
	return std, init
}

func BenchmarkStartupCheck(b *testing.B) {
	std, init := startupBenchFiles(b)
	defs := append(append([]MShellDefinition{}, std.Definitions...), init.Definitions...)
	decls := append(declarationItems(std.Items), declarationItems(init.Items)...)
	// init's top level, less its declarations, which are in the base.
	top := &MShellFile{}
	for _, item := range init.Items {
		switch item.(type) {
		case *MShellTypeDecl, *MShellEnumDecl:
		default:
			top.Items = append(top.Items, item)
		}
	}
	b.Run("parseStd", func(b *testing.B) {
		src, _ := os.ReadFile("../lib/std.msh")
		b.ReportAllocs()
		for b.Loop() {
			parseMShellInput(string(src), &TokenFile{"std.msh"})
		}
	})
	b.Run("builtinsOnly", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			NewCoreBase(nil, nil)
		}
	})
	b.Run("sigsOnly", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			newCoreBase(defs, decls, true)
		}
	})
	b.Run("everyBody", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if errs := NewCoreBase(defs, decls).StartupErrors(); len(errs) > 0 {
				b.Fatal(errs)
			}
		}
	})
	b.Run("everyStdBody", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			NewCoreBase(std.Definitions, declarationItems(std.Items))
		}
	})
	b.Run("everyBodyAndInitTop", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			base := NewCoreBase(defs, decls)
			if errs, ok := base.Check(top); !ok {
				b.Fatal(errs)
			}
		}
	})
}

