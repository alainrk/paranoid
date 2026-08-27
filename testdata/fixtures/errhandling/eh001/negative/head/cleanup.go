package app

// runJob does the work and always tries to remove the scratch volume
// afterwards. The cleanup's result is intentionally discarded: by the
// time it runs, the job's own outcome has already been decided, and a
// failed best-effort cleanup is not worth failing the job over.
func runJob(volume string) {
	create(volume)
	defer func() {
		_, _, _ = removeVolume(volume)
	}()
}
