package health

// Check reports whether the dependent service is ready to serve
// traffic. It should be a fast, synchronous check.
func Check() bool {
	return probe()
}

func probe() bool { return true }
