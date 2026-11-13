package main

import (
	"strings"
	"testing"
)

// TestCutOnceMatchesSplitN checks that cutOnce gives what
// strings.SplitN(s, sep, 2) does, so splitOnce splits as split does.
func TestCutOnceMatchesSplitN(t *testing.T) {
	for _, s := range []string{"", "a", "ab", "abc", "héllo", "é", "\xff", "\xffa", "a=b", "=", "a=", "=b", "a=b=c", "a::b::c"} {
		for _, sep := range []string{"", "=", "::", "x"} {
			want := strings.SplitN(s, sep, 2)
			before, after, found := cutOnce(s, sep)
			if found != (len(want) == 2) || found && (before != want[0] || after != want[1]) {
				t.Errorf("%q %q: got %q %q %v, want %q", s, sep, before, after, found, want)
			}
		}
	}
}
