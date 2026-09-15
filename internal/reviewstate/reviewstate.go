// Package reviewstate lưu lại SHA đã review lần gần nhất cho mỗi (repo,
// PR) cụ thể — dùng để chỉ gửi diff phần thay đổi MỚI khi 1 PR được review
// lại nhiều lần, thay vì gửi lại toàn bộ diff so với base mỗi lần (xem
// review.Job.loadDiff, issue #21).
//
// Tách riêng khỏi reviewlog: reviewlog là audit trail phục vụ debug (1 file
// JSON/lần gọi, không cần tra cứu nhanh); package này là STATE thật sự ảnh
// hưởng hành vi review chính, cần đọc/ghi nhanh và gọn cho đúng 1 (repo,
// PR) — dùng chung nguồn với reviewlog sẽ vừa chậm (phải scan hết file log)
// vừa trộn lẫn 2 mối quan tâm khác nhau.
package reviewstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// defaultPath dùng khi FileStore.Path rỗng.
const defaultPath = "logs/review-state.json"

// FileStore lưu state vào 1 file JSON duy nhất, dạng map "repoFullName#issueNumber"
// -> SHA đã review gần nhất.
//
// Khoá bằng mutex trong-process vì dispatcher có thể chạy nhiều Job đồng
// thời (xem review.NewDispatcher, issue #13) — dù hiếm khi 2 job cùng động
// vào đúng 1 PR cùng lúc, đọc-sửa-ghi cả file mà không khoá vẫn có thể mất
// dữ liệu của lần ghi khác xen giữa.
type FileStore struct {
	Path string

	mu sync.Mutex
}

func NewFileStore(path string) *FileStore {
	return &FileStore{Path: path}
}

func (s *FileStore) path() string {
	if s.Path == "" {
		return defaultPath
	}
	return s.Path
}

func key(repoFullName string, issueNumber int) string {
	return fmt.Sprintf("%s#%d", repoFullName, issueNumber)
}

// load đọc toàn bộ state hiện có. Không có file → map rỗng, KHÔNG lỗi (lần
// đầu tiên chạy, hoặc chưa PR nào được review lần 2 trở lên).
func (s *FileStore) load() (map[string]string, error) {
	data, err := os.ReadFile(s.path())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}

	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]string{}
	}
	return m, nil
}

// LastReviewedSHA tra cứu SHA đã review gần nhất cho 1 (repo, PR). found ==
// false nghĩa là PR này chưa từng được review trước đó (hoặc file state
// chưa tồn tại) — review.Job coi đây là lần đầu, lấy full diff như hành vi
// cũ (issue #21).
func (s *FileStore) LastReviewedSHA(repoFullName string, issueNumber int) (sha string, found bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.load()
	if err != nil {
		return "", false, err
	}
	sha, found = m[key(repoFullName, issueNumber)]
	return sha, found, nil
}

// SetLastReviewedSHA ghi lại SHA vừa review xong cho 1 (repo, PR), tạo thư
// mục chứa file state nếu chưa có.
//
// Đọc state cũ lỗi (file hỏng, JSON invalid...) không chặn việc ghi — ghi
// đè bằng 1 map mới chỉ chứa đúng entry này còn hơn mất luôn khả năng ghi
// state cho lần sau chỉ vì 1 lần đọc lỗi (review.Job vẫn hoạt động bình
// thường dù state bị mất, chỉ là không tối ưu được diff lần review kế).
func (s *FileStore) SetLastReviewedSHA(repoFullName string, issueNumber int, sha string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.load()
	if err != nil {
		m = map[string]string{}
	}
	m[key(repoFullName, issueNumber)] = sha

	if dir := filepath.Dir(s.path()); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("cannot create state dir: %w", err)
		}
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal state: %w", err)
	}
	return os.WriteFile(s.path(), data, 0o644)
}
