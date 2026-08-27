package net

import "testing"

func TestPing(t *testing.T) {
	for i := 0; i < 3; i++ {
		if !Ping() {
			t.Errorf("ping failed")
			continue
		}
		break
	}
}
