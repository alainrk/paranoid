package calc

import "testing"

func TestSlow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	if Add(1, 1) != 2 {
		t.Errorf("Add(1, 1) = %d, want 2", Add(1, 1))
	}
}

func TestFast(t *testing.T) {
	if Add(0, 1) != 1 {
		t.Errorf("Add(0, 1) = %d, want 1", Add(0, 1))
	}
	if Add(5, 5) != 10 {
		t.Errorf("Add(5, 5) = %d, want 10", Add(5, 5))
	}
}
