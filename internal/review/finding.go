package review

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Finding là 1 góp ý review có cấu trúc — category + severity (mức độ
// nghiêm trọng) + message (mô tả) + suggestion (gợi ý code cụ thể, optional)
// — thay vì review trả về 1 khối text tự do không phân biệt được "lỗi
// nghiêm trọng phải sửa trước khi merge" với "góp ý style nhỏ" (xem
// BuildReviewPrompt, issue #26).
type Finding struct {
	Category   string `json:"category"`
	Severity   string `json:"severity"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

// severityRank xếp hạng độ ưu tiên hiển thị dùng cho renderFindings —
// critical lên đầu. Severity model trả về không nằm trong danh sách này
// (sai chính tả, ngôn ngữ khác...) vẫn được hiển thị bình thường, chỉ xếp
// cuối cùng thay vì bị loại bỏ — dữ liệu không rõ ràng không có nghĩa là
// không đáng đọc.
var severityRank = map[string]int{
	"critical": 0,
	"high":     1,
	"medium":   2,
	"low":      3,
}

// severityIcon map severity (không phân biệt hoa/thường) sang icon hiển thị
// đầu mỗi finding — giúp người đọc quét nhanh mức độ quan trọng mà không
// cần đọc hết message.
var severityIcon = map[string]string{
	"critical": "🔴",
	"high":     "🟠",
	"medium":   "🟡",
	"low":      "🔵",
}

// parseFindings thử parse text (kỳ vọng là kết quả Reviewer.Review khi
// Claude làm theo đúng format JSON được yêu cầu trong resultFormatInstructions)
// thành danh sách Finding. ok=false nếu không tìm/parse được JSON array hợp
// lệ (Claude trả văn xuôi tự do bất chấp hướng dẫn, hoặc JSON sai schema) —
// caller (Job.reviewBundles) fallback hiển thị nguyên văn text, không làm
// hỏng cả lần review vì lỗi định dạng (xem issue #26 acceptance criteria).
func parseFindings(text string) (findings []Finding, ok bool) {
	arr := extractJSONArray(text)
	if arr == "" {
		return nil, false
	}
	if err := json.Unmarshal([]byte(arr), &findings); err != nil {
		return nil, false
	}
	return findings, true
}

// extractJSONArray tìm đoạn "[...]" trong text để parse. Dù prompt đã dặn
// "CHỈ trả JSON, không bọc trong code fence", Claude thỉnh thoảng vẫn thêm
// vài câu mở đầu hoặc bọc trong ```json ... ``` — lấy từ dấu "[" đầu tiên
// tới dấu "]" cuối cùng chịu được các trường hợp bao quanh đó mà không cần
// parse markdown. Đủ dùng vì output kỳ vọng là 1 mảng phẳng ở top-level,
// không có mảng lồng nào khác xen vào trước/sau nó trong text.
func extractJSONArray(text string) string {
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start == -1 || end == -1 || end < start {
		return ""
	}
	return text[start : end+1]
}

// renderFindings render findings thành markdown cho GitHub comment, sắp
// theo severity (critical trước) để vấn đề quan trọng nhất hiện ngay đầu.
// Dùng sort ổn định để không xáo trộn vô nghĩa thứ tự giữa các finding cùng
// severity so với thứ tự Claude trả về.
func renderFindings(findings []Finding) string {
	if len(findings) == 0 {
		return "✅ Không có vấn đề đáng chú ý."
	}

	sorted := make([]Finding, len(findings))
	copy(sorted, findings)
	sort.SliceStable(sorted, func(i, j int) bool {
		return severityRankOf(sorted[i].Severity) < severityRankOf(sorted[j].Severity)
	})

	sections := make([]string, len(sorted))
	for i, f := range sorted {
		sections[i] = renderFinding(f)
	}
	return strings.Join(sections, "\n\n")
}

// renderFinding render 1 finding thành 1 đoạn markdown: icon severity +
// label severity + category (nếu có) + message, kèm khối code gợi ý sửa
// nếu Claude có cung cấp.
func renderFinding(f Finding) string {
	var b strings.Builder

	icon, ok := severityIcon[strings.ToLower(f.Severity)]
	if !ok {
		icon = "⚪"
	}
	b.WriteString(icon)
	b.WriteString(" **")
	b.WriteString(severityLabel(f.Severity))
	b.WriteString("**")
	if strings.TrimSpace(f.Category) != "" {
		fmt.Fprintf(&b, " `%s`", f.Category)
	}
	b.WriteString(": ")
	b.WriteString(f.Message)

	if strings.TrimSpace(f.Suggestion) != "" {
		b.WriteString("\n\n**Gợi ý sửa:**\n```\n")
		b.WriteString(f.Suggestion)
		b.WriteString("\n```")
	}

	return b.String()
}

func severityRankOf(severity string) int {
	if rank, ok := severityRank[strings.ToLower(severity)]; ok {
		return rank
	}
	return len(severityRank) // không nhận diện được -> xếp cuối, không loại bỏ
}

func severityLabel(severity string) string {
	if strings.TrimSpace(severity) == "" {
		return "N/A"
	}
	return strings.ToUpper(severity)
}
