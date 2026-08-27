package calc

import "testing"

func TestDivide(t *testing.T) {
	got, err := Divide(10, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != 5 {
		t.Fatalf("want 5, got %d", got)
	}
}
