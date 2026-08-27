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

func TestMul(t *testing.T) {
	if Mul(2, 2) != 4 {
		t.Errorf("Mul(2, 2) = %d, want 4", Mul(2, 2))
	}
	if Mul(2, 3) != 6 {
		t.Errorf("Mul(2, 3) = %d, want 6", Mul(2, 3))
	}
	if Mul(0, 5) != 0 {
		t.Errorf("Mul(0, 5) = %d, want 0", Mul(0, 5))
	}
	if Mul(1, 5) != 5 {
		t.Errorf("Mul(1, 5) = %d, want 5", Mul(1, 5))
	}
}
