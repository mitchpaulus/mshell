package main

import (
	"testing"
)

func TestVersionSort(t *testing.T) {
	cmp := VersionSortComparer("1.20", "1.7")

	if cmp != 1 {
		t.Errorf("Expected 1, got %d", cmp)
	}

	cmp = VersionSortComparer("1.7", "1.20")
	if cmp != -1 {
		t.Errorf("Expected -1, got %d", cmp)
	}

	cmp = VersionSortComparer("1.20", "abc")
	if cmp != -1 {
		t.Errorf("Expected -1, got %d", cmp)
	}

	cmp = VersionSortComparer("-1", "2")
	if cmp != -1 {
		t.Errorf("Expected -1, got %d", cmp)
	}
}
