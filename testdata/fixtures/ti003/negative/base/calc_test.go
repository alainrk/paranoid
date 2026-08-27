package calc

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Errorf("Add(1, 2) = %d, want 3", Add(1, 2))
	}
	if Add(2, 2) != 4 {
		t.Errorf("Add(2, 2) = %d, want 4", Add(2, 2))
	}
}
