package claudecli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

type ClaudeResult struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

// defaultMaxAttempts là số lần thử tối đa mặc định khi Reviewer.MaxAttempts
// không được set (<=0): 1 lần gọi đầu + tối đa 2 lần retry (xem issue #11).
const defaultMaxAttempts = 3

// defaultBackoff là hàm backoff mặc định khi Reviewer.Backoff == nil.
// attempt đếm từ 2 (lần retry đầu tiên, sau lần gọi thứ 1 thất bại) —
// backoff tuyến tính ngắn (2s, 4s...), đủ để chờ qua sự cố mạng/tải tạm
// thời mà không làm review chậm đáng kể so với thời gian Claude CLI vốn đã
// cần (có thể tới vài chục giây/phút).
func defaultBackoff(attempt int) time.Duration {
	return time.Duration(attempt-1) * 2 * time.Second
}

// Reviewer gọi Claude CLI để review code. Nó implement review.Reviewer.
type Reviewer struct {
	// MaxAttempts giới hạn số lần thử tối đa (lần gọi đầu + retry) khi gặp
	// lỗi có vẻ tạm thời. <=0 nghĩa "chưa cấu hình", dùng defaultMaxAttempts.
	MaxAttempts int

	// Backoff quyết định thời gian chờ trước lần thử thứ `attempt` (attempt
	// đếm từ 2). nil dùng defaultBackoff.
	Backoff func(attempt int) time.Duration

	// sleep tách riêng khỏi Backoff để test override (khỏi phải chờ backoff
	// thật) — không export vì bên ngoài package không cần chỉnh; mặc định
	// (nil) dùng time.Sleep.
	sleep func(time.Duration)
}

func NewReviewer() *Reviewer {
	return &Reviewer{}
}

// Review gọi Claude CLI, tự retry tối đa MaxAttempts lần nếu gặp lỗi có vẻ
// tạm thời (chạy lệnh thất bại: timeout, lỗi mạng khi gọi Claude CLI...).
// Lỗi Claude tự báo rõ ràng (is_error=true kèm subtype cụ thể) hoặc output
// không parse được KHÔNG được retry — đây là lỗi xác định trước, thử lại
// với cùng input không giúp gì, chỉ tốn thêm thời gian (xem issue #11).
func (r *Reviewer) Review(prompt string, dir string) (string, error) {
	maxAttempts := r.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
	}
	backoff := r.Backoff
	if backoff == nil {
		backoff = defaultBackoff
	}
	sleep := r.sleep
	if sleep == nil {
		sleep = time.Sleep
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err, retryable := runOnce(prompt, dir)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !retryable || attempt == maxAttempts {
			break
		}
		wait := backoff(attempt + 1)
		fmt.Println("claude review attempt", attempt, "failed, retrying in", wait, ":", err)
		sleep(wait)
	}
	return "", lastErr
}

// runOnce gọi Claude CLI đúng 1 lần. retryable báo lỗi này có đáng thử lại
// không: true cho lỗi chạy lệnh (timeout, lệnh không chạy được...) — những
// lỗi này thường do mạng/tải tạm thời, chạy lại có cơ hội thành công; false
// cho lỗi xác định trước (output không parse được đúng định dạng kỳ vọng,
// hoặc Claude tự báo is_error=true) — retry không thay đổi được kết quả.
func runOnce(prompt string, dir string) (result string, err error, retryable bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "-p", prompt, "--output-format", "json")
	cmd.Dir = dir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	output, cmdErr := cmd.Output()
	if cmdErr != nil {
		return "", fmt.Errorf("claude command failed: %w (stderr: %s)", cmdErr, stderr.String()), true
	}

	var claudeResult ClaudeResult
	if jsonErr := json.Unmarshal(output, &claudeResult); jsonErr != nil {
		return "", fmt.Errorf("cannot parse claude output: %w", jsonErr), false
	}

	if claudeResult.IsError {
		return "", fmt.Errorf("claude returned error (%s): %s", claudeResult.Subtype, claudeResult.Result), false
	}

	return claudeResult.Result, nil, false
}
