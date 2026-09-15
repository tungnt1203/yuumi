package review

// Reviewer thực hiện việc review code trong thư mục dir dựa trên prompt đã
// dựng sẵn (xem BuildReviewPrompt), trả về nội dung review dạng text.
//
// Tách interface này ra để main.go không phụ thuộc trực tiếp vào cách
// review được thực hiện (hiện tại là gọi Claude CLI qua claudecli.Reviewer) —
// sau này muốn đổi sang cách khác (vd Claude API) chỉ cần viết implementation
// mới, không phải sửa chỗ gọi.
type Reviewer interface {
	Review(prompt string, dir string) (string, error)
}
