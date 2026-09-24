package reviewlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Totals là tổng số lần gọi và token/chi phí của một nhóm entry.
type Totals struct {
	Calls                    int     `json:"calls"`
	InputTokens              int     `json:"input_tokens"`
	CacheCreationInputTokens int     `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int     `json:"cache_read_input_tokens"`
	OutputTokens             int     `json:"output_tokens"`
	CostUSD                  float64 `json:"cost_usd"`
}

func (t *Totals) add(u usage) {
	t.Calls++
	t.InputTokens += u.InputTokens
	t.CacheCreationInputTokens += u.CacheCreationInputTokens
	t.CacheReadInputTokens += u.CacheReadInputTokens
	t.OutputTokens += u.OutputTokens
	t.CostUSD += u.CostUSD
}

// Summary gom usage trong thư mục log theo repo và theo ngày (issue #63).
// Key của ByDay là "YYYY-MM-DD" theo múi giờ truyền vào Summarize.
type Summary struct {
	Total  Totals            `json:"total"`
	ByRepo map[string]Totals `json:"by_repo"`
	ByDay  map[string]Totals `json:"by_day"`

	// Skipped là số file .json không đọc/parse được hoặc không phải review
	// log (thiếu time). Bỏ qua thay vì dừng hẳn, để 1 file hỏng không che
	// mất số liệu của các file còn lại.
	Skipped int `json:"skipped"`

	// NoUsage là số entry ghi trước khi có field usage: vẫn đếm vào Calls
	// nhưng token/chi phí bằng 0, để phân biệt "không có số liệu" với "chi
	// phí thật bằng 0".
	NoUsage int `json:"no_usage"`
}

// Summarize đọc mọi file log .json trong dir (rỗng thì dùng defaultDir,
// giống FileLogger) và cộng usage theo repo, theo ngày. Entry ghi trước khi
// có field usage vẫn được đếm vào Calls với token/chi phí bằng 0 (xem
// Summary.NoUsage). loc nil thì dùng time.Local.
func Summarize(dir string, loc *time.Location) (Summary, error) {
	if dir == "" {
		dir = defaultDir
	}
	if loc == nil {
		loc = time.Local
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return Summary{}, fmt.Errorf("read log dir: %w", err)
	}

	s := Summary{ByRepo: map[string]Totals{}, ByDay: map[string]Totals{}}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			s.Skipped++
			continue
		}
		// Chỉ decode field cần cho thống kê, bỏ qua prompt/response (có thể
		// rất lớn). Usage là con trỏ để biết entry có field usage hay không.
		var e struct {
			Time         time.Time `json:"time"`
			RepoFullName string    `json:"repo_full_name"`
			Usage        *usage    `json:"usage"`
		}
		if err := json.Unmarshal(data, &e); err != nil || e.Time.IsZero() {
			s.Skipped++
			continue
		}
		var u usage
		if e.Usage == nil {
			s.NoUsage++
		} else {
			u = *e.Usage
		}

		s.Total.add(u)

		repo := s.ByRepo[e.RepoFullName]
		repo.add(u)
		s.ByRepo[e.RepoFullName] = repo

		dayKey := e.Time.In(loc).Format(time.DateOnly)
		day := s.ByDay[dayKey]
		day.add(u)
		s.ByDay[dayKey] = day
	}
	return s, nil
}
