package review

import (
	"errors"
	"io/fs"
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
//
// BlockSeverity (issue #60): finding ở các mức này làm check run "yuumi
// review" thành failure, để branch protection chặn merge. Rỗng (mặc định)
// là không chặn gì. Chỉ có hiệu lực khi đọc từ commit BASE của PR (xem
// Job.loadBlockSeverity) — giá trị trong .yuumi.yml ở head bị bỏ qua, vì tác
// giả PR sửa được file đó.
type repoConfig struct {
	Exclude       []string `yaml:"exclude"`
	Instructions  string   `yaml:"instructions"`
	BlockSeverity []string `yaml:"block_severity"`
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
	data, err := readCloneFile(dir, repoConfigFileName)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return repoConfig{}, nil
		}
		return repoConfig{}, err
	}

	return parseRepoConfig(data)
}

// parseRepoConfig parse nội dung .yuumi.yml — dùng chung cho file ở head
// (loadRepoConfig, đọc từ thư mục clone) và ở base (Job.loadBlockSeverity,
// đọc qua GitHub API).
func parseRepoConfig(data []byte) (repoConfig, error) {
	var cfg repoConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return repoConfig{}, err
	}

	cfg.Exclude = removeBlank(cfg.Exclude)
	cfg.Instructions = strings.TrimSpace(cfg.Instructions)

	return cfg, nil
}

// validSeverities là các mức severity finding có thể mang (xem Finding).
var validSeverities = map[string]bool{"critical": true, "high": true, "medium": true, "low": true}

// normalizeSeverities chuẩn hoá danh sách severity (bỏ khoảng trắng, chữ
// thường, bỏ trùng). Giá trị không hợp lệ (gõ nhầm, vd "hight") trả riêng
// trong invalid để nơi gọi log cảnh báo — không làm hỏng cả cấu hình.
func normalizeSeverities(items []string) (valid []string, invalid []string) {
	seen := map[string]bool{}
	for _, item := range items {
		sev := strings.ToLower(strings.TrimSpace(item))
		switch {
		case sev == "" || seen[sev]:
		case validSeverities[sev]:
			seen[sev] = true
			valid = append(valid, sev)
		default:
			invalid = append(invalid, item)
		}
	}
	return valid, invalid
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
