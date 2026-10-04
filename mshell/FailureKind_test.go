package main

import "testing"

// runForFailure runs src with no type check and returns the kind of the
// failure it stops with, NoFailure if it succeeds.
func runForFailure(t *testing.T, src string) FailureKind {
	t.Helper()
	file, err := parseMShellInput(src, &TokenFile{"kind.msh"})
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	state := EvalState{CallStack: make([]CallStackItem, 0, 10)}
	stack := MShellStack{}
	context := ExecuteContext{Variables: map[string]MShellObject{}, Pbm: NewPathBinManager()}
	callStackItem := CallStackItem{Name: "main", CallStackType: CALLSTACKFILE}
	result := state.Evaluate(file.Items, &stack, context, file.Definitions, callStackItem)
	if result.Success {
		return NoFailure
	}
	return state.FailureKind
}

// TestFailureKinds checks the kind of a sample of failures, including each
// helper that can fail either way (failErr).
func TestFailureKinds(t *testing.T) {
	cases := []struct {
		src  string
		want FailureKind
	}{
		{`1 "a" +`, TypeMismatch},
		{`5 x`, TypeMismatch},
		{`+`, TypeMismatch},
		{`[1 2] (drop) map`, TypeMismatch},
		{`none ?`, CheckedFailure},
		{`1 0 /`, CheckedFailure},
		{`"[" parseJson`, CheckedFailure},
		{`"/no/such/file" readFile`, CheckedFailure},
		{`@unset`, CheckedFailure},
		// Index and slice helpers: out of range is checked, a kind that
		// cannot be indexed is a mismatch.
		{`[1 2] :5:`, CheckedFailure},
		{`[1 2] 1:9`, CheckedFailure},
		{`[1 2] 5 nth`, CheckedFailure},
		{`true :0:`, TypeMismatch},
		// Option helpers.
		{`1000 {"grouping": [0]} numFmt`, CheckedFailure},
		{`1000 {"grouping": ["a"]} numFmt`, TypeMismatch},
		// toGrid: no rows is checked, a row that is not a list a mismatch.
		{`[] toGrid`, CheckedFailure},
		{`[5] toGrid`, TypeMismatch},
		// tryAs: a target that does not resolve is a mismatch.
		{`5 tryAs Nope`, TypeMismatch},
	}
	for _, c := range cases {
		if got := runForFailure(t, c.src); got != c.want {
			t.Errorf("%s: got %s, want %s", c.src, got, c.want)
		}
	}
}
