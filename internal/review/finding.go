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
//
// File/Line (issue #5) là vị trí Claude cho là finding này áp dụng — File
// rỗng hoặc Line <= 0 nghĩa là nhận xét TỔNG QUÁT, không gắn với 1 dòng cụ
// thể (vd kiến trúc tổng thể). Có File/Line không có nghĩa nó ĐÚNG: Claude
// có thể diễn giải lại thay vì copy nguyên văn số dòng từ diff — luôn phải
// đối chiếu với diff thật (xem splitFindingsForPosting) trước khi tin dùng
// để post inline comment, không dùng thẳng.
type Finding struct {
	File       string `json:"file,omitempty"`
	Line       int    `json:"line,omitempty"`
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

// renderBundleSummary render phần hiển thị trong comment tổng hợp cho 1
// bundle: findings là TOÀN BỘ finding parse được, general là phần con
// KHÔNG gắn inline được (xem splitFindingsForPosting, issue #5) — 2 danh
// sách này khác nhau (general là tập con của findings) nên cần cả 2 để biết
// có bao nhiêu finding đã "biến mất" khỏi đây vì được post inline riêng,
// tránh người đọc tưởng bot bỏ sót.
func renderBundleSummary(findings []Finding, general []Finding) string {
	if len(findings) == 0 {
		return "✅ Không có vấn đề đáng chú ý."
	}

	inlineCount := len(findings) - len(general)

	var b strings.Builder
	if inlineCount > 0 {
		fmt.Fprintf(&b, "_(%d góp ý đã được gắn trực tiếp vào dòng code liên quan — xem tab \"Files changed\".)_", inlineCount)
	}
	if len(general) > 0 {
		if inlineCount > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(renderFindings(general))
	}
	return b.String()
}

// renderFindings render findings thành markdown cho GitHub comment, nhóm
// theo file (xem groupFindingsByFile) và bọc mỗi nhóm trong 1 khối
// <details> gấp gọn được — để comment không dài lê thê khi PR đổi nhiều
// file, người đọc tự mở đúng file mình quan tâm (phong cách các bot review
// phổ biến, đổi qua nhóm-theo-file thay vì liệt kê phẳng từ issue rename
// bot #56).
func renderFindings(findings []Finding) string {
	if len(findings) == 0 {
		return "✅ Không có vấn đề đáng chú ý."
	}

	order, groups := groupFindingsByFile(findings)
	sections := make([]string, len(order))
	for i, file := range order {
		sections[i] = renderFileGroup(file, groups[file])
	}
	return strings.Join(sections, "\n\n")
}

// groupFindingsByFile nhóm findings theo Finding.File, giữ nguyên thứ tự
// xuất hiện đầu tiên của mỗi file (không sort tên file) để gần giống thứ tự
// Claude nhắc tới trong review. Nhóm "" (nhận xét chung, không gắn file cụ
// thể) luôn bị đẩy xuống CUỐI cùng bất kể xuất hiện ở đâu trong input — đọc
// theo file cụ thể trước, nhận xét tổng quát đọc sau.
func groupFindingsByFile(findings []Finding) (order []string, groups map[string][]Finding) {
	groups = map[string][]Finding{}
	for _, f := range findings {
		if _, ok := groups[f.File]; !ok {
			order = append(order, f.File)
		}
		groups[f.File] = append(groups[f.File], f)
	}

	for i, file := range order {
		if file == "" && i != len(order)-1 {
			order = append(append(order[:i], order[i+1:]...), "")
			break
		}
	}
	return order, groups
}

// renderFileGroup render toàn bộ finding của 1 file thành 1 khối <details>,
// sắp theo severity (critical trước) bên trong nhóm đó — dùng sort ổn định
// để không xáo trộn vô nghĩa thứ tự giữa các finding cùng severity so với
// thứ tự Claude trả về. file rỗng nghĩa là nhận xét tổng quát (xem
// Finding.File), hiển thị dưới nhãn "Nhận xét chung" thay vì tên file.
//
// Bắt buộc có dòng trống ngay sau "</summary>" và trước "</details>" — cú
// pháp GitHub cần vậy để render markdown (code block, bold...) bên trong,
// thiếu dòng trống này nội dung sẽ hiện thành text thô không định dạng.
func renderFileGroup(file string, findings []Finding) string {
	sorted := make([]Finding, len(findings))
	copy(sorted, findings)
	sort.SliceStable(sorted, func(i, j int) bool {
		return severityRankOf(sorted[i].Severity) < severityRankOf(sorted[j].Severity)
	})

	label := "📝 Nhận xét chung"
	if file != "" {
		label = fmt.Sprintf("📄 `%s`", file)
	}

	sections := make([]string, len(sorted))
	for i, f := range sorted {
		sections[i] = renderFinding(f)
	}

	return fmt.Sprintf(
		"<details>\n<summary>%s (%d)</summary>\n\n%s\n\n</details>",
		label, len(sorted), strings.Join(sections, "\n\n---\n\n"),
	)
}

// renderReviewHeader render banner mở đầu comment tổng hợp: tiêu đề + bảng
// tổng số finding theo severity trên TOÀN BỘ PR (gộp mọi bundle, kể cả
// những finding đã được post inline riêng — xem Job.reviewBundles) để
// người đọc nắm được bức tranh chung ngay dòng đầu tiên thay vì phải đọc
// hết comment mới biết PR có bao nhiêu vấn đề. 🟣 là màu icon nhận diện
// riêng của bot này, không liên quan/không nhắc tới bot review nào khác.
func renderReviewHeader(findings []Finding) string {
	var b strings.Builder
	b.WriteString("## 🟣 Yuumi Review\n\n")

	if len(findings) == 0 {
		b.WriteString("✅ Không phát hiện vấn đề nào đáng chú ý.")
		return b.String()
	}

	counts := map[string]int{}
	for _, f := range findings {
		counts[strings.ToLower(strings.TrimSpace(f.Severity))]++
	}

	knownOrder := []string{"critical", "high", "medium", "low"}
	known := 0
	b.WriteString("| Mức độ | Số lượng |\n|---|---|\n")
	for _, sev := range knownOrder {
		if counts[sev] == 0 {
			continue
		}
		known += counts[sev]
		fmt.Fprintf(&b, "| %s %s | %d |\n", severityIcon[sev], strings.ToUpper(sev), counts[sev])
	}
	// Severity model trả về không khớp 4 mức chuẩn (sai chính tả, ngôn ngữ
	// khác...) gộp chung vào 1 dòng "Khác" thay vì bỏ sót khỏi tổng số hiển
	// thị (nhất quán với severityRankOf/severityIcon: dữ liệu lạ vẫn được
	// đếm, chỉ xếp/hiển thị khác đi).
	if other := len(findings) - known; other > 0 {
		fmt.Fprintf(&b, "| ⚪ Khác | %d |\n", other)
	}
	fmt.Fprintf(&b, "\n**Tổng: %d góp ý**", len(findings))

	return b.String()
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
