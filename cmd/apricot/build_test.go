package main

import "testing"

func TestSliceContains_Found(t *testing.T) {
	if !sliceContains([]string{"a", "b", "c"}, "b") {
		t.Error("expected true")
	}
}

func TestSliceContains_NotFound(t *testing.T) {
	if sliceContains([]string{"a", "b", "c"}, "d") {
		t.Error("expected false")
	}
}

func TestSliceContains_Empty(t *testing.T) {
	if sliceContains([]string{}, "a") {
		t.Error("expected false for empty slice")
	}
}
