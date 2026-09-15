package review

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// repoConfigFileName là tên file cấu hình review riêng cho từng repo, đặt
// ở root repo được review, đọc được sau khi clone (xem issue #7).
const repoConfigFileName = ".yuumi.yml"

// repoConfig là cấu hình tối thiểu mà issue #7 yêu cầu:
//   - Exclude: thêm pattern loại trừ file/thư mục, GỘP với
//     defaultIgnoredPathPatterns (xem isIgnoredPath) — không thay thế, vì
//     default vẫn luôn đúng cho mọi repo.
//   - Instructions: đoạn hướng dẫn bổ sung chèn vào prompt, để review theo
//     đúng convention/mức độ nghiêm khắc riêng của repo đó.
//
// Không có repo nào cấu hình 2 việc này thì dùng default hiện tại (không
// loại trừ thêm gì, không có hướng dẫn riêng) — repoConfig zero-value làm
// đúng việc đó.
type repoConfig struct {
	Exclude      []string `yaml:"exclude"`
	Instructions string   `yaml:"instructions"`
}

// loadRepoConfig đọc .yuumi.yml ở root dir (repo đã checkout).
//
// Không có file → trả zero-value, KHÔNG lỗi — đây là trường hợp bình
// thường (đa số repo sẽ không có file này), không phải sự cố. File có
// nhưng đọc/parse lỗi → trả zero-value KÈM lỗi để nơi gọi tự log — không
// chặn review vì 1 file cấu hình sai không đáng để hỏng cả lần review,
// nhất quán với cách GetPullRequestDiff/GetPullRequestChangedFilesCount lỗi
// cũng không chặn Run (xem job.go).
func loadRepoConfig(dir string) (repoConfig, error) {
	data, err := os.ReadFile(filepath.Join(dir, repoConfigFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return repoConfig{}, nil
		}
		return repoConfig{}, err
	}

	var cfg repoConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return repoConfig{}, err
	}

	cfg.Exclude = removeBlank(cfg.Exclude)
	cfg.Instructions = strings.TrimSpace(cfg.Instructions)

	return cfg, nil
}

// removeBlank bỏ các phần tử rỗng/toàn khoảng trắng — người viết .yuumi.yml
// gõ nhầm dòng trống trong list YAML không nên tạo ra 1 "pattern rỗng" làm
// hỏng isIgnoredPath (pattern rỗng sẽ match mọi path, vì "".HasSuffix luôn
// đúng và strings.Contains(path, "") luôn đúng).
func removeBlank(items []string) []string {
	cleaned := make([]string, 0, len(items))
	for _, s := range items {
		if strings.TrimSpace(s) != "" {
			cleaned = append(cleaned, s)
		}
	}
	return cleaned
}
