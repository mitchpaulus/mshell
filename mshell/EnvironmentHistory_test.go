package main

import (
	"strconv"
	"testing"
)

func TestEnvironmentHistoryRecordsChangesAndNoOps(t *testing.T) {
	const name = "MSH_ENVIRONMENT_HISTORY_UNIT_TEST"
	t.Setenv(name, "inherited")

	history := NewEnvironmentHistory()
	if err := history.Set(name, "inherited", "same-value source"); err != nil {
		t.Fatal(err)
	}
	if err := history.Set(name, "changed", "set source"); err != nil {
		t.Fatal(err)
	}
	if err := history.Unset(name, "unset source"); err != nil {
		t.Fatal(err)
	}
	if err := history.Unset(name, "missing source"); err != nil {
		t.Fatal(err)
	}

	events := history.Inspect(name)
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
	}

	wants := []struct {
		kind    string
		source  string
		changed bool
	}{
		{"inherited", "Inherited from parent", false},
		{"set", "same-value source", false},
		{"set", "set source", true},
		{"unset", "unset source", true},
		{"unset", "missing source", false},
	}
	for i, want := range wants {
		got := events[i]
		if got.Kind != want.kind || got.Source != want.source || got.Changed != want.changed {
			t.Errorf("event %d: got kind=%q source=%q changed=%t", i, got.Kind, got.Source, got.Changed)
		}
		if got.DateTime.IsZero() {
			t.Errorf("event %d has a zero datetime", i)
		}
	}
}

func TestEnvironmentHistoryKeepsLatest256Events(t *testing.T) {
	const name = "MSH_ENVIRONMENT_HISTORY_LIMIT_TEST"
	t.Setenv(name, "inherited")

	history := NewEnvironmentHistory()
	for i := 0; i < 300; i++ {
		if err := history.Set(name, strconv.Itoa(i), "set source"); err != nil {
			t.Fatal(err)
		}
	}

	events := history.Inspect(name)
	if len(events) != environmentHistoryLimit {
		t.Fatalf("expected %d events, got %d", environmentHistoryLimit, len(events))
	}
	if events[0].Kind != "set" {
		t.Fatalf("expected inherited event to age out, first kind is %q", events[0].Kind)
	}
}

func TestEnvironmentSource(t *testing.T) {
	token := Token{Line: 12, Column: 7, TokenFile: &TokenFile{Path: "/tmp/init.msh"}}
	if got := environmentSource(token); got != "/tmp/init.msh:12:7" {
		t.Fatalf("unexpected source %q", got)
	}

	token.TokenFile = &TokenFile{Path: "REPL"}
	if got := environmentSource(token); got != "REPL:12:7" {
		t.Fatalf("unexpected REPL source %q", got)
	}
}
