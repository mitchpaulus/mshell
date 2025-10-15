package main

import (
	"testing"
)

func TestParseLinkHeadersSingle(t *testing.T) {
	t.Parallel()

	input := `<https://api.example.com/resource?page=2>; rel="next"; type="application/json"`
	got, err := ParseLinkHeaders(input)
	if err != nil {
		t.Fatalf("ParseLinkHeaders returned error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 link header, got %d", len(got))
	}

	want := LinkHeader{
		Uri: "https://api.example.com/resource?page=2",
		Rel: "next",
		Params: map[string]string{
			"type": "application/json",
		},
	}

	if got[0].Uri != want.Uri {
		t.Errorf("Uri mismatch: got %q, want %q", got[0].Uri, want.Uri)
	}
	if got[0].Rel != want.Rel {
		t.Errorf("Rel mismatch: got %q, want %q", got[0].Rel, want.Rel)
	}
	if len(got[0].Params) != len(want.Params) {
		t.Fatalf("Params length mismatch: got %d, want %d", len(got[0].Params), len(want.Params))
	}
	for key, wantVal := range want.Params {
		gotVal, ok := got[0].Params[key]
		if !ok {
			t.Fatalf("Params missing key %q", key)
		}
		if gotVal != wantVal {
			t.Errorf("Param %q mismatch: got %q, want %q", key, gotVal, wantVal)
		}
	}
}

func TestParseLinkHeadersMultiple(t *testing.T) {
	t.Parallel()

	input := ` <https://api.example.com/v1?page=2>; rel="next",<https://api.example.com/v1?page=5>; rel="last" `
	got, err := ParseLinkHeaders(input)
	if err != nil {
		t.Fatalf("ParseLinkHeaders returned error: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 link headers, got %d", len(got))
	}

	want := []LinkHeader{
		{
			Uri:    "https://api.example.com/v1?page=2",
			Rel:    "next",
			Params: map[string]string{},
		},
		{
			Uri:    "https://api.example.com/v1?page=5",
			Rel:    "last",
			Params: map[string]string{},
		},
	}

	for i := range want {
		if got[i].Uri != want[i].Uri {
			t.Errorf("header %d Uri mismatch: got %q, want %q", i, got[i].Uri, want[i].Uri)
		}
		if got[i].Rel != want[i].Rel {
			t.Errorf("header %d Rel mismatch: got %q, want %q", i, got[i].Rel, want[i].Rel)
		}
		if len(got[i].Params) != len(want[i].Params) {
			t.Fatalf("header %d Params length mismatch: got %d, want %d", i, len(got[i].Params), len(want[i].Params))
		}
		for key, wantVal := range want[i].Params {
			gotVal, ok := got[i].Params[key]
			if !ok {
				t.Fatalf("header %d Params missing key %q", i, key)
			}
			if gotVal != wantVal {
				t.Errorf("header %d Param %q mismatch: got %q, want %q", i, key, gotVal, wantVal)
			}
		}
	}
}

func TestParseLinkHeadersQuotedAndTokenParams(t *testing.T) {
	t.Parallel()

	input := `<https://example.com/first>; rel="prev"; title="The \"best\" example"; type=json; hreflang="en-US"`
	got, err := ParseLinkHeaders(input)
	if err != nil {
		t.Fatalf("ParseLinkHeaders returned error: %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 link header, got %d", len(got))
	}

	wantParams := map[string]string{
		"title":    `The "best" example`,
		"type":     "json",
		"hreflang": "en-US",
	}

	if got[0].Uri != "https://example.com/first" {
		t.Errorf("Uri mismatch: got %q, want %q", got[0].Uri, "https://example.com/first")
	}
	if got[0].Rel != "prev" {
		t.Errorf("Rel mismatch: got %q, want %q", got[0].Rel, "prev")
	}
	if len(got[0].Params) != len(wantParams) {
		t.Fatalf("Params length mismatch: got %d, want %d", len(got[0].Params), len(wantParams))
	}
	for key, wantVal := range wantParams {
		gotVal, ok := got[0].Params[key]
		if !ok {
			t.Fatalf("Params missing key %q", key)
		}
		if gotVal != wantVal {
			t.Errorf("Param %q mismatch: got %q, want %q", key, gotVal, wantVal)
		}
	}
}

func TestParseLinkHeadersErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"missing angle brackets":      `https://example.com>; rel="next"`,
		"missing rel parameter":       `<https://example.com>; title="no rel"`,
		"missing parameter value":     `<https://example.com>; rel=`,
		"unterminated quoted string":  `<https://example.com>; rel="next"; title="unterminated`,
		"missing comma between links": `<https://example.com>; rel="next" <https://example.com>; rel="last"`,
	}

	for name, input := range tests {
		input := input
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseLinkHeaders(input); err == nil {
				t.Fatalf("expected error, got nil for input %q", input)
			}
		})
	}
}
