//go:build integration

package net

import "testing"

func TestPing(t *testing.T) {
	if !Ping() {
		t.Error("Ping() should be true")
	}
}
