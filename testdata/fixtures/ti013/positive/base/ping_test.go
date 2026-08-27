package net

import "testing"

func TestPing(t *testing.T) {
	if !Ping() {
		t.Errorf("ping failed")
	}
}
