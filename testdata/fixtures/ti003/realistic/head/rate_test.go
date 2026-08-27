package rate

import "testing"

func TestAllow(t *testing.T) {
	if Allow(0) {
		t.Error("Allow(0) should be false")
	}
	if !Allow(1) {
		t.Error("Allow(1) should be true")
	}
	if Allow(100) {
		t.Error("Allow(100) should be false")
	}
}

func TestBurst(t *testing.T) {
	if !Burst(3) {
		t.Error("Burst(3) should be true")
	}
	if Burst(4) {
		t.Error("Burst(4) should be false")
	}
	if !Burst(0) {
		t.Error("Burst(0) should be true")
	}
}

func TestLevel(t *testing.T) {
	if Level(0) != 0 {
		t.Errorf("Level(0) = %d, want 0", Level(0))
	}
	if Level(4) != 2 {
		t.Errorf("Level(4) = %d, want 2", Level(4))
	}
	if Level(10) != 5 {
		t.Errorf("Level(10) = %d, want 5", Level(10))
	}
}
