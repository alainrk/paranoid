package repository

import "database/sql"

// UserRepository stores users in a SQL database.
type UserRepository struct {
	db *sql.DB
}

// UpdateEmail updates a user's email address inside a transaction.
func (r *UserRepository) UpdateEmail(id int, email string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE users SET email = ? WHERE id = ?", email, id)
	if err != nil {
		return err
	}
	_ = tx.Commit()
	return nil
}

// Count returns the number of users, or zero when there are none.
func (r *UserRepository) Count() (int, error) {
	var n int
	err := r.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return n, err
}
