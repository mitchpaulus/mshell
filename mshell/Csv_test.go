package main

import (
	"fmt"
	"testing"
	"time"
)

func TestIso8601MatchesPaddedFormat(t *testing.T) {
	for _, year := range []int{-5, 0, 7, 99, 999, 2024, 10000} {
		tm := time.Date(year, 3, 4, 5, 6, 7, 0, time.UTC)
		want := fmt.Sprintf("%d-%02d-%02dT%02d:%02d:%02d", year, 3, 4, 5, 6, 7)
		if got := (&MShellDateTime{Time: tm}).Iso8601(); got != want {
			t.Errorf("year %d: got %q, want %q", year, got, want)
		}
	}
}
