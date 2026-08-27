package calc

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 1) != 2 {
		t.Errorf("Add(1, 1) = %d, want 2", Add(1, 1))
	}
}
