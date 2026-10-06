package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"testing"
)

// builtinSwitchCases are the words evaluateBuiltinToken's switch on
// t.Lexeme handles, read from Evaluator.go.
func builtinSwitchCases(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "Evaluator.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		if f, ok := decl.(*ast.FuncDecl); ok && f.Name.Name == "evaluateBuiltinToken" {
			fn = f
		}
	}
	if fn == nil {
		t.Fatal("Evaluator.go has no evaluateBuiltinToken")
	}
	var cases []string
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if cases != nil {
			return false
		}
		sw, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		sel, ok := sw.Tag.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Lexeme" {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "t" {
			return true
		}
		cases = []string{}
		for _, stmt := range sw.Body.List {
			for _, e := range stmt.(*ast.CaseClause).List {
				lit, ok := e.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Fatalf("a case of the builtin switch is not a string literal: %T", e)
				}
				word, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				cases = append(cases, word)
			}
		}
		return false
	})
	if len(cases) == 0 {
		t.Fatal("found no switch on t.Lexeme in evaluateBuiltinToken")
	}
	return cases
}

// TestBuiltinSwitchCasesAreListed checks that every word the builtin
// switch handles is in BuiltInList. Declarations check BuiltInList, so no
// enum, enum member or type can take a builtin's name. The evaluator relies
// on that: it looks a word up as an enum member only after the switch has
// missed, so a builtin missing from the list would shadow a member of the
// same name.
func TestBuiltinSwitchCasesAreListed(t *testing.T) {
	var missing []string
	for _, word := range builtinSwitchCases(t) {
		if _, ok := BuiltInList[word]; !ok {
			missing = append(missing, word)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("the builtin switch handles words missing from BuiltInList (BuiltInList.go): %v", missing)
	}
}
