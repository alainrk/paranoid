package app

func save() error {
	err := doThing()
	if err != nil {
		return err
	}
	return nil
}
