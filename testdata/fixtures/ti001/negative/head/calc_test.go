package calc

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Fatalf("Add(1, 2) = %d, want 3", Add(1, 2))
	}
}

func TestDouble(t *testing.T) {
	if double(2) != 4 {
		t.Fatalf("double(2) = %d, want 4", double(2))
	}
}
