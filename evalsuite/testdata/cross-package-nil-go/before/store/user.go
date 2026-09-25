// Package store giữ dữ liệu user trong bộ nhớ. Bản production dùng
// Postgres nhưng giữ đúng interface này.
package store

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrNotFound trả về khi không có user với id được hỏi.
var ErrNotFound = errors.New("store: user not found")

// ErrInvalidEmail trả về khi email không hợp lệ.
var ErrInvalidEmail = errors.New("store: invalid email")

// User là 1 tài khoản khách hàng.
type User struct {
	ID        string
	Name      string
	Email     string
	CreatedAt time.Time
	Disabled  bool
}

// UserStore lưu user theo id, an toàn khi dùng từ nhiều goroutine.
type UserStore struct {
	mu    sync.RWMutex
	users map[string]User
	now   func() time.Time
}

// NewUserStore tạo store rỗng.
func NewUserStore() *UserStore {
	return &UserStore{users: map[string]User{}, now: time.Now}
}

// Get trả user theo id, hoặc ErrNotFound.
func (s *UserStore) Get(id string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

// Create thêm user mới.
func (s *UserStore) Create(id, name, email string) (User, error) {
	if !strings.Contains(email, "@") {
		return User{}, ErrInvalidEmail
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u := User{ID: id, Name: name, Email: email, CreatedAt: s.now()}
	s.users[id] = u
	return u, nil
}

// List trả mọi user đang hoạt động, sắp theo id.
func (s *UserStore) List() []User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]User, 0, len(s.users))
	for _, u := range s.users {
		if !u.Disabled {
			out = append(out, u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
