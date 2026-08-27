package app

func save() error {
	err := doThing()
	if err != nil {
		log.Println("save failed:", err)
		return err
	}
	return nil
}
