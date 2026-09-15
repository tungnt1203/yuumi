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
