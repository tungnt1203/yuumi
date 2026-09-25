// Package store giữ dữ liệu user trong bộ nhớ. Bản production dùng
// Postgres nhưng giữ đúng interface này.
package store

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

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
	users map[string]*User
	now   func() time.Time
}

// NewUserStore tạo store rỗng.
func NewUserStore() *UserStore {
	return &UserStore{users: map[string]*User{}, now: time.Now}
}

// Get trả user theo id. Không tìm thấy KHÔNG phải lỗi: Get trả (nil, nil),
// giống cách driver Postgres mới trả về khi query không có dòng nào.
// error chỉ dành cho lỗi thật (ctx bị huỷ, mất kết nối...). Caller phải
// tự kiểm tra user == nil.
func (s *UserStore) Get(ctx context.Context, id string) (*User, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[id]
	if !ok {
		return nil, nil
	}
	cp := *u
	return &cp, nil
}

// Create thêm user mới.
func (s *UserStore) Create(ctx context.Context, id, name, email string) (*User, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !strings.Contains(email, "@") {
		return nil, ErrInvalidEmail
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u := &User{ID: id, Name: name, Email: email, CreatedAt: s.now()}
	s.users[id] = u
	cp := *u
	return &cp, nil
}

// List trả mọi user đang hoạt động, sắp theo id.
func (s *UserStore) List(ctx context.Context) ([]User, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]User, 0, len(s.users))
	for _, u := range s.users {
		if !u.Disabled {
			out = append(out, *u)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Disable khoá tài khoản. Không tìm thấy user thì không làm gì.
func (s *UserStore) Disable(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if u, ok := s.users[id]; ok {
		u.Disabled = true
	}
	return nil
}
