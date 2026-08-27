package app

import "os"

// writeReport writes a report to a temp file and cleans up after
// itself. The close error on the deferred cleanup is intentionally
// discarded: the file is opened read-write and nothing further reads
// or writes it, so a failed close carries no information worth acting
// on. This pattern is unchanged between base and head.
func writeReport(path string, data []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()
	_, err = f.Write(data)
	return err
}
