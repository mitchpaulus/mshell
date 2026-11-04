package main

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkParseMatchPatterns(b *testing.B) {
	var sb strings.Builder
	for i := range 500 {
		fmt.Fprintf(&sb, "[1 2 3] match [a%d b c] : @a%d drop, ['x' [p q] ...rest] : 0 drop, {'k': v} : 0 drop, _ : 0 drop, end\n", i, i)
	}
	src := sb.String()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := parseMShellInput(src, &TokenFile{"p"}); err != nil {
			b.Fatal(err)
		}
	}
}
