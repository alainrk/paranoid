package app

func save(f *os.File) error {
	_ = f.Close()
	return nil
}
