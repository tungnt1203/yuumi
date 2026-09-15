package review

import (
	"fmt"
	"strings"
)

// languageRule là 1 rule mặc định bot TỰ CÓ SẴN cho 1 nhóm file cùng loại
// (theo extension) — không phụ thuộc repo được review có cấu hình gì hay
// không. Khác với repoInstructions (issue #7/#23, đọc từ .yuumi.yml — rule
// người dùng/maintainer của repo TỰ khai báo riêng cho repo họ): rule ở đây
// luôn có sẵn cho mọi repo bot review, xem issue #25.
type languageRule struct {
	// extensions là danh sách đuôi file (kèm dấu ".") áp dụng rule này —
	// dùng matchesAnyPattern (diffsplit.go) để khớp, cùng cơ chế suffix-match
	// đang dùng cho extraIgnoredPatterns, không cần thêm glob engine riêng.
	extensions []string
	label      string // tên hiển thị trong prompt, vd "Go"
	rules      string // nội dung rule, chèn nguyên văn vào prompt
}

// defaultLanguageRules bắt đầu với vài ngôn ngữ phổ biến nhất bot thực tế
// sẽ gặp (Go là chắc chắn — chính project này viết bằng Go — cộng thêm
// JS/TS/Python/SQL) — không cần phủ hết mọi ngôn ngữ ngay từ đầu, thêm dần
// khi thực tế cần (xem issue #25).
var defaultLanguageRules = []languageRule{
	{
		extensions: []string{".go"},
		label:      "Go",
		rules: "- Race condition: biến shared giữa goroutine có được bảo vệ đúng (mutex/channel) không.\n" +
			"- Error wrapping: lỗi trả lên có dùng fmt.Errorf(\"...: %w\", err) để giữ lại lỗi gốc, thay vì nuốt mất hoặc tạo lỗi mới không rõ nguồn.\n" +
			"- Context leak: context.WithCancel/WithTimeout có luôn được cancel() (thường qua defer) không.\n" +
			"- Goroutine leak: goroutine có cách dừng đúng lúc (channel/context) không, tránh chạy mãi sau khi caller đã xong việc.",
	},
	{
		extensions: []string{".ts", ".tsx", ".js", ".jsx"},
		label:      "JavaScript/TypeScript",
		rules: "- Promise/async: có await đầy đủ không, có floating promise (Promise bị bỏ quên, không ai await/catch) không.\n" +
			"- Type safety (TS): có lạm dụng any/as để né type check không.\n" +
			"- null/undefined: có kiểm tra trước khi truy cập field/gọi method trên giá trị có thể null/undefined không.",
	},
	{
		extensions: []string{".py"},
		label:      "Python",
		rules: "- Mutable default argument (vd def f(x=[])) — lỗi kinh điển, giá trị mặc định bị CHIA SẺ giữa các lần gọi.\n" +
			"- Exception quá rộng: có bắt \"except:\" trần hoặc \"except Exception\" nuốt hết lỗi thay vì bắt đúng loại lỗi cụ thể không.\n" +
			"- Resource: file/connection có dùng \"with\" (context manager) để đảm bảo đóng đúng không.",
	},
	{
		extensions: []string{".sql"},
		label:      "SQL",
		rules: "- N+1 query: có vòng lặp gọi query riêng cho từng phần tử thay vì 1 query gộp/JOIN không.\n" +
			"- Thiếu index: WHERE/JOIN/ORDER BY trên cột không có khả năng đã được index.\n" +
			"- SQL injection: query có nối string trực tiếp từ input thay vì dùng parameter/placeholder không.",
	},
}

// languageRulesForDiff xác định rule mặc định áp dụng cho diff, dựa trên
// extension của TỪNG file có mặt trong diff — không chỉ loại chiếm đa số:
// 1 bundle nhỏ vẫn có thể gồm nhiều loại file khác nhau (vd 1 handler Go
// gọi kèm 1 file .sql migration), bỏ sót rule của loại thiểu số chỉ vì nó
// ít file hơn thì phí, không có lý do chính đáng để loại trừ.
//
// Trả về "" nếu diff rỗng hoặc không file nào khớp rule nào đã định nghĩa —
// file chưa có rule riêng vẫn review bình thường với hướng dẫn chung, không
// breaking change (đúng acceptance criteria issue #25).
//
// Giữ đúng THỨ TỰ defaultLanguageRules (không phải thứ tự file xuất hiện
// trong diff) khi ghép nhiều rule lại — kết quả ổn định/deterministic giữa
// các lần gọi cho cùng 1 diff.
func languageRulesForDiff(diff string) string {
	matched := make([]bool, len(defaultLanguageRules))

	for _, fileDiff := range splitDiffByFile(diff) {
		p := extractFilePath(fileDiff)
		if p == "" {
			continue
		}
		for i, rule := range defaultLanguageRules {
			if !matched[i] && matchesAnyPattern(p, rule.extensions) {
				matched[i] = true
			}
		}
	}

	var b strings.Builder
	for i, rule := range defaultLanguageRules {
		if !matched[i] {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "%s:\n%s", rule.label, rule.rules)
	}
	return b.String()
}
