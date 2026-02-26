package main

import (
	"os"
	"testing"
)

func TestHistory(t *testing.T) {
	path := "test.mshell_history"
	_ = WriteToHistory(os.Getenv("HOME"), "echo hello", path)
}

func TestAllMatchesAreFiles(t *testing.T) {
	if allMatchesAreFiles(nil) {
		t.Fatalf("expected false for empty matches")
	}

	if !allMatchesAreFiles([]TabMatch{
		{TabMatchType: TABMATCHFILE, Match: "Floor 0/"},
		{TabMatchType: TABMATCHFILE, Match: "Floor 1/"},
	}) {
		t.Fatalf("expected true when all matches are files")
	}

	if allMatchesAreFiles([]TabMatch{
		{TabMatchType: TABMATCHFILE, Match: "Floor 0/"},
		{TabMatchType: TABMATCHDEF, Match: "FloorHelper"},
	}) {
		t.Fatalf("expected false for mixed match types")
	}
}

func TestBuildCompletionInsertPrefersPathQuoteForFileMatches(t *testing.T) {
	state := TermState{l: NewLexer("", nil)}

	got := state.buildCompletionInsert("Floor 0/", LITERAL, true)
	want := "`Floor 0/"
	if got != want {
		t.Fatalf("buildCompletionInsert() = %q, want %q", got, want)
	}

	got = state.buildCompletionInsert("Floor 0.txt", LITERAL, true)
	want = "`Floor 0.txt` "
	if got != want {
		t.Fatalf("buildCompletionInsert() = %q, want %q", got, want)
	}

	got = state.buildCompletionInsert("Floor 0/", LITERAL, false)
	want = "'Floor 0/'"
	if got != want {
		t.Fatalf("buildCompletionInsert() = %q, want %q", got, want)
	}
}

func TestBuildSharedCompletionInsertUsesBacktickForFilePrefixes(t *testing.T) {
	state := TermState{l: NewLexer("", nil)}

	got := state.buildSharedCompletionInsert("Floor ", LITERAL, true)
	want := "`Floor "
	if got != want {
		t.Fatalf("buildSharedCompletionInsert() = %q, want %q", got, want)
	}

	got = state.buildSharedCompletionInsert("Floor ", LITERAL, false)
	want = "'Floor "
	if got != want {
		t.Fatalf("buildSharedCompletionInsert() = %q, want %q", got, want)
	}
}
