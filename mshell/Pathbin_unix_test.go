//go:build linux || darwin

package main

import "testing"

// binPaths must push plain string values, like every other builtin.
func TestDebugListPushesStringValues(t *testing.T) {
	pbm := &PathBinManager{binaryPaths: map[string]string{"tool": "/usr/bin/tool"}}
	list := pbm.DebugList()
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(list.Items))
	}
	inner, ok := list.Items[0].(*MShellList)
	if !ok {
		t.Fatalf("expected a list, got %T", list.Items[0])
	}
	for i, want := range []string{"tool", "/usr/bin/tool"} {
		s, ok := inner.Items[i].(MShellString)
		if !ok {
			t.Fatalf("item %d: expected MShellString, got %T", i, inner.Items[i])
		}
		if s.Content != want {
			t.Fatalf("item %d: expected %q, got %q", i, want, s.Content)
		}
	}
}
