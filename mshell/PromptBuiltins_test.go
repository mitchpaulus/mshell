package main

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// These tests tie builtinDeclSource to the Go code and the docs that name
// its members, so a member added, renamed, or removed there cannot be
// forgotten anywhere else.

// builtinEnums are the enums builtinDeclSource declares, by name.
func builtinEnums(t *testing.T) map[string]*MShellEnumDecl {
	t.Helper()
	enums := map[string]*MShellEnumDecl{}
	for _, item := range builtinDeclItems() {
		if d, ok := item.(*MShellEnumDecl); ok {
			enums[d.Name] = d
		}
	}
	return enums
}

// promptInfoFields are the field names of the PromptInfo type.
func promptInfoFields(t *testing.T) []string {
	t.Helper()
	for _, item := range builtinDeclItems() {
		if d, ok := item.(*MShellTypeDecl); ok && d.Name == "PromptInfo" {
			shape, ok := d.Body.(*TypeShapeExpr)
			if !ok {
				t.Fatalf("PromptInfo is a %T, not a shape", d.Body)
			}
			var names []string
			for _, f := range shape.Fields {
				names = append(names, f.Name)
			}
			return names
		}
	}
	t.Fatal("builtinDeclSource does not declare PromptInfo")
	return nil
}

func sameNames(t *testing.T, what string, declared []string, have []string) {
	t.Helper()
	want := append([]string(nil), declared...)
	got := append([]string(nil), have...)
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(want, " ") != strings.Join(got, " ") {
		t.Errorf("%s has %v, but the declaration has %v", what, got, want)
	}
}

// describeItem is a PromptItem member and its payload, for test messages.
func describeItem(member string, payload []MShellObject) string {
	parts := []string{member}
	for _, v := range payload {
		switch v := v.(type) {
		case *MShellEnum:
			parts = append(parts, v.Member)
		case MShellString:
			parts = append(parts, `"`+v.Content+`"`)
		case MShellInt:
			parts = append(parts, strconv.Itoa(v.Value))
		}
	}
	return strings.Join(parts, " ")
}

func keys(m map[string]int) []string {
	var names []string
	for k := range m {
		names = append(names, k)
	}
	return names
}

// TestPromptTablesCoverBuiltinEnums checks that each enum whose members
// map to escape sequence parameters has exactly one entry per member. An
// enum added to builtinDeclSource must be listed here.
func TestPromptTablesCoverBuiltinEnums(t *testing.T) {
	type table struct {
		name string
		m    map[string]int
	}
	tables := map[string][]table{
		"Base16Color":    {{"base16Index", base16Index}},
		"BaseBrightness": {{"baseBrightOffset", baseBrightOffset}},
		"TextAttribute":  {{"textAttrOn", textAttrOn}, {"textAttrOff", textAttrOff}},
		"CursorShape":    {{"cursorShapeParam", cursorShapeParam}},
		// Its members are the cases of renderPrompt's switch, which
		// TestRenderPromptHandlesEveryItem checks.
		"PromptItem": nil,
	}
	enums := builtinEnums(t)
	for name, d := range enums {
		ts, ok := tables[name]
		if !ok {
			t.Errorf("built-in enum %s is not listed in TestPromptTablesCoverBuiltinEnums", name)
			continue
		}
		for _, table := range ts {
			sameNames(t, table.name, d.Members, keys(table.m))
		}
	}
	for name := range tables {
		if enums[name] == nil {
			t.Errorf("TestPromptTablesCoverBuiltinEnums lists %s, which is not a built-in enum", name)
		}
	}
}

// payloadValues are values of a payload type: one of each primitive, and
// every member of a built-in enum.
func payloadValues(t *testing.T, enums map[string]*MShellEnumDecl, typ MShellParseItem) []MShellObject {
	t.Helper()
	switch typ := typ.(type) {
	case *TypePrim:
		switch typ.Tid {
		case TidStr:
			return []MShellObject{MShellString{"x"}}
		case TidInt:
			return []MShellObject{MShellInt{1}}
		}
	case *TypeNamed:
		if d := enums[typ.Name]; d != nil && len(typ.Args) == 0 {
			var values []MShellObject
			for i, m := range d.Members {
				if len(d.MemberPayloads[i]) > 0 {
					t.Fatalf("payloadValues cannot make %s, which has a payload", m)
				}
				values = append(values, enumValue(d.Name, m))
			}
			return values
		}
	}
	t.Fatalf("payloadValues cannot make a value of payload type %T; teach it", typ)
	return nil
}

// TestRenderPromptHandlesEveryItem renders every PromptItem member with
// every combination of enum payloads, and checks each one is understood
// and paints something.
func TestRenderPromptHandlesEveryItem(t *testing.T) {
	enums := builtinEnums(t)
	d := enums["PromptItem"]
	for i, member := range d.Members {
		payloads := [][]MShellObject{nil}
		for _, typ := range d.MemberPayloads[i] {
			var next [][]MShellObject
			for _, prefix := range payloads {
				for _, v := range payloadValues(t, enums, typ) {
					next = append(next, append(append([]MShellObject(nil), prefix...), v))
				}
			}
			payloads = next
		}
		for _, payload := range payloads {
			r, err := renderPrompt(promptList(promptItem(member, payload...)))
			if err != nil {
				t.Errorf("%s: %v", describeItem(member, payload), err)
				continue
			}
			if r.Text == "" && len(r.Styles) == 0 && !r.HasTitle {
				t.Errorf("%s paints nothing", describeItem(member, payload))
			}
		}
	}
}

// TestPromptInfoMatchesType checks that the shell fills in every field of
// PromptInfo, and no others.
func TestPromptInfoMatchesType(t *testing.T) {
	info := (&TermState{}).promptInfo()
	var have []string
	for k := range info.Items {
		have = append(have, k)
	}
	sameNames(t, "promptInfo", promptInfoFields(t), have)
}

// TestPromptBuiltinsDocumented checks that the user docs describe every
// built-in enum member and PromptInfo field, and that the agent docs name
// every built-in enum.
func TestPromptBuiltinsDocumented(t *testing.T) {
	userDoc, err := os.ReadFile("../doc/getting-started.inc.html")
	if err != nil {
		t.Fatal(err)
	}
	agentDoc, err := os.ReadFile("../doc/mshell.md")
	if err != nil {
		t.Fatal(err)
	}
	inCode := func(name string) bool {
		return regexp.MustCompile(`<code>` + regexp.QuoteMeta(name) + `[ <]`).Match(userDoc)
	}
	for name, d := range builtinEnums(t) {
		if !strings.Contains(string(agentDoc), "`"+name+"`") {
			t.Errorf("doc/mshell.md does not name the built-in enum %s", name)
		}
		for _, m := range d.Members {
			if !inCode(m) {
				t.Errorf("doc/getting-started.inc.html does not describe %s, a member of %s", m, name)
			}
		}
	}
	for _, f := range promptInfoFields(t) {
		if !inCode(f) {
			t.Errorf("doc/getting-started.inc.html does not describe %s, a field of PromptInfo", f)
		}
	}
}
