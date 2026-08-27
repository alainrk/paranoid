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
	err = tx.Commit()
	if err != nil {
		return err
	}
	return nil
}

// Count returns the number of users.
func (r *UserRepository) Count() (int, error) {
	var n int
	err := r.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&n)
	return n, err
}
