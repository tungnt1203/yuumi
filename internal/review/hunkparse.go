package review

// File này parse diff ở cấp hunk/dòng (path + line ranges + map từng dòng
// +/-/context về đúng số dòng trong file cũ/mới) — nền tảng để sau này định
// vị comment vào đúng dòng code qua GitHub Reviews API (issue #5), điều mà
// splitDiffByFile (cắt theo file, không parse hunk) không đáp ứng được.
//
// Issue #24: bản thân file này CHƯA được dùng ở đâu trong pipeline hiện tại
// (Job vẫn gửi nguyên diff dạng text cho Reviewer.Review như trước) — việc
// dùng kết quả parse ở đây để định vị comment thuộc issue #5, làm riêng sau.

import (
	"regexp"
	"strconv"
	"strings"
)

// LineKind phân loại 1 dòng trong hunk: giữ nguyên (context), thêm mới, hay
// bị xoá — suy ra trực tiếp từ ký tự đầu dòng unified diff (' ', '+', '-').
type LineKind int

const (
	LineContext LineKind = iota
	LineAdded
	LineRemoved
)

// HunkLine là 1 dòng trong hunk, kèm số dòng tương ứng ở file cũ/mới. OldLine
// == 0 nghĩa là dòng này không tồn tại ở file cũ (LineAdded); NewLine == 0
// nghĩa là không tồn tại ở file mới (LineRemoved) — dòng 0 không hợp lệ
// trong đánh số dòng file (git bắt đầu từ 1) nên dùng được làm giá trị "N/A".
type HunkLine struct {
	Kind    LineKind
	Content string // nội dung dòng, đã bỏ ký tự +/-/space ở đầu
	OldLine int
	NewLine int
}

// Hunk là 1 khối thay đổi liên tục trong diff của 1 file, tương ứng 1 header
// "@@ -OldStart,OldLines +NewStart,NewLines @@".
type Hunk struct {
	OldStart, OldLines int
	NewStart, NewLines int
	Lines              []HunkLine
}

// FileDiff là kết quả parse hunk-level cho diff của 1 file (unified diff
// git tạo ra, xem splitDiffByFile). OldPath/NewPath rỗng khi file không tồn
// tại ở phía đó (file mới/bị xoá) — xem parseFileHunks.
type FileDiff struct {
	OldPath string
	NewPath string
	Hunks   []Hunk
}

// LineAtNew tìm dòng tương ứng với số dòng n trong file MỚI (chỉ context
// hoặc added — dòng removed không tồn tại ở file mới nên không bao giờ
// khớp). ok=false nếu n không nằm trong hunk nào đã parse được (context xa
// hunk, git diff không show ra nên không xác định được).
// coversNewLineRange báo mọi dòng file mới từ start đến end (kể cả hai đầu)
// đều là dòng comment được (context hoặc added) và nằm trong CÙNG một hunk.
// GitHub từ chối cả review — request atomic, sai một comment mất hết — nếu
// multi-line comment vượt hunk hoặc trỏ dòng không có trong diff.
func (fd FileDiff) coversNewLineRange(start, end int) bool {
	if start <= 0 || end < start {
		return false
	}
	want := end - start + 1
	for _, h := range fd.Hunks {
		count := 0
		for _, l := range h.Lines {
			if l.Kind == LineRemoved || l.NewLine < start || l.NewLine > end {
				continue
			}
			count++
		}
		if count == want {
			return true
		}
	}
	return false
}

func (fd FileDiff) LineAtNew(n int) (HunkLine, bool) {
	for _, h := range fd.Hunks {
		for _, l := range h.Lines {
			if l.NewLine == n && l.Kind != LineRemoved {
				return l, true
			}
		}
	}
	return HunkLine{}, false
}

// LineAtOld là bản đối xứng của LineAtNew cho file CŨ (chỉ context hoặc
// removed).
func (fd FileDiff) LineAtOld(n int) (HunkLine, bool) {
	for _, h := range fd.Hunks {
		for _, l := range h.Lines {
			if l.OldLine == n && l.Kind != LineAdded {
				return l, true
			}
		}
	}
	return HunkLine{}, false
}

// hunkHeaderRe khớp "@@ -oldStart[,oldLines] +newStart[,newLines] @@..."
// (phần sau "@@" thứ 2, thường là tên hàm/context, bị bỏ qua — không cần
// cho việc map số dòng). oldLines/newLines mặc định 1 khi bị lược bỏ (đúng
// theo spec unified diff — 1 dòng thay đổi không cần ghi ",1").
var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// parseFileHunks parse hunk-level 1 file diff (dạng splitDiffByFile trả
// về, bắt đầu bằng "diff --git a/... b/..."). Fail-safe theo đúng cách
// extractFilePath/splitDiffByFile hiện có: input không đúng định dạng
// (không bắt đầu bằng "diff --git ") trả về FileDiff{} rỗng thay vì panic;
// input đúng định dạng nhưng có phần dị dạng/bị cắt giữa chừng (vd
// truncateDiff) vẫn trả về những gì parse được TRƯỚC chỗ dị dạng, không cố
// đoán phần còn lại.
func parseFileHunks(fileDiffBody string) FileDiff {
	lines := strings.Split(fileDiffBody, "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "diff --git ") {
		return FileDiff{}
	}

	// Baseline lấy từ dòng "diff --git a/X b/Y" — luôn có 2 path đối xứng,
	// kể cả khi file là binary hoặc chỉ đổi permission (không có dòng
	// "--- "/"+++ " hay "rename from/to" nào để ghi đè lại cho chính xác
	// hơn ở dưới).
	fd := FileDiff{}
	fd.OldPath, fd.NewPath = gitHeaderPaths(lines[0])

	for i := 1; i < len(lines); i++ {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "--- "):
			fd.OldPath = diffPathLine(line, "--- ")
		case strings.HasPrefix(line, "+++ "):
			fd.NewPath = diffPathLine(line, "+++ ")
		case strings.HasPrefix(line, "rename from "):
			fd.OldPath = strings.TrimPrefix(line, "rename from ")
		case strings.HasPrefix(line, "rename to "):
			fd.NewPath = strings.TrimPrefix(line, "rename to ")
		case strings.HasPrefix(line, "@@ "):
			hunk, consumed, ok := parseHunk(lines, i)
			if !ok {
				// Header "@@ " nhưng không khớp định dạng mong đợi — bỏ qua
				// đúng dòng này, không cố đoán, tiếp tục tìm hunk kế tiếp.
				continue
			}
			fd.Hunks = append(fd.Hunks, hunk)
			i += consumed - 1 // vòng lặp tự +1, trừ trước 1 để không nhảy đè dòng kế
		}
	}

	return fd
}

// gitHeaderPaths trích 2 path từ dòng "diff --git a/<old> b/<new>". Dùng
// strings.Fields (tách theo khoảng trắng) như extractFilePath hiện có —
// cùng hạn chế đã biết: path chứa dấu cách sẽ bị tách sai, chấp nhận được
// vì git escape (quote) path như vậy thay vì để nguyên trong dòng này.
func gitHeaderPaths(line string) (oldPath, newPath string) {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return "", ""
	}
	return strings.TrimPrefix(fields[2], "a/"), strings.TrimPrefix(fields[3], "b/")
}

// diffPathLine trích path từ dòng "--- a/<path>"/"+++ b/<path>", trả về ""
// cho "/dev/null" (file không tồn tại ở phía đó — file mới hoặc bị xoá).
func diffPathLine(line, prefix string) string {
	p := strings.TrimPrefix(line, prefix)
	if p == "/dev/null" {
		return ""
	}
	if strings.HasPrefix(p, "a/") || strings.HasPrefix(p, "b/") {
		p = p[2:]
	}
	return p
}

// parseHunk parse 1 hunk bắt đầu tại lines[start] (dòng header "@@ ... @@").
// consumed là số dòng đã tiêu thụ TÍNH TỪ start (kể cả header) — caller
// dùng để nhảy qua đúng số dòng đó. ok=false chỉ khi header không khớp
// hunkHeaderRe (caller bỏ qua đúng 1 dòng header lỗi, không panic).
func parseHunk(lines []string, start int) (h Hunk, consumed int, ok bool) {
	m := hunkHeaderRe.FindStringSubmatch(lines[start])
	if m == nil {
		return Hunk{}, 1, false
	}

	h = Hunk{
		OldStart: atoiDefault(m[1], 0),
		OldLines: atoiDefault(m[2], 1),
		NewStart: atoiDefault(m[3], 0),
		NewLines: atoiDefault(m[4], 1),
	}

	oldLine, newLine := h.OldStart, h.NewStart
	oldConsumed, newConsumed := 0, 0

	i := start + 1
	for i < len(lines) && (oldConsumed < h.OldLines || newConsumed < h.NewLines) {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "\\"):
			// "\ No newline at end of file" — không phải nội dung dòng,
			// không tính vào bộ đếm old/new.
		case strings.HasPrefix(line, "+"):
			h.Lines = append(h.Lines, HunkLine{Kind: LineAdded, Content: line[1:], NewLine: newLine})
			newLine++
			newConsumed++
		case strings.HasPrefix(line, "-"):
			h.Lines = append(h.Lines, HunkLine{Kind: LineRemoved, Content: line[1:], OldLine: oldLine})
			oldLine++
			oldConsumed++
		case strings.HasPrefix(line, " "):
			h.Lines = append(h.Lines, HunkLine{Kind: LineContext, Content: line[1:], OldLine: oldLine, NewLine: newLine})
			oldLine++
			newLine++
			oldConsumed++
			newConsumed++
		default:
			// Dòng không đúng định dạng hunk (diff dị dạng, hoặc bị cắt
			// giữa chừng bởi truncateDiff) — dừng lại tại đây, trả về những
			// gì đã parse được thay vì đoán bừa hoặc panic.
			return h, i - start, true
		}
		i++
	}

	return h, i - start, true
}

// atoiDefault parse s thành int, trả về def nếu s rỗng (phần ",N" bị lược
// bỏ trong header hunk, mặc định 1 theo spec) hoặc không parse được (dị
// dạng — fail-safe, không panic).
func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
