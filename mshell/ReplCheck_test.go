package main

import (
	"errors"
	"testing"
)

// TestReplClearStack checks that after the REPL clears its stack the
// checker has no values, and checks later lines from an empty stack.
func TestReplClearStack(t *testing.T) {
	r := &replChecker{session: NewCoreBase(nil, nil).NewSession(2)}
	if _, ok := sessionLine(t, r.session, `1 "a"`); !ok {
		t.Fatal("refused")
	}
	r.commit(MShellStack{})
	r.clearStack()
	if n := r.session.Len(); n != 0 {
		t.Fatalf("checker has %d values after the clear", n)
	}
	if _, ok := sessionLine(t, r.session, `drop`); ok {
		t.Error("drop on the cleared stack checked")
	}
	if out, ok := sessionLine(t, r.session, `3 4 +`); !ok {
		t.Errorf("refused: %v", out)
	}
}

// TestLostTrackClears checks the answers to the clear-or-exit question:
// only a clear answer goes on, an unknown one asks again, and the end of
// input exits.
func TestLostTrackClears(t *testing.T) {
	cases := []struct {
		answers []string
		want    bool
	}{
		{[]string{"c"}, true},
		{[]string{" Clear \n"}, true},
		{[]string{"e"}, false},
		{[]string{"exit"}, false},
		{[]string{"", "yes", "c"}, true},
		{[]string{"maybe"}, false}, // then the end of input
		{nil, false},
	}
	for _, tc := range cases {
		i := 0
		read := func(string) (string, error) {
			if i == len(tc.answers) {
				return "", errors.New("EOF")
			}
			i++
			return tc.answers[i-1], nil
		}
		if got := lostTrackClears(read); got != tc.want {
			t.Errorf("answers %q: got %v, want %v", tc.answers, got, tc.want)
		}
	}
}
