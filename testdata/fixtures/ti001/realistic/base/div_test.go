package calc

import "testing"

func TestDiv(t *testing.T) {
	if Div(6, 3) != 2 {
		t.Fatalf("Div(6, 3) = %d, want 2", Div(6, 3))
	}
}

func TestDivByZero(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Div(1, 0) should panic")
		}
	}()
	Div(1, 0)
}
