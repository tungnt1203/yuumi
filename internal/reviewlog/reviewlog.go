// Package reviewlog ghi lại chi tiết mỗi lần Job gọi Reviewer.Review —
// prompt gửi đi, response nhận được, lỗi (nếu có) và thời gian xử lý — ra
// file JSON, để trace lại được khi review lỗi hoặc kết quả không như mong
// đợi ở production (issue #9). Trước đây log chỉ in ra console (fmt.Println)
// và mất khi process restart.
//
// Bắt đầu đơn giản bằng file JSON theo từng lần gọi; chuyển sang lưu trữ
// lâu dài hơn (DB, S3...) sau nếu thực tế thấy cần — YAGNI, chưa có nhu cầu
// thật thì chưa xây.
package reviewlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tungnt1203/yuumi/internal/review"
)

// defaultDir dùng khi FileLogger.Dir rỗng.
const defaultDir = "logs/reviews"

// entry là 1 bản ghi log, không xuất ra ngoài package — review.ReviewLogger
// chỉ cần gọi LogReview với tham số rời, không cần biết cấu trúc lưu trữ
// bên trong (xem review.Job.Logger).
type entry struct {
	Time         time.Time `json:"time"`
	RepoFullName string    `json:"repo_full_name"`
	IssueNumber  int       `json:"issue_number"`
	SHA          string    `json:"sha"`
	BundleIndex  int       `json:"bundle_index"`
	BundleTotal  int       `json:"bundle_total"`
	Prompt       string    `json:"prompt"`
	Response     string    `json:"response"`
	Error        string    `json:"error,omitempty"`
	DurationMs   int64     `json:"duration_ms"`

	// Attempts là số lần Reviewer.Review thực sự tốn (gọi Claude CLI) để ra
	// được kết quả/lỗi ở entry này — 1 nghĩa là không phải retry, >1 nghĩa
	// là gặp lỗi tạm thời và phải thử lại (xem claudecli.Reviewer, issue
	// #28). Dùng để dò tần suất lỗi tạm thời trong thực tế qua log.
	Attempts int `json:"attempts"`

	// NumTurns là số turn Claude CLI dùng ở lần gọi cuối để ra được kết
	// quả/lỗi này (0 nếu không có output JSON để đọc, vd timeout/kill).
	// Proxy rẻ để biết model có thực sự đọc thêm file ngoài diff hay
	// chỉ review mù trên diff — num_turns thấp bất thường trên 1 bundle
	// nhiều file là dấu hiệu đáng ngờ (xem claudecli.Reviewer, issue #20).
	NumTurns int `json:"num_turns"`

	// Usage là token/chi phí của lần gọi này, cộng dồn qua retry (issue
	// #63). Toàn 0 khi Reviewer không báo được usage — review vẫn chạy
	// bình thường, chỉ thiếu số liệu.
	Usage usage `json:"usage"`
}

// usage là bản ghi của review.Usage. Tách riêng để json tag nằm ở package
// lưu trữ, không gắn vào kiểu domain của review.
type usage struct {
	InputTokens              int     `json:"input_tokens"`
	CacheCreationInputTokens int     `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int     `json:"cache_read_input_tokens"`
	OutputTokens             int     `json:"output_tokens"`
	CostUSD                  float64 `json:"cost_usd"`
}

// FileLogger implement review.ReviewLogger bằng cách ghi mỗi lần gọi thành
// 1 file JSON riêng trong Dir (Dir rỗng thì dùng defaultDir).
//
// Lỗi ghi log (không tạo được thư mục, không ghi được file...) không bao
// giờ trả ra ngoài — chỉ in ra console. Review là luồng chính, ghi log là
// phụ trợ: hỏng ghi log không được phép làm hỏng/chặn review.
//
// Không log secret: prompt/response được ghi nguyên văn, nhưng bản thân
// chúng chỉ chứa diff code + hướng dẫn review, không chứa GitHub App private
// key, installation token hay webhook secret (những giá trị đó không bao
// giờ đi vào prompt).
type FileLogger struct {
	Dir string
}

func NewFileLogger(dir string) *FileLogger {
	return &FileLogger{Dir: dir}
}

func (l *FileLogger) LogReview(repoFullName string, issueNumber int, sha string, bundleIndex, bundleTotal int, prompt, response, errMsg string, duration time.Duration, stats review.CallStats) {
	dir := l.Dir
	if dir == "" {
		dir = defaultDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Println("reviewlog: cannot create log dir:", err)
		return
	}

	e := entry{
		Time:         time.Now(),
		RepoFullName: repoFullName,
		IssueNumber:  issueNumber,
		SHA:          sha,
		BundleIndex:  bundleIndex,
		BundleTotal:  bundleTotal,
		Prompt:       prompt,
		Response:     response,
		Error:        errMsg,
		DurationMs:   duration.Milliseconds(),
		Attempts:     stats.Attempts,
		NumTurns:     stats.NumTurns,
		Usage: usage{
			InputTokens:              stats.Usage.InputTokens,
			CacheCreationInputTokens: stats.Usage.CacheCreationInputTokens,
			CacheReadInputTokens:     stats.Usage.CacheReadInputTokens,
			OutputTokens:             stats.Usage.OutputTokens,
			CostUSD:                  stats.Usage.CostUSD,
		},
	}

	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		fmt.Println("reviewlog: cannot marshal entry:", err)
		return
	}

	name := fmt.Sprintf("%d_%s_%d_%d-%d.json", e.Time.UnixNano(), sanitize(repoFullName), issueNumber, bundleIndex, bundleTotal)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Println("reviewlog: cannot write log file:", err)
	}
}

// sanitize thay "/" trong repo full name (vd "owner/repo") bằng "_" để dùng
// an toàn trong tên file trên mọi OS.
func sanitize(s string) string {
	return strings.ReplaceAll(s, "/", "_")
}
