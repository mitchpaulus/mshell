package main

import (
	"fmt"
	"strings"
	"testing"
)

func findMatchBlock(items []MShellParseItem) *MShellParseMatchBlock {
	for _, it := range items {
		if m, ok := it.(*MShellParseMatchBlock); ok {
			return m
		}
	}
	return nil
}

func BenchmarkMatchPatternMicro(b *testing.B) {
	names := func(n int) string {
		var sb strings.Builder
		for i := range n {
			fmt.Fprintf(&sb, "name%d ", i)
		}
		return sb.String()
	}
	ints := func(n int) []MShellObject {
		out := make([]MShellObject, n)
		for i := range out {
			out[i] = MShellInt{i}
		}
		return out
	}
	cases := []struct {
		name    string
		pattern string
		subject MShellObject
		enums   bool
	}{
		{"List3", "[a b c]", &MShellList{Items: ints(3)}, false},
		{"List3Enum", "[a b c]", &MShellList{Items: ints(3)}, true},
		{"List8", "[" + names(8) + "]", &MShellList{Items: ints(8)}, false},
		{"List8Enum", "[" + names(8) + "]", &MShellList{Items: ints(8)}, true},
		{"List32", "[" + names(32) + "]", &MShellList{Items: ints(32)}, false},
		{"List128", "[" + names(128) + "]", &MShellList{Items: ints(128)}, false},
		{"Nested2x2", "[[a b] [c d]]", &MShellList{Items: []MShellObject{&MShellList{Items: ints(2)}, &MShellList{Items: ints(2)}}}, false},
		{"Dict2", "{'x': a, 'y': b}", &MShellDict{Items: map[string]MShellObject{"x": MShellInt{1}, "y": MShellInt{2}}}, false},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			src := "[1] => " + c.pattern
			if c.enums {
				src = "enum Color = red | green | blue | cyan | magenta end\n" + src
			}
			file, err := parseMShellInput(src, &TokenFile{c.name})
			if err != nil {
				b.Fatal(err)
			}
			mb := findMatchBlock(file.Items)
			state := EvalState{EnumMembers: map[string]EnumMemberInfo{}, EnumNames: map[string]bool{}}
			if err := state.RegisterDeclarations(file.Items, file.Definitions); err != nil {
				b.Fatal(err)
			}
			vars := map[string]MShellObject{}
			pat := mb.Arms[0].Pattern
			tok := mb.GetStartToken()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				ok, res := state.matchPattern(pat, mb.Arms[0].RepeatsWord, c.subject, tok, vars)
				if !ok || !res.Success {
					b.Fatal("no match")
				}
			}
		})
	}
}
