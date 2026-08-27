package calc

import (
	"testing"
	"time"
)

func TestAdd(t *testing.T) {
	time.Sleep(500 * time.Millisecond)
	if Add(1, 1) != 2 {
		t.Errorf("Add(1, 1) = %d, want 2", Add(1, 1))
	}
}
