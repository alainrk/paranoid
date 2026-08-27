package calc

import "testing"

func TestAdd(t *testing.T) {
	cases := []struct{ a, b, want int }{
		{1, 1, 2},
		{2, 2, 4},
	}
	for _, c := range cases {
		if Add(c.a, c.b) != c.want {
			t.Errorf("Add(%d, %d) = %d, want %d", c.a, c.b, Add(c.a, c.b), c.want)
		}
	}
}
