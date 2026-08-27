package calc

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Fatal("bad sum")
	}
	if Add(0, 0) != 0 {
		t.Fatal("bad zero")
	}
}

func TestSub(t *testing.T) {
	if Sub(3, 2) != 1 {
		t.Fatal("bad difference")
	}
}
