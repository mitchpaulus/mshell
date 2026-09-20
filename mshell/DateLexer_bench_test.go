package main

import "testing"

var benchDateInputs = []struct{ name, in string }{
	{"iso", "2025-06-16"},
	{"isoT", "2025-04-30T17:58:18.5467067Z"},
	{"usTime", "12/16/2025 2:56:13 PM"},
	{"monthWord", "Jun 5, 2027"},
	{"dmy", "16/06/2025"},
	{"ambiguous", "06/05/2025"},
	{"garbage", "hello world"},
}

func BenchmarkParseDateTime(b *testing.B) {
	for _, c := range benchDateInputs {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			order := new(DateOrder)
			for i := 0; i < b.N; i++ {
				*order = ORDER_UNSET
				ParseDateTime(c.in, order)
			}
		})
	}
}
