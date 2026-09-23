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

	// Skipped là số file .json không đọc/parse được. Bỏ qua thay vì dừng
	// hẳn, để 1 file hỏng không che mất số liệu của các file còn lại.
	Skipped int `json:"skipped"`
}

// Summarize đọc mọi file log .json trong dir (rỗng thì dùng defaultDir,
// giống FileLogger) và cộng usage theo repo, theo ngày. Entry ghi trước khi
// có field usage vẫn được đếm vào Calls với token/chi phí bằng 0.
func Summarize(dir string, loc *time.Location) (Summary, error) {
	if dir == "" {
		dir = defaultDir
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
		var e entry
		if err := json.Unmarshal(data, &e); err != nil {
			s.Skipped++
			continue
		}

		s.Total.add(e.Usage)

		repo := s.ByRepo[e.RepoFullName]
		repo.add(e.Usage)
		s.ByRepo[e.RepoFullName] = repo

		dayKey := e.Time.In(loc).Format(time.DateOnly)
		day := s.ByDay[dayKey]
		day.add(e.Usage)
		s.ByDay[dayKey] = day
	}
	return s, nil
}
