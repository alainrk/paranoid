package app

func settle(tx *Transaction) error {
	_ = tx.Commit()
	return nil
}
