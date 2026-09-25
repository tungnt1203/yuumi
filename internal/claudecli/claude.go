package claudecli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/tungnt1203/yuumi/internal/procenv"
	"github.com/tungnt1203/yuumi/internal/review"
	"github.com/tungnt1203/yuumi/internal/sandbox"
)

type ClaudeResult struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`

	// StructuredOutput là object model trả qua tool structured output khi
	// CLI chạy với --json-schema (issue #72) — CLI đã validate nó theo
	// schema. Rỗng hoặc "null" khi không có.
	StructuredOutput json.RawMessage `json:"structured_output"`

	// NumTurns là số turn Claude CLI thực sự dùng để ra kết quả này — proxy
	// rẻ để biết model có khám phá thêm gì ngoài diff hay không (1 turn bất
	// thường trên diff nhiều file là dấu hiệu model không tự đọc thêm gì,
	// xem reviewlog, issue #20).
	NumTurns int `json:"num_turns"`

	// TotalCostUSD và Usage là chi phí/token CLI báo cho lần gọi này (issue
	// #63). Output không có các field này (CLI cũ) thì giữ zero value.
	TotalCostUSD float64     `json:"total_cost_usd"`
	Usage        ClaudeUsage `json:"usage"`
}

// ClaudeUsage là phần token trong field "usage" của output
// `claude -p --output-format json`.
type ClaudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	OutputTokens             int `json:"output_tokens"`
}

// text là kết quả trả cho review.Job: structured output nếu có, không thì
// Result (văn xuôi). Job tự nhận ra văn xuôi và hiển thị nguyên văn, nên
// CLI thiếu structured output không làm mất nội dung review.
func (c ClaudeResult) text() string {
	if s := bytes.TrimSpace(c.StructuredOutput); len(s) > 0 && !bytes.Equal(s, []byte("null")) {
		return string(s)
	}
	return c.Result
}

func (c ClaudeResult) usage() review.Usage {
	return review.Usage{
		InputTokens:              c.Usage.InputTokens,
		CacheCreationInputTokens: c.Usage.CacheCreationInputTokens,
		CacheReadInputTokens:     c.Usage.CacheReadInputTokens,
		OutputTokens:             c.Usage.OutputTokens,
		CostUSD:                  c.TotalCostUSD,
	}
}

// defaultMaxAttempts là số lần thử tối đa mặc định khi Reviewer.MaxAttempts
// không được set (<=0): 1 lần gọi đầu + tối đa 2 lần retry (xem issue #11).
const defaultMaxAttempts = 3

// defaultTimeout giới hạn 1 lần gọi Claude CLI khi Reviewer.Timeout không
// được set. Bundle lớn (tới ~100k ký tự diff, xem
// review.defaultBundleBudgetChars) cần vài phút đọc thêm file; 5 phút cũ
// đã sát trần với bundle 12k (log có lần gọi 200s, issue #73).
const defaultTimeout = 15 * time.Minute

// defaultMaxBudgetUSD là trần chi phí 1 lần gọi khi Reviewer.MaxBudgetUSD
// không được set. Log tới 2026-09-25 (115 lần gọi): cao nhất $0.54, p95
// $0.39 — phần lớn với bundle 12k; bundle 100k có thể tới ~$1-1.5. $3 đủ
// rộng để không chặn review bình thường, chỉ chặn lần gọi chạy mất kiểm
// soát (vd diff chứa prompt injection bắt đọc cả repo) (issue #100).
const defaultMaxBudgetUSD = 3.0

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

	// Timeout giới hạn 1 lần gọi CLI. <=0 dùng defaultTimeout.
	Timeout time.Duration

	// MaxBudgetUSD là trần chi phí 1 lần gọi CLI. <=0 dùng
	// defaultMaxBudgetUSD.
	MaxBudgetUSD float64

	// sleep tách riêng khỏi Backoff để test override (khỏi phải chờ backoff
	// thật) — không export vì bên ngoài package không cần chỉnh; mặc định
	// (nil) dùng time.Sleep.
	sleep func(time.Duration)
}

func NewReviewer() *Reviewer {
	return &Reviewer{}
}

// Review gọi Claude CLI, tự retry tối đa MaxAttempts lần nếu gặp lỗi có vẻ
// tạm thời (chạy lệnh thất bại, lỗi mạng khi gọi Claude CLI...; hết
// timeout thì không, xem runOnce).
// Lỗi Claude tự báo rõ ràng (is_error=true kèm subtype cụ thể) hoặc output
// không parse được KHÔNG được retry — đây là lỗi xác định trước, thử lại
// với cùng input không giúp gì, chỉ tốn thêm thời gian (xem issue #11).
//
// stats.Attempts là số lần Claude CLI thực sự được gọi (1 nghĩa là thành
// công hoặc thất bại ngay lần đầu, không phải retry) — để nơi gọi ghi nhận
// lại tần suất phải retry trong thực tế (xem reviewlog, issue #28).
//
// stats.NumTurns là ClaudeResult.NumTurns của lần gọi cuối cùng (0 nếu lần
// đó không có output JSON để đọc, vd timeout/kill hoặc output không parse
// được) —
// dùng làm tín hiệu Claude có thực sự đọc thêm file ngoài diff hay không
// (xem reviewlog, issue #20).
//
// stats.Usage cộng dồn token/chi phí của mọi lần thử có output parse được,
// kể cả lần Claude tự báo is_error (vẫn tốn tiền thật) và lần lệnh thoát
// exit != 0 nhưng vẫn in JSON ra stdout. Lần thử bị timeout/kill giữa chừng
// không có output nên không đếm được — con số này có thể thấp hơn thực tế
// (issue #63).
func (r *Reviewer) Review(prompt string, box sandbox.Env) (result string, stats review.CallStats, err error) {
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
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	budget := r.MaxBudgetUSD
	if budget <= 0 {
		budget = defaultMaxBudgetUSD
	}

	var lastErr error
	var usage review.Usage
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		res, err, retryable := runOnce(prompt, box, timeout, budget)
		usage = usage.Add(res.usage())
		stats = review.CallStats{Attempts: attempt, NumTurns: res.NumTurns, Usage: usage}
		if err == nil {
			return res.text(), stats, nil
		}
		lastErr = err
		if !retryable || attempt == maxAttempts {
			return "", stats, lastErr
		}
		wait := backoff(attempt + 1)
		fmt.Println("claude review attempt", attempt, "failed, retrying in", wait, ":", err)
		sleep(wait)
	}
	return "", review.CallStats{Attempts: maxAttempts, Usage: usage}, lastErr
}

// allowedTools là toàn bộ tool Claude được dùng khi review: review chỉ cần
// đọc code. Allowlist (--tools) thay cho blocklist (--disallowedTools) cũ:
// với blocklist, CLI 2.1.281 vẫn mở Agent, Workflow, Skill, ToolSearch,
// ScheduleWakeup... — tool mới của bản CLI sau cũng tự lọt vào. Chạy lệnh,
// sửa file hay gọi mạng trên code PR không tin cậy là đường cho prompt
// injection trong diff biến thành hành động thật (issue #78, #100).
const allowedTools = "Read,Grep,Glob"

// claudeArgs dựng tham số cho Claude CLI. Thư mục làm việc là repo của PR,
// nên mọi cấu hình CLI tự đọc từ đó đều do tác giả PR viết:
//   - --tools: chỉ còn Read/Grep/Glob (cộng tool structured output nội bộ
//     của --json-schema) — đã kiểm chứng bằng cách hỏi CLI danh sách tool.
//   - --restricted: CLI tự giới hạn file tools trong thư mục làm việc, bỏ
//     tool chạy code và mọi settings file (user/project/local). Trước đây
//     việc Read/Grep/Glob không đọc được ra ngoài (đường dẫn tuyệt đối,
//     symlink, /proc/<pid>/environ...) chỉ dựa vào mặc định của -p.
//   - --setting-sources user: bỏ .claude/settings*.json của repo (hook, nới
//     quyền tool). --restricted đã bao hàm, giữ lại làm lớp thứ hai.
//   - --strict-mcp-config (không kèm --mcp-config): bỏ MCP server khai báo
//     trong .mcp.json của repo.
//   - --no-session-persistence: không ghi transcript (có code PR) ra đĩa.
//   - --max-budget-usd: trần chi phí 1 lần gọi. Vượt thì CLI trả is_error
//     (subtype error_max_budget_usd), runOnce không retry. Trần mềm: CLI
//     dừng SAU lượt vượt trần, không cắt giữa chừng.
//
// --json-schema (review.FindingsSchema) buộc model trả kết quả qua tool
// structured output, CLI validate theo schema và bắt model gọi lại nếu sai;
// hết lượt mà vẫn sai thì CLI báo is_error (issue #72). Đây là tool nội bộ
// của CLI, không đọc/ghi file hay gọi mạng.
//
// CLAUDE.md của repo vẫn được CLI đọc (chỉ --bare tắt được, mà --bare bắt
// buộc xác thực bằng ANTHROPIC_API_KEY). Với tool đã khoá chỉ còn đọc, nó
// chỉ ảnh hưởng được nội dung review.
func claudeArgs(prompt string, maxBudgetUSD float64) []string {
	return []string{
		"-p", prompt,
		"--output-format", "json",
		"--tools", allowedTools,
		"--restricted",
		"--setting-sources", "user",
		"--strict-mcp-config",
		"--no-session-persistence",
		"--max-budget-usd", strconv.FormatFloat(maxBudgetUSD, 'f', -1, 64),
		"--json-schema", review.FindingsSchema,
	}
}

// runOnce gọi Claude CLI đúng 1 lần. retryable báo lỗi này có đáng thử lại
// không: true cho lỗi chạy lệnh (lệnh không chạy được, CLI chết giữa
// chừng...) — thường do mạng/tải tạm thời, chạy lại có cơ hội thành công;
// false cho lỗi xác định trước (output không parse được đúng định dạng kỳ
// vọng, hoặc Claude tự báo is_error=true, kể cả khi lệnh thoát exit != 0) —
// retry không thay đổi được kết quả.
//
// Hết timeout cũng KHÔNG retry: lần gọi đã chạy hết thời gian cho phép
// thường là do bundle quá lớn, không phải trục trặc thoáng qua (CLI tự
// retry lỗi API bên trong). Retry chỉ nhân thời gian chờ lên tới
// MaxAttempts lần, và trong Docker sandbox tiến trình cũ còn chạy tiếp tới
// khi container bị xoá (xem sandbox.docker.Command), tức là tốn token gấp
// đôi.
//
// res là output đã parse, kể cả khi is_error=true hoặc lệnh thoát exit != 0
// mà stdout vẫn là JSON (để caller đọc num_turns, usage); zero value nếu
// không có output JSON để đọc.
func runOnce(prompt string, box sandbox.Env, timeout time.Duration, maxBudgetUSD float64) (res ClaudeResult, err error, retryable bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := box.Command(ctx, procenv.WithoutSecrets(os.Environ()), "claude", claudeArgs(prompt, maxBudgetUSD)...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	output, cmdErr := cmd.Output()
	if cmdErr != nil {
		// cmd.Output() vẫn trả stdout khi exit != 0 — CLI có thể đã in JSON
		// kèm usage trước khi thoát, giữ lại để không mất số liệu chi phí.
		var partial ClaudeResult
		if json.Unmarshal(output, &partial) == nil && partial.IsError {
			// CLI thoát exit != 0 vì chính Claude báo lỗi (vd error_max_turns)
			// — lỗi xác định trước như nhánh is_error bên dưới, không retry.
			return partial, fmt.Errorf("claude returned error (%s): %s: %w", partial.Subtype, partial.Result, cmdErr), false
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return partial, fmt.Errorf("claude timed out after %s: %w", timeout, cmdErr), false
		}
		return partial, fmt.Errorf("claude command failed: %w (stderr: %s)", cmdErr, stderr.String()), true
	}

	var claudeResult ClaudeResult
	if jsonErr := json.Unmarshal(output, &claudeResult); jsonErr != nil {
		return ClaudeResult{}, fmt.Errorf("cannot parse claude output: %w", jsonErr), false
	}

	if claudeResult.IsError {
		return claudeResult, fmt.Errorf("claude returned error (%s): %s", claudeResult.Subtype, claudeResult.Result), false
	}

	return claudeResult, nil, false
}
