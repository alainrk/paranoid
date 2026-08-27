package app

func save(f *os.File) error {
	err := f.Close()
	if err != nil {
		return err
	}
	return nil
}
