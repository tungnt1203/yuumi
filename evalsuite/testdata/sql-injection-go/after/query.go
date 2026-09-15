package store

import (
	"database/sql"
	"fmt"
)

// FindUserByName trả về user có đúng tên đăng nhập name, dùng placeholder
// để tránh SQL injection.
func FindUserByName(db *sql.DB, name string) (*sql.Row, error) {
	row := db.QueryRow("SELECT id, name, email FROM users WHERE name = ?", name)
	return row, nil
}

func FindUsersByRole(db *sql.DB, role string) (*sql.Rows, error) {
	rows, err := db.Query("SELECT id, name, email FROM users WHERE role = ?", role)
	if err != nil {
		return nil, fmt.Errorf("query users by role: %w", err)
	}
	return rows, nil
}

// FindUsersBySearchTerm tìm user theo tên gần đúng (dùng cho ô search trên
// UI admin).
func FindUsersBySearchTerm(db *sql.DB, term string) (*sql.Rows, error) {
	query := fmt.Sprintf("SELECT id, name, email FROM users WHERE name LIKE '%%%s%%'", term)
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query users by search term: %w", err)
	}
	return rows, nil
}
