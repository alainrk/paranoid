package worker

import (
	"testing"
	"time"
)

func TestJobCompletes(t *testing.T) {
	job := Enqueue()
	time.Sleep(2 * time.Second)
	if !job.Done {
		t.Fatal("job should be done")
	}
}
