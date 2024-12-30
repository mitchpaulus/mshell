package main

import (
	// "fmt"
	// "strings"
	"testing"
)

func TestSimpleBinding(t *testing.T) {
	generic := TypeGeneric{"a"}
	concrete := TypeInt{}

	binding, err := generic.Bind(concrete)
	if err != nil {
		t.Error("Binding failed")
	}

	if len(binding) != 1 {
		t.Error("Binding failed")
	}

	if !binding[0].Type.Equals(concrete) {
		t.Error("Binding failed")
	}
}

func TestRecursiveBinding(t *testing.T) {
	generic := &TypeList{
		ListType: &TypeList{
			ListType: TypeGeneric{"a"},
			Count:    -1,
		},
		Count: -1,
	}

	concrete := &TypeList{
		ListType: &TypeList{
			ListType: TypeInt{},
			Count:    -1,
		},
		Count: -1,
	}

	binding, err := generic.Bind(concrete)
	if err != nil {
		t.Error("Binding failed")
	}

	if len(binding) != 1 {
		t.Error("Binding failed")
	}

	if !binding[0].Type.Equals(TypeInt{}) {
		t.Error("Binding failed")
	}
}
