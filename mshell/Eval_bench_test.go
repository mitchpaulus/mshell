package main

import (
	"os"
	"testing"
)

// Interpreter hot-path benchmarks. Each program is parsed once; every
// iteration evaluates it against a fresh stack and variable map.
//
//   go test -run '^$' -bench BenchmarkEval -benchmem -count 5

const benchSeqIff = `
def seqIff (int -- [int])
    max!
    0 current!
    [] seq-accum!
    (
        @current @max >= (break) iff
        @seq-accum @current append drop
        @current 1 + current!
    ) loop
    @seq-accum
end
`

const benchSeqIf = `
def seqIf (int -- [int])
    max!
    0 current!
    [] seq-accum!
    (
        @current @max >= if break end
        @seq-accum @current append drop
        @current 1 + current!
    ) loop
    @seq-accum
end
`

var evalBenchmarks = []struct {
	name   string
	source string
}{
	{"CounterLoop", `100000 max! 0 current! ( @current @max >= if break end @current 1 + current! ) loop`},
	{"SeqIff", benchSeqIff + `100000 seqIff drop`},
	{"SeqIf", benchSeqIf + `100000 seqIf drop`},
	{"SeqEachStd", `100000 seq each. drop end`},
	{"DefCall", "def inc (int -- int) 1 + end\n" + `0 i! ( @i 100000 >= if break end @i inc i! ) loop`},
	{"MapQuote", `100000 seq (1 +) map drop`},
}

func loadBenchStdlib(b *testing.B) []MShellDefinition {
	b.Helper()
	source, err := os.ReadFile("../lib/std.msh")
	if err != nil {
		b.Fatalf("reading std.msh: %v", err)
	}
	parsed, err := parseMShellInput(string(source), &TokenFile{"../lib/std.msh"})
	if err != nil {
		b.Fatalf("parsing std.msh: %v", err)
	}
	return parsed.Definitions
}

func BenchmarkEval(b *testing.B) {
	stdDefs := loadBenchStdlib(b)

	for _, bench := range evalBenchmarks {
		b.Run(bench.name, func(b *testing.B) {
			file, err := parseMShellInput(bench.source, &TokenFile{bench.name})
			if err != nil {
				b.Fatalf("parse: %v", err)
			}
			definitions := append(append([]MShellDefinition{}, stdDefs...), file.Definitions...)
			pbm := NewPathBinManager()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				state := EvalState{CallStack: make([]CallStackItem, 0, 10)}
				stack := MShellStack{}
				context := ExecuteContext{Variables: map[string]MShellObject{}, Pbm: pbm}
				callStackItem := CallStackItem{Name: "main", CallStackType: CALLSTACKFILE}
				result := state.Evaluate(file.Items, &stack, context, definitions, callStackItem)
				if !result.Success {
					b.Fatalf("evaluation failed")
				}
			}
		})
	}
}
