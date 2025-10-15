package main

import (
	// "fmt"
	// "strings"
	"testing"
)

func TestSimpleBinding(t *testing.T) {
	generic := TypeGeneric{"a", 1}
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
	generic := &TypeHomogeneousList{
		ListType: &TypeHomogeneousList{
			ListType: TypeGeneric{"a", 1},
			Count:    -1,
		},
		Count: -1,
	}

	concrete := &TypeHomogeneousList{
		ListType: &TypeHomogeneousList{
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
