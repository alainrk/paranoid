package worker

// Job describes a background unit of work.
type Job struct {
	Done bool
}

// Enqueue schedules a job and returns immediately.
func Enqueue() *Job { return &Job{} }
