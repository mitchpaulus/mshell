package main

import "testing"

const loopHead = `0 i! ( @i 100000 >= if break end `
const loopTail = ` @i 1 + i! ) loop`

var matchBenchCommon = []struct{ name, src string }{
	{"Baseline", `[1 2 3] xs! ` + loopHead + `@xs drop` + loopTail},
	{"ListNames", `[1 2 3] xs! ` + loopHead + `@xs match [a b c] : @a drop, end` + loopTail},
	{"ListLenMissThenHit", `[1 2 3] xs! ` + loopHead + `@xs match [a b] : 0 drop, [a b c] : @a drop, end` + loopTail},
	{"ListSpread", `[1 2 3] xs! ` + loopHead + `@xs match [a ...rest] : @a drop, end` + loopTail},
	{"DictNames", `{'x': 1, 'y': 2} d! ` + loopHead + `@d match {'x': a, 'y': b} : @a drop, end` + loopTail},
	{"DictMissThenHit", `{'x': 1, 'y': 2} d! ` + loopHead + `@d match {'z': a} : 0 drop, {'x': a, 'y': b} : @a drop, end` + loopTail},
	{"Assertive", `[1 2 3] xs! ` + loopHead + `@xs => [a b c]` + loopTail},
	{"Names8", `[1 2 3 4 5 6 7 8] xs! ` + loopHead + `@xs => [a b c d e f g h]` + loopTail},
}

var matchBenchBranch = []struct{ name, src string }{
	{"LitHeadRoute", `['POST' '/a'] xs! ` + loopHead + `@xs match ['GET' p] : 0 drop, ['PUT' p] : 0 drop, ['POST' p] : @p drop, _ : 0 drop, end` + loopTail},
	{"LitHeadMissAfterBind", `['/a' 'x'] xs! ` + loopHead + `@xs match [p 'GET'] : 0 drop, [p 'PUT'] : 0 drop, [p 'x'] : @p drop, end` + loopTail},
	{"Nested", `[[1 2] [3 4]] xs! ` + loopHead + `@xs match [[a b] [c d]] : @a drop, end` + loopTail},
	{"EnumMember", "enum Tok = lp | rp | num int end\n" + `[lp 5 rp] xs! ` + loopHead + `@xs match [rp n lp] : 0 drop, [lp n rp] : @n drop, end` + loopTail},
	{"Names8Enum", "enum Color = red | green | blue end\n" + `[1 2 3 4 5 6 7 8] xs! ` + loopHead + `@xs => [a b c d e f g h]` + loopTail},
	{"Names16Enum", "enum Color = red | green | blue end\n" + `[1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16] xs! ` + loopHead + `@xs => [x1 x2 x3 x4 x5 x6 x7 x8 x9 x10 x11 x12 x13 x14 x15 x16]` + loopTail},
	{"Names16", `[1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16] xs! ` + loopHead + `@xs => [x1 x2 x3 x4 x5 x6 x7 x8 x9 x10 x11 x12 x13 x14 x15 x16]` + loopTail},
}

func runMatchBench(b *testing.B, set []struct{ name, src string }) {
	for _, bench := range set {
		b.Run(bench.name, func(b *testing.B) {
			file, err := parseMShellInput(bench.src, &TokenFile{bench.name})
			if err != nil {
				b.Fatalf("parse: %v", err)
			}
			pbm := NewPathBinManager()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state := EvalState{CallStack: make([]CallStackItem, 0, 10)}
				if err := state.RegisterDeclarations(file.Items, file.Definitions); err != nil {
					b.Fatal(err)
				}
				stack := MShellStack{}
				context := ExecuteContext{Variables: map[string]MShellObject{}, Pbm: pbm}
				callStackItem := CallStackItem{Name: "main", CallStackType: CALLSTACKFILE}
				result := state.Evaluate(file.Items, &stack, context, file.Definitions, callStackItem)
				if !result.Success {
					b.Fatalf("evaluation failed")
				}
			}
		})
	}
}

func BenchmarkMatchCommon(b *testing.B) { runMatchBench(b, matchBenchCommon) }
func BenchmarkMatchBranch(b *testing.B) { runMatchBench(b, matchBenchBranch) }
