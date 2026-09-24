package review

// CallStats là số liệu phụ của 1 lần gọi Reviewer.Review, dùng để ghi log
// (xem ReviewLogger) chứ không ảnh hưởng tới nội dung review. Gom vào 1
// struct thay vì thêm từng giá trị trả về rời, để thêm số liệu mới không
// phải sửa chữ ký của mọi implementation và chỗ gọi.
type CallStats struct {
	// Attempts là số lần thực hiện thực sự tốn (vd gọi Claude CLI) để có
	// được kết quả hoặc lỗi cuối cùng — 1 nghĩa là không phải retry. Dùng để
	// ghi log tần suất phải retry (issue #28).
	Attempts int

	// NumTurns là số turn model dùng ở lần gọi cuối (0 nếu không có output
	// để đọc) — tín hiệu model có thực sự khám phá thêm file ngoài diff hay
	// không (issue #20).
	NumTurns int

	// Usage là token/chi phí đã tiêu, cộng dồn qua mọi lần thử (issue #63).
	// Zero value nghĩa là implementation không báo được usage, không phải
	// lần gọi miễn phí.
	Usage Usage
}

// Usage là token và chi phí model báo về. Input chia 3 phần vì Claude CLI
// dùng prompt cache: InputTokens chỉ là phần KHÔNG đi qua cache, thường rất
// nhỏ — phần lớn input nằm ở CacheCreationInputTokens/CacheReadInputTokens,
// mỗi loại tính giá khác nhau. Chỉ log InputTokens sẽ báo thiếu gần hết.
type Usage struct {
	InputTokens              int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	OutputTokens             int
	CostUSD                  float64
}

// Add trả về tổng u và other, dùng để cộng dồn usage qua các lần retry.
func (u Usage) Add(other Usage) Usage {
	return Usage{
		InputTokens:              u.InputTokens + other.InputTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens + other.CacheCreationInputTokens,
		CacheReadInputTokens:     u.CacheReadInputTokens + other.CacheReadInputTokens,
		OutputTokens:             u.OutputTokens + other.OutputTokens,
		CostUSD:                  u.CostUSD + other.CostUSD,
	}
}

// Reviewer thực hiện việc review code trong thư mục dir dựa trên prompt đã
// dựng sẵn (xem BuildReviewPrompt), trả về nội dung review dạng text kèm
// CallStats. Implementation không có khái niệm retry/turn (hoặc fake dùng
// cho test) chỉ cần trả CallStats{Attempts: 1}.
//
// Tách interface này ra để main.go không phụ thuộc trực tiếp vào cách
// review được thực hiện (hiện tại là gọi Claude CLI qua claudecli.Reviewer) —
// sau này muốn đổi sang cách khác (vd Claude API) chỉ cần viết implementation
// mới, không phải sửa chỗ gọi.
type Reviewer interface {
	Review(prompt string, dir string) (result string, stats CallStats, err error)
}
