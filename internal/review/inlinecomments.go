package review

import (
	"strings"
	"unicode"
)

// pendingComment là 1 finding đã được XÁC THỰC khớp đúng với 1 dòng thật
// trong diff (qua parseFileHunks/FileDiff.LineAtNew, issue #24) — sẵn sàng
// gửi thành 1 inline comment qua GitHub Reviews API (issue #5). Body đã
// được render sẵn (renderInlineFinding) để Job.postInlineComments không cần biết
// gì về Finding, chỉ việc gửi đi.
type pendingComment struct {
	Path string
	// StartLine > 0 và nhỏ hơn Line nghĩa là comment phủ một khoảng dòng
	// (GitHub start_line/line) — chỉ set khi suggestion thay nhiều dòng và
	// mọi dòng trong khoảng đều nằm trong cùng một hunk. 0 = comment 1 dòng.
	StartLine int
	Line      int
	Body      string
}

// splitFindingsForPosting tách findings thành 2 nhóm:
//   - inline: có File+Line hợp lệ VÀ khớp đúng với 1 dòng thật trong diff.
//   - general: không có File/Line (nhận xét tổng quát, kiến trúc...), hoặc
//     có nhưng không đối chiếu được với diff thật.
//
// Bắt buộc đối chiếu lại với diff thật (không tin thẳng Finding.File/Line
// Claude tự báo) vì model có thể diễn giải lại thay vì copy nguyên văn số
// dòng — post nhầm dòng còn tệ hơn không post inline, nên finding không xác
// thực được rơi về hiển thị trong comment tổng hợp (general) thay vì bị bỏ
// luôn hay post sai chỗ trong im lặng (đúng ghi chú "fallback verify" của
// issue #24).
//
// diff phải là diff của ĐÚNG bundle đã gửi cho Reviewer sinh ra findings
// này (không phải diff của cả PR khi PR bị chia nhiều bundle) — Line trong
// Finding chỉ có nghĩa trong phạm vi diff Claude thực sự đã thấy.
func splitFindingsForPosting(diff string, findings []Finding) (inline []pendingComment, general []Finding) {
	index := buildFileDiffIndex(diff)

	for _, f := range findings {
		if f.File == "" || f.Line <= 0 {
			general = append(general, f)
			continue
		}
		fd, ok := index[f.File]
		if !ok {
			general = append(general, f)
			continue
		}
		if _, ok := fd.LineAtNew(f.Line); !ok {
			general = append(general, f)
			continue
		}
		start, line := suggestionLineRange(fd, f)
		body := renderInlineFinding(f)
		if suggestionRepeatsNeighbor(fd, start, line, f.Suggestion) {
			// Khoảng dòng hẹp hơn đoạn code suggestion viết lại — bấm
			// "Commit suggestion" sẽ nhân đôi dòng kế bên. Vẫn gắn inline
			// nhưng hiện suggestion thành code block thường.
			body = renderFinding(f)
		}
		inline = append(inline, pendingComment{
			Path:      f.File,
			StartLine: start,
			Line:      line,
			Body:      body,
		})
	}

	return inline, general
}

// suggestionLineRange quyết định khoảng dòng review comment sẽ thay.
// Suggestion 1 dòng, hoặc không khai báo EndLine, chỉ gắn đúng Finding.Line
// (StartLine trả về 0). Suggestion nhiều dòng chỉ phủ start_line..line khi
// EndLine > Line và coversNewLineRange xác nhận cả khoảng nằm trong cùng
// một hunk — nếu không, giữ comment 1 dòng: nội dung ```suggestion vẫn có
// thể dài nhiều dòng và GitHub sẽ thay đúng dòng đó, không xoá nhầm các
// dòng kế bên.
func suggestionLineRange(fd FileDiff, f Finding) (startLine, line int) {
	line = f.Line
	if f.EndLine <= f.Line {
		return 0, line
	}
	if !fd.coversNewLineRange(f.Line, f.EndLine) {
		return 0, line
	}
	return f.Line, f.EndLine
}

// suggestionRepeatsNeighbor báo suggestion có chứa nguyên văn dòng ngay
// trước hoặc ngay sau khoảng dòng nó thay (startLine..line, startLine 0 là
// chỉ dòng line). Dấu hiệu model gắn khoảng quá hẹp: vd suggestion viết lại
// cả vòng for nhưng chỉ gắn dòng thân vòng lặp — áp dụng sẽ ra 2 dòng for.
// Chỉ so dòng có chữ/số, bỏ qua dòng như "}" hay dòng trống vốn hay lặp.
func suggestionRepeatsNeighbor(fd FileDiff, startLine, line int, suggestion string) bool {
	if startLine <= 0 {
		startLine = line
	}
	lines := make(map[string]bool)
	for _, l := range strings.Split(suggestion, "\n") {
		if t := strings.TrimSpace(l); hasLetterOrDigit(t) {
			lines[t] = true
		}
	}
	for _, n := range []int{startLine - 1, line + 1} {
		if hl, ok := fd.LineAtNew(n); ok && lines[strings.TrimSpace(hl.Content)] {
			return true
		}
	}
	return false
}

func hasLetterOrDigit(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0
}

// buildFileDiffIndex parse hunk-level từng file trong diff, index theo
// NewPath — dùng để tra cứu nhanh khi xác thực Finding.File/Line (xem
// splitFindingsForPosting). File không xác định được NewPath (vd bị xoá
// hoàn toàn — không có gì để comment "vào dòng mới" cả) không được đưa vào
// index, nên finding trỏ tới file đó tự động rơi về general.
func buildFileDiffIndex(diff string) map[string]FileDiff {
	index := make(map[string]FileDiff)
	for _, body := range splitDiffByFile(diff) {
		fd := parseFileHunks(body)
		if fd.NewPath != "" {
			index[fd.NewPath] = fd
		}
	}
	return index
}
