package app

func settle(tx *Transaction) error {
	err := tx.Commit()
	if err != nil {
		return err
	}
	return nil
}
