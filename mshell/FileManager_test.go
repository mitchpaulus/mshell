package main

import (
	"reflect"
	"testing"
)

func TestAppendUniquePathAppendsWhenMissing(t *testing.T) {
	paths := []string{"/tmp/a", "/tmp/b"}
	got := appendUniquePath(paths, "/tmp/c")
	want := []string{"/tmp/a", "/tmp/b", "/tmp/c"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendUniquePath() = %v, want %v", got, want)
	}
}

func TestAppendUniquePathSkipsDuplicate(t *testing.T) {
	paths := []string{"/tmp/a", "/tmp/b"}
	got := appendUniquePath(paths, "/tmp/a")
	want := []string{"/tmp/a", "/tmp/b"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("appendUniquePath() = %v, want %v", got, want)
	}
}

func TestRemovePathRemovesExisting(t *testing.T) {
	paths := []string{"/tmp/a", "/tmp/b", "/tmp/c"}
	got := removePath(paths, "/tmp/b")
	want := []string{"/tmp/a", "/tmp/c"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("removePath() = %v, want %v", got, want)
	}
}

func TestRemovePathNoOpWhenMissing(t *testing.T) {
	paths := []string{"/tmp/a", "/tmp/b"}
	got := removePath(paths, "/tmp/c")
	want := []string{"/tmp/a", "/tmp/b"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("removePath() = %v, want %v", got, want)
	}
}
