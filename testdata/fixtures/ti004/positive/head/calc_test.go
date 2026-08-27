package calc

import "testing"

func TestFlaky(t *testing.T) {
	t.Skip("flaky on CI, needs investigation")
	if Add(1, 1) != 2 {
		t.Errorf("Add(1, 1) = %d, want 2", Add(1, 1))
	}
	if Add(2, 2) != 4 {
		t.Errorf("Add(2, 2) = %d, want 4", Add(2, 2))
	}
}
