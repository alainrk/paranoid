package health

import "testing"

func TestCheckIsReady(t *testing.T) {
	if !Check() {
		t.Fatal("service should report ready")
	}
}
