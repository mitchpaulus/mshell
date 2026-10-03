package main

import "testing"

// BenchmarkCoreCheckScript is what `msh --check-types` spends checking a
// script: building the base from the standard library, then checking the
// script. (Parsing is not included.)
func BenchmarkCoreCheckScript(b *testing.B) {
	std := benchStdlib(b)
	for _, s := range []struct{ name, src string }{
		{"oneLine", "1 2 + wl"},
		{"usesStd", "[[1 2] [3]] transpose len wl  [\"a\" \"b\"] tjoin chomp wl  [1 2 3] (2 >) any str wl  [1 2 3 4] 2 chunk len wl"},
	} {
		file := benchParse(b, s.src)
		b.Run(s.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, ok := CoreTypeCheckProgram(file, std, nil); !ok {
					b.Fatal("script does not check")
				}
			}
		})
	}
}
