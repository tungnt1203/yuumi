package review

import (
	"os"
	"path/filepath"
	"strings"
)

// gitignoreFileName là tên file .gitignore thật của repo được review, đọc
// được ở root sau khi clone (xem issue #27).
const gitignoreFileName = ".gitignore"

// loadGitignorePatterns đọc .gitignore ở root dir (repo đã checkout), trả
// về pattern đã convert sang đúng format mà matchesAnyPattern hiểu được
// (xem parseGitignorePatterns, diffsplit.go).
//
// Không có file → trả nil, KHÔNG lỗi — không phải repo nào cũng có
// .gitignore, và thiếu file này không nên chặn review. File có nhưng đọc
// lỗi (quyền, ...) → trả lỗi kèm nil để nơi gọi tự log, không chặn review —
// nhất quán với loadRepoConfig (xem repoconfig.go, issue #7).
func loadGitignorePatterns(dir string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(dir, gitignoreFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return parseGitignorePatterns(string(data)), nil
}

// parseGitignorePatterns đọc nội dung 1 file .gitignore, trả pattern ở
// đúng format matchesAnyPattern/isIgnoredPath đã hỗ trợ: pattern có "/"
// match kiểu "path chứa chuỗi này", pattern không có "/" match theo suffix
// của path (xem diffsplit.go).
//
// Chỉ cần hỗ trợ các case phổ biến nhất theo yêu cầu issue #27 (không cần
// đúng 100% spec .gitignore, ưu tiên basename/prefix/thư mục):
//   - dòng trống và comment ("#...") → bỏ qua.
//   - pattern phủ định ("!...") → bỏ qua: isIgnoredPath không có khái niệm
//     "un-ignore lại 1 pattern đã bị loại trước đó", hỗ trợ đúng nghĩa sẽ
//     phức tạp không tương xứng lợi ích cho 1 file cấu hình phụ.
//   - pattern có "/" (đầu, giữa, hoặc cuối, vd "build/", "/dist", "src/gen/")
//     → giữ nguyên (bỏ "/" dẫn đầu nếu có), match kiểu "contains" giống hệt
//     defaultIgnoredPathPatterns — KHÔNG phân biệt anchored (chỉ tính từ
//     root) hay không, chấp nhận match rộng hơn 1 chút để giữ đơn giản.
//   - pattern không có "/", không wildcard (vd "coverage.log") → match theo
//     suffix, giống defaultIgnoredPathPatterns.
//   - pattern dạng "*<phần còn lại>" với đúng 1 dấu * ở đầu (vd "*.log") →
//     bỏ dấu * đầu, dùng phần còn lại match suffix — case wildcard phổ biến
//     nhất trong .gitignore thật.
//   - wildcard khác (nhiều dấu *, ?, [...]) → bỏ qua, không cố dịch: lọc
//     nhầm khiến bot bỏ sót file thật nguy hiểm hơn nhiều so với việc không
//     tận dụng được vài dòng .gitignore ít gặp.
func parseGitignorePatterns(data string) []string {
	var patterns []string

	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}

		if strings.Contains(line, "/") {
			patterns = append(patterns, strings.TrimPrefix(line, "/"))
			continue
		}

		if suffix, ok := simpleWildcardSuffix(line); ok {
			patterns = append(patterns, suffix)
			continue
		}

		if strings.ContainsAny(line, "*?[") {
			continue
		}

		patterns = append(patterns, line)
	}

	return patterns
}

// simpleWildcardSuffix nhận diện đúng case "*<phần còn lại không có ký tự
// đặc biệt>" (vd "*.log", "*.tmp") — wildcard phổ biến nhất trong
// .gitignore thật, dịch an toàn được sang suffix match.
func simpleWildcardSuffix(pattern string) (suffix string, ok bool) {
	if !strings.HasPrefix(pattern, "*") {
		return "", false
	}
	rest := pattern[1:]
	if rest == "" || strings.ContainsAny(rest, "*?[") {
		return "", false
	}
	return rest, true
}
