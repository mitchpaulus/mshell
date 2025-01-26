package main

import (
	"testing"
	"strconv"
)

func ModifySlice(myslice []int) {
	_ = append(myslice, 1)
	_ = append(myslice, 2)
	_ = append(myslice, 3)
	_ = append(myslice, 4)
}

func TestSliceCall(t *testing.T) {
	s := make([]int, 0)
	ModifySlice(s)
	if len(s) != 0 {
		t.Errorf("Expected length of 0, but got %d", len(s))
	}
}

func TestAtoI(t *testing.T) {
	test := "01"
	_, err := strconv.Atoi(test)

	if err != nil {
		t.Errorf("Could not parse")
	}
}
