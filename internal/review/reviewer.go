package review

// Reviewer thực hiện việc review code trong thư mục dir dựa trên prompt đã
// dựng sẵn (xem BuildReviewPrompt), trả về nội dung review dạng text.
//
// attempts báo số lần thực hiện thực sự tốn (vd gọi Claude CLI) để có được
// kết quả (hoặc lỗi cuối cùng) — dùng để ghi log tần suất phải retry (xem
// ReviewLogger, issue #28). Implementation không có khái niệm retry (hoặc
// fake dùng cho test) chỉ cần luôn trả 1.
//
// Tách interface này ra để main.go không phụ thuộc trực tiếp vào cách
// review được thực hiện (hiện tại là gọi Claude CLI qua claudecli.Reviewer) —
// sau này muốn đổi sang cách khác (vd Claude API) chỉ cần viết implementation
// mới, không phải sửa chỗ gọi.
type Reviewer interface {
	Review(prompt string, dir string) (result string, attempts int, err error)
}
