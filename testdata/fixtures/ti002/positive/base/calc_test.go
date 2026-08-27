package calc

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Errorf("Add(1, 2) = %d, want 3", Add(1, 2))
	}
	if Add(0, 0) != 0 {
		t.Errorf("Add(0, 0) = %d, want 0", Add(0, 0))
	}
	if Add(-1, 1) != 0 {
		t.Errorf("Add(-1, 1) = %d, want 0", Add(-1, 1))
	}
	if Add(2, 2) != 4 {
		t.Errorf("Add(2, 2) = %d, want 4", Add(2, 2))
	}
}

func TestSub(t *testing.T) {
	if Sub(3, 1) != 2 {
		t.Errorf("Sub(3, 1) = %d, want 2", Sub(3, 1))
	}
}
