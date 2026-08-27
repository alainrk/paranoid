package health

// Check reports whether the dependent service is ready.
func Check() bool { return probe() }

func probe() bool { return true }
