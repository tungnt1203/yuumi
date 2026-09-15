package review

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// buildPrimer tổng hợp 1 lần ngữ cảnh dùng chung cho MỌI bundle của cùng 1
// PR (issue #18): danh sách toàn bộ file bị đổi trong PR (không chỉ file
// của riêng bundle đang review) và đường dẫn README/convention doc gần nhất
// tìm được trong repo. Job.Run gọi hàm này 1 lần trước khi chia bundle rồi
// nhúng cố định kết quả vào đầu prompt của mọi bundle (xem BuildReviewPrompt)
// — thay cho việc mỗi bundle tự quyết định lại có nên "đọc thêm file khác"
// hay không, vốn dễ dẫn tới review không nhất quán giữa các phần của cùng 1
// PR và tốn công khám phá lặp lại.
//
// dir là thư mục repo đã checkout (dùng để dò README thật trên đĩa, không
// đoán mù). changedFiles rỗng (diff rỗng, hoặc mọi file đều bị lọc) trả về
// "" — không có gì đáng chia sẻ giữa các bundle.
func buildPrimer(dir string, changedFiles []string) string {
	if len(changedFiles) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Ngữ cảnh dùng chung cho TẤT CẢ các phần của PR này (đã tổng hợp sẵn 1 lần, không cần tự khám phá lại):\n\n")

	fmt.Fprintf(&b, "Toàn bộ %d file bị thay đổi trong PR (không chỉ phần diff bạn thấy ở phần này, các file khác đang được review ở phần khác):\n", len(changedFiles))
	for _, f := range changedFiles {
		b.WriteString("- ")
		b.WriteString(f)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	if readmes := findReadmes(dir, changedFiles); len(readmes) > 0 {
		b.WriteString("README/convention doc gần nhất tìm thấy trong repo, hãy đọc trước khi kết luận để hiểu đúng kiến trúc và convention của project:\n")
		for _, r := range readmes {
			b.WriteString("- ")
			b.WriteString(r)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	return b.String()
}

// readmeFilenames là các tên file convention doc được tìm trong mỗi thư
// mục, theo thứ tự ưu tiên — chỉ lấy tên đầu tiên khớp cho mỗi thư mục
// (1 thư mục hiếm khi có cả README.md lẫn README, không cần liệt kê trùng).
var readmeFilenames = []string{"README.md", "README", "readme.md"}

// findReadmes dò README ở root repo và ở thư mục cha trực tiếp của mỗi file
// đã đổi (path.Dir) — đủ cho phần lớn convention thực tế (README theo từng
// package/module), không cần đi bộ toàn bộ cây thư mục cha để đơn giản.
//
// dir rỗng hoặc không đọc được trên đĩa (repo chưa clone xong, path lạ...)
// chỉ khiến kết quả thiếu, không phải lỗi chặn review — nhất quán với cách
// loadRepoConfig/loadGitignorePatterns xử lý lỗi đọc đĩa.
func findReadmes(dir string, changedFiles []string) []string {
	if dir == "" {
		return nil
	}

	dirsToCheck := []string{"."}
	seenDir := map[string]bool{".": true}
	for _, f := range changedFiles {
		d := path.Dir(f)
		if !seenDir[d] {
			seenDir[d] = true
			dirsToCheck = append(dirsToCheck, d)
		}
	}

	var found []string
	for _, d := range dirsToCheck {
		for _, name := range readmeFilenames {
			rel := name
			if d != "." {
				rel = path.Join(d, name)
			}
			info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
			if err == nil && !info.IsDir() {
				found = append(found, rel)
				break // chỉ lấy 1 tên khớp đầu tiên cho mỗi thư mục
			}
		}
	}
	return found
}
