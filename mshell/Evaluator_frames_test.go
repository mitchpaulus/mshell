package main

import (
	"os"
	"testing"
)

func evalForTest(t *testing.T, state *EvalState, source string) EvalResult {
	t.Helper()
	file, err := parseMShellInput(source, &TokenFile{"test"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer devNull.Close()
	stack := MShellStack{}
	context := ExecuteContext{
		Variables:      map[string]MShellObject{},
		Pbm:            NewPathBinManager(),
		StandardOutput: devNull,
		StandardError:  devNull,
	}
	callStackItem := CallStackItem{Name: "main", CallStackType: CALLSTACKFILE}
	return state.Evaluate(file.Items, &stack, context, file.Definitions, callStackItem)
}

// A failure deep inside quotations run by builtins must leave the state as
// it was, since the REPL keeps using the same EvalState afterwards.
func TestEvaluateRestoresStateAfterFailure(t *testing.T) {
	state := EvalState{CallStack: make([]CallStackItem, 0, 10)}

	// Fail inside a loop, inside a map, inside a loop.
	result := evalForTest(t, &state, `( [1] ( ( 1 2 + "a" + ) loop ) map ) loop`)
	if result.Success {
		t.Fatalf("expected failure")
	}
	if len(state.frames) != 0 || len(state.CallStack) != 0 || state.LoopDepth != 0 {
		t.Fatalf("state not restored: %d frames, %d call stack items, loop depth %d", len(state.frames), len(state.CallStack), state.LoopDepth)
	}

	// With the loop depth restored, a stray break is an error again.
	if result := evalForTest(t, &state, `break`); result.Success {
		t.Fatalf("expected 'break' outside a loop to fail")
	}
}

func TestNeverUsesVariables(t *testing.T) {
	cases := []struct {
		body string
		want bool
	}{
		{`1 +`, true},
		{`dup 0 > if 1 else 2 end`, true},
		{`[1 2] {"a": 3}`, true},
		{`@x 1 +`, true},
		{`x!`, false},
		{`[x!]`, false},
		{`true if x! end`, false},
		{`(1 +) map`, false},
		{`(1) loop`, false},
		{`$"text"`, true},
		{`$"{1}"`, true},
		{`$"{@x}"`, true},
		{`$"{1 x! @x}"`, false},
		{`$"{(1) 0 get}"`, false},
		{`match _: 1 end`, false},
	}
	for _, c := range cases {
		file, err := parseMShellInput("def d (--) "+c.body+" end", &TokenFile{"test"})
		if err != nil {
			t.Fatalf("parse %q: %v", c.body, err)
		}
		if got := file.Definitions[0].NeverUsesVariables; got != c.want {
			t.Errorf("%q: NeverUsesVariables = %v, want %v", c.body, got, c.want)
		}
	}
}
