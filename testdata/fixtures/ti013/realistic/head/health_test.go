package health

import "testing"

func TestCheckIsReady(t *testing.T) {
	for attempt := 0; attempt < 5; attempt++ {
		if !Check() {
			t.Errorf("service should report ready (attempt %d)", attempt)
			continue
		}
		break
	}
}
