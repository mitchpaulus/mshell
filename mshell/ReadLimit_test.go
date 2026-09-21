package main

import (
	"errors"
	"strings"
	"testing"
)

func TestParseReadLimit(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"0", 0, true},
		{"123", 123, true},
		{" 4096 ", 4096, true},
		{"1k", 1024, true},
		{"1K", 1024, true},
		{"1KB", 1024, true},
		{"1KiB", 1024, true},
		{"2M", 2 * 1024 * 1024, true},
		{"2MB", 2 * 1024 * 1024, true},
		{"1G", 1024 * 1024 * 1024, true},
		{"50MiB", 50 * 1024 * 1024, true},
		{"", 0, false},
		{"-1", 0, false},
		{"abc", 0, false},
		{"1.5M", 0, false},
		{"1T", 0, false},
		{"99999999999999999999", 0, false},
		{"9999999999G", 0, false},
	}
	for _, c := range cases {
		got, err := parseReadLimit(c.in)
		if c.ok && err != nil {
			t.Errorf("parseReadLimit(%q) unexpected error: %v", c.in, err)
			continue
		}
		if !c.ok && err == nil {
			t.Errorf("parseReadLimit(%q) expected error, got %d", c.in, got)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("parseReadLimit(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// neverEnding is a reader that never reaches EOF.
type neverEnding struct{}

func (neverEnding) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func TestReadAllBounded(t *testing.T) {
	// Empty input.
	got, err := readAllBounded(strings.NewReader(""), 10)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty: got %q, err %v", got, err)
	}

	// Below the limit.
	got, err = readAllBounded(strings.NewReader("hello"), 10)
	if err != nil || string(got) != "hello" {
		t.Fatalf("below: got %q, err %v", got, err)
	}

	// Exactly the limit succeeds.
	got, err = readAllBounded(strings.NewReader("0123456789"), 10)
	if err != nil || string(got) != "0123456789" {
		t.Fatalf("exact: got %q, err %v", got, err)
	}

	// One byte over fails.
	_, err = readAllBounded(strings.NewReader("0123456789a"), 10)
	var limitErr errReadLimitExceeded
	if !errors.As(err, &limitErr) || limitErr.limit != 10 {
		t.Fatalf("over: expected errReadLimitExceeded{10}, got %v", err)
	}

	// A stream that never ends fails promptly instead of growing forever.
	_, err = readAllBounded(neverEnding{}, 1024)
	if !errors.As(err, &limitErr) {
		t.Fatalf("nonterminating: expected errReadLimitExceeded, got %v", err)
	}

	// Zero means unlimited.
	got, err = readAllBounded(strings.NewReader("0123456789a"), 0)
	if err != nil || string(got) != "0123456789a" {
		t.Fatalf("unlimited: got %q, err %v", got, err)
	}
}
