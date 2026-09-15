package webhook

import "sync"

// SeenComments nhớ những comment ID đã xử lý, để chặn xử lý trùng khi
// GitHub redeliver webhook hoặc người dùng mention 2 lần sát nhau trên
// cùng 1 comment — tránh 2 review.Job chạy song song cùng edit 1
// PlaceholderID (race, comment cuối "thắng" đè lên comment kia).
//
// Lưu trong memory, không có eviction — chấp nhận được ở quy mô hiện tại
// (1 instance, bot dùng nội bộ, comment ID là int64 nên tốn rất ít bộ nhớ
// mỗi entry). Nếu sau này chạy lâu dài với traffic lớn, cần TTL hoặc lưu ở
// nơi khác (Redis, DB) — đây là giới hạn đã biết, chưa cần giải quyết bây
// giờ.
type SeenComments struct {
	mu   sync.Mutex
	seen map[int64]struct{}
}

func NewSeenComments() *SeenComments {
	return &SeenComments{seen: make(map[int64]struct{})}
}

// MarkIfNew trả về true nếu đây là lần đầu thấy id (và ghi nhận lại luôn,
// atomic với lần kiểm tra), false nếu id đã được xử lý trước đó.
func (s *SeenComments) MarkIfNew(id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.seen[id]; ok {
		return false
	}
	s.seen[id] = struct{}{}
	return true
}
