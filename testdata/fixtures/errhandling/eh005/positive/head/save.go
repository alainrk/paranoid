package app

func save() error {
	err := doThing()
	if err != nil {
		log.Println(err)
	}
	return nil
}
