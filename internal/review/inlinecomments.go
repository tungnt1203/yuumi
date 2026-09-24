package review

import (
	"strings"
	"unicode"
)

// pendingComment là 1 finding đã được XÁC THỰC khớp đúng với 1 dòng thật
// trong diff (qua parseFileHunks/FileDiff.LineAtNew, issue #24) — sẵn sàng
// gửi thành 1 inline comment qua GitHub Reviews API (issue #5). Body đã
// được render sẵn (renderInlineFinding, hoặc renderFinding khi
// suggestionRepeatsNeighbor thấy khoảng dòng bị gắn quá hẹp) để
// Job.postInlineComments không cần biết gì về Finding, chỉ việc gửi đi.
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
// Finding có ExistingCode thì vị trí lấy theo chỗ đoạn code đó xuất hiện
// trong diff (locateExistingCode), không theo Line: đúng file nhưng lệch
// dòng vẫn gắn đúng chỗ, còn đoạn code không có trong diff thì về general
// (issue #71).
//
// diff phải là diff của ĐÚNG bundle đã gửi cho Reviewer sinh ra findings
// này (không phải diff của cả PR khi PR bị chia nhiều bundle) — Line trong
// Finding chỉ có nghĩa trong phạm vi diff Claude thực sự đã thấy.
func splitFindingsForPosting(diff string, findings []Finding) (inline []pendingComment, general []Finding) {
	index := buildFileDiffIndex(diff)

	for _, f := range findings {
		code := codeLines(f.ExistingCode)
		useCode := hasDistinctiveLine(code)
		// Line <= 0 vẫn gắn inline được nếu existing_code định vị được
		// (khớp đúng 1 chỗ, xem locateExistingCode).
		if f.File == "" || (f.Line <= 0 && !useCode) {
			general = append(general, f)
			continue
		}
		fd, ok := index[f.File]
		if !ok {
			general = append(general, f)
			continue
		}
		if useCode {
			start, end, ok := locateExistingCode(fd, code, f.Line)
			if !ok {
				// Đoạn code Claude trích không có trong diff: gắn theo số
				// dòng dễ trúng sai chỗ, đưa về comment tổng hợp.
				general = append(general, f)
				continue
			}
			switch {
			case end > start:
				f.EndLine = end
			case f.Line > 0 && f.EndLine > f.Line:
				// existing_code chỉ chép dòng đầu của khoảng Claude báo:
				// giữ độ dài khoảng đó, dời theo chỗ khớp.
				f.EndLine += start - f.Line
			default:
				f.EndLine = 0
			}
			f.Line = start
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

// codeLines tách đoạn code thành từng dòng đã bỏ khoảng trắng hai đầu, bỏ
// dòng trống ở đầu/cuối. So khớp bỏ qua thụt lề vì model hay chép lệch tab
// với space.
func codeLines(code string) []string {
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(code, "\r\n", "\n"), "\n") {
		lines = append(lines, strings.TrimSpace(l))
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// hasDistinctiveLine báo có ít nhất 1 dòng chứa chữ/số. Đoạn chỉ gồm "}"
// hay ")" khớp ở quá nhiều chỗ, không dùng để xác định vị trí được.
func hasDistinctiveLine(lines []string) bool {
	for _, l := range lines {
		if hasLetterOrDigit(l) {
			return true
		}
	}
	return false
}

// locateExistingCode tìm các dòng liên tiếp trong cùng 1 hunk (file mới,
// bỏ dòng removed) có nội dung khớp want, trả khoảng dòng của chỗ khớp gần
// hint (Finding.Line) nhất. ok=false khi không khớp chỗ nào, hoặc có nhiều
// chỗ khớp mà không chọn được 1 chỗ gần hint nhất (hint <= 0 hoặc 2 chỗ
// cách đều) — thà không gắn inline còn hơn gắn nhầm (issue #71).
func locateExistingCode(fd FileDiff, want []string, hint int) (start, end int, ok bool) {
	matches := 0
	bestDist := -1
	tie := false
	for _, h := range fd.Hunks {
		var lines []HunkLine
		for _, l := range h.Lines {
			if l.Kind != LineRemoved {
				lines = append(lines, l)
			}
		}
		for i := 0; i+len(want) <= len(lines); i++ {
			if !linesMatch(lines[i:i+len(want)], want) {
				continue
			}
			matches++
			dist := abs(lines[i].NewLine - hint)
			switch {
			case bestDist == -1 || dist < bestDist:
				bestDist, tie = dist, false
				start, end = lines[i].NewLine, lines[i+len(want)-1].NewLine
			case dist == bestDist:
				tie = true
			}
		}
	}
	if matches == 0 || tie || (hint <= 0 && matches > 1) {
		return 0, 0, false
	}
	return start, end, true
}

func linesMatch(lines []HunkLine, want []string) bool {
	for i, l := range lines {
		if strings.TrimSpace(l.Content) != want[i] {
			return false
		}
	}
	return true
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
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
