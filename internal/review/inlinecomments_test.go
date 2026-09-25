package review

import (
	"strings"
	"testing"
)

const sampleDiff = "diff --git a/main.go b/main.go\n" +
	"--- a/main.go\n" +
	"+++ b/main.go\n" +
	"@@ -1,2 +1,3 @@\n" +
	" package main\n" +
	"+import \"fmt\"\n" +
	" var x = 1"

func TestSplitFindingsForPosting_ValidFileAndLine_GoesInline(t *testing.T) {
	findings := []Finding{
		{File: "main.go", Line: 2, Severity: "high", Message: "unused import"},
	}

	inline, general := splitFindingsForPosting(sampleDiff, findings)

	if len(inline) != 1 {
		t.Fatalf("got %d inline, want 1", len(inline))
	}
	if inline[0].Path != "main.go" || inline[0].Line != 2 {
		t.Errorf("inline[0] = %+v, want path=main.go line=2", inline[0])
	}
	if !strings.Contains(inline[0].Body, "unused import") {
		t.Errorf("inline[0].Body = %q, want it to contain the message", inline[0].Body)
	}
	if len(general) != 0 {
		t.Errorf("got %d general, want 0", len(general))
	}
}

func TestSplitFindingsForPosting_NoFileOrLine_GoesGeneral(t *testing.T) {
	findings := []Finding{
		{Severity: "medium", Message: "kiến trúc tổng thể chưa nhất quán"},
	}

	inline, general := splitFindingsForPosting(sampleDiff, findings)

	if len(inline) != 0 {
		t.Errorf("got %d inline, want 0", len(inline))
	}
	if len(general) != 1 {
		t.Fatalf("got %d general, want 1", len(general))
	}
}

func TestSplitFindingsForPosting_UnknownFile_GoesGeneral(t *testing.T) {
	// Claude "bịa" ra 1 file không có trong diff — không được post nhầm.
	findings := []Finding{
		{File: "does_not_exist.go", Line: 1, Severity: "high", Message: "m"},
	}

	inline, general := splitFindingsForPosting(sampleDiff, findings)

	if len(inline) != 0 {
		t.Errorf("got %d inline, want 0 (file not in diff)", len(inline))
	}
	if len(general) != 1 {
		t.Fatalf("got %d general, want 1", len(general))
	}
}

func TestSplitFindingsForPosting_LineNotInAnyHunk_GoesGeneral(t *testing.T) {
	// File đúng nhưng dòng Claude báo lại không khớp diff thật (model diễn
	// giải lại thay vì copy nguyên văn số dòng) — xem issue #24's ghi chú
	// về mismatch detection: không post sai chỗ trong im lặng.
	findings := []Finding{
		{File: "main.go", Line: 999, Severity: "high", Message: "m"},
	}

	inline, general := splitFindingsForPosting(sampleDiff, findings)

	if len(inline) != 0 {
		t.Errorf("got %d inline, want 0 (line not covered by any hunk)", len(inline))
	}
	if len(general) != 1 {
		t.Fatalf("got %d general, want 1", len(general))
	}
}

func TestSplitFindingsForPosting_ZeroOrNegativeLine_GoesGeneral(t *testing.T) {
	findings := []Finding{
		{File: "main.go", Line: 0, Severity: "high", Message: "a"},
		{File: "main.go", Line: -1, Severity: "high", Message: "b"},
	}

	inline, general := splitFindingsForPosting(sampleDiff, findings)

	if len(inline) != 0 {
		t.Errorf("got %d inline, want 0", len(inline))
	}
	if len(general) != 2 {
		t.Fatalf("got %d general, want 2", len(general))
	}
}

func TestSplitFindingsForPosting_LineOnlyRemoved_GoesGeneral(t *testing.T) {
	// Hunk toàn dòng bị xoá, không còn context/added nào (xoá hẳn code,
	// không thay thế) — không có dòng nào ở file MỚI trong hunk này, comment
	// gắn vào file MỚI (side=RIGHT luôn) không có chỗ nào hợp lệ để post.
	diff := "diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -5,2 +5,0 @@\n" +
		"-line5\n" +
		"-line6"
	findings := []Finding{
		{File: "main.go", Line: 5, Severity: "high", Message: "m"},
	}

	inline, general := splitFindingsForPosting(diff, findings)

	if len(inline) != 0 {
		t.Errorf("got %d inline, want 0 (line only exists as removed, not in new file)", len(inline))
	}
	if len(general) != 1 {
		t.Fatalf("got %d general, want 1", len(general))
	}
}

func TestSplitFindingsForPosting_MixedFindings(t *testing.T) {
	findings := []Finding{
		{File: "main.go", Line: 2, Severity: "high", Message: "valid"},
		{Severity: "low", Message: "general note"},
		{File: "ghost.go", Line: 1, Severity: "low", Message: "unknown file"},
	}

	inline, general := splitFindingsForPosting(sampleDiff, findings)

	if len(inline) != 1 {
		t.Fatalf("got %d inline, want 1", len(inline))
	}
	if len(general) != 2 {
		t.Fatalf("got %d general, want 2", len(general))
	}
}

func TestSplitFindingsForPosting_EmptyDiff_EverythingGeneral(t *testing.T) {
	findings := []Finding{
		{File: "main.go", Line: 2, Severity: "high", Message: "m"},
	}

	inline, general := splitFindingsForPosting("", findings)

	if len(inline) != 0 {
		t.Errorf("got %d inline, want 0 (no diff to validate against)", len(inline))
	}
	if len(general) != 1 {
		t.Fatalf("got %d general, want 1", len(general))
	}
}

func TestSplitFindingsForPosting_Suggestion_UsesSuggestionFence(t *testing.T) {
	findings := []Finding{
		{File: "main.go", Line: 2, Severity: "high", Message: "unused import", Suggestion: `import "fmt"`},
	}

	inline, general := splitFindingsForPosting(sampleDiff, findings)

	if len(inline) != 1 || len(general) != 0 {
		t.Fatalf("got %d inline %d general, want 1 and 0", len(inline), len(general))
	}
	if inline[0].StartLine != 0 || inline[0].Line != 2 {
		t.Errorf("inline range = %d..%d, want single line 2", inline[0].StartLine, inline[0].Line)
	}
	if !strings.Contains(inline[0].Body, "```suggestion\nimport \"fmt\"\n```") {
		t.Errorf("inline body = %q, want a suggestion fence", inline[0].Body)
	}
}

func TestSplitFindingsForPosting_MultiLineSuggestionWithoutEndLine_StaysSingleLine(t *testing.T) {
	findings := []Finding{
		{File: "main.go", Line: 2, Severity: "high", Message: "m", Suggestion: "a\nb"},
	}

	inline, _ := splitFindingsForPosting(sampleDiff, findings)

	if len(inline) != 1 {
		t.Fatalf("got %d inline, want 1", len(inline))
	}
	if inline[0].StartLine != 0 || inline[0].Line != 2 {
		t.Errorf("inline range = %d..%d, want single line 2 (no end_line)", inline[0].StartLine, inline[0].Line)
	}
	if !strings.Contains(inline[0].Body, "```suggestion\na\nb\n```") {
		t.Errorf("inline body = %q, want multi-line suggestion fence", inline[0].Body)
	}
}

func TestSplitFindingsForPosting_EndLineInSameHunk_SetsRange(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -1,2 +1,4 @@\n" +
		" package main\n" +
		"+import \"fmt\"\n" +
		"+import \"os\"\n" +
		" var x = 1"
	findings := []Finding{
		{File: "main.go", Line: 2, EndLine: 3, Severity: "high", Message: "m", Suggestion: "import (\n\t\"fmt\"\n\t\"os\"\n)"},
	}

	inline, general := splitFindingsForPosting(diff, findings)

	if len(general) != 0 || len(inline) != 1 {
		t.Fatalf("got %d inline %d general, want 1 and 0", len(inline), len(general))
	}
	if inline[0].StartLine != 2 || inline[0].Line != 3 {
		t.Errorf("inline range = %d..%d, want 2..3", inline[0].StartLine, inline[0].Line)
	}
}

func TestSplitFindingsForPosting_EndLineOutsideHunk_FallsBackToSingleLine(t *testing.T) {
	findings := []Finding{
		{File: "main.go", Line: 2, EndLine: 99, Severity: "high", Message: "m", Suggestion: "a\nb"},
	}

	inline, general := splitFindingsForPosting(sampleDiff, findings)

	if len(general) != 0 || len(inline) != 1 {
		t.Fatalf("got %d inline %d general, want 1 and 0", len(inline), len(general))
	}
	if inline[0].StartLine != 0 || inline[0].Line != 2 {
		t.Errorf("inline range = %d..%d, want single line 2 when end_line is not in the diff", inline[0].StartLine, inline[0].Line)
	}
}

func TestSplitFindingsForPosting_EndLineAcrossHunks_FallsBackToSingleLine(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -1,1 +1,1 @@\n" +
		"-old\n" +
		"+new1\n" +
		"@@ -10,1 +10,1 @@\n" +
		"-old2\n" +
		"+new2"
	findings := []Finding{
		{File: "main.go", Line: 1, EndLine: 10, Severity: "high", Message: "m", Suggestion: "x\ny"},
	}

	inline, _ := splitFindingsForPosting(diff, findings)

	if len(inline) != 1 {
		t.Fatalf("got %d inline, want 1", len(inline))
	}
	if inline[0].StartLine != 0 || inline[0].Line != 1 {
		t.Errorf("inline range = %d..%d, want single line 1 when end_line crosses hunks", inline[0].StartLine, inline[0].Line)
	}
}

func TestBuildFileDiffIndex_IndexesByNewPath(t *testing.T) {
	diff := sampleDiff + "\n" +
		"diff --git a/other.go b/other.go\n" +
		"--- a/other.go\n" +
		"+++ b/other.go\n" +
		"@@ -1,1 +1,1 @@\n" +
		"-old\n" +
		"+new"

	index := buildFileDiffIndex(diff)

	if len(index) != 2 {
		t.Fatalf("got %d entries, want 2: %v", len(index), index)
	}
	if _, ok := index["main.go"]; !ok {
		t.Error("expected main.go in index")
	}
	if _, ok := index["other.go"]; !ok {
		t.Error("expected other.go in index")
	}
}

// Trường hợp thật ở PR #81: suggestion viết lại cả vòng for nhưng chỉ gắn
// dòng thân vòng lặp. Áp dụng sẽ ra 2 dòng for, nên không hiện nút
// "Commit suggestion" mà hiện code block thường, vẫn gắn đúng dòng.
func TestSplitFindingsForPosting_SuggestionRepeatsLineBefore_PlainBlock(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -1,3 +1,3 @@\n" +
		" for _, key := range keys {\n" +
		"-\tprint(key)\n" +
		"+\tprintRow(key)\n" +
		" }"
	findings := []Finding{
		{File: "main.go", Line: 2, Severity: "low", Message: "m", Suggestion: "for _, key := range keys {\n\tprintRow(label(key))\n}"},
	}

	inline, general := splitFindingsForPosting(diff, findings)

	if len(general) != 0 || len(inline) != 1 {
		t.Fatalf("got %d inline %d general, want 1 and 0", len(inline), len(general))
	}
	if inline[0].Line != 2 {
		t.Errorf("inline line = %d, want 2", inline[0].Line)
	}
	if strings.Contains(inline[0].Body, "```suggestion") {
		t.Errorf("inline body = %q, want no suggestion fence", inline[0].Body)
	}
	if !strings.Contains(inline[0].Body, "printRow(label(key))") {
		t.Errorf("inline body = %q, want suggestion shown as plain code", inline[0].Body)
	}
}

func TestSplitFindingsForPosting_SuggestionRepeatsLineAfter_PlainBlock(t *testing.T) {
	findings := []Finding{
		{File: "main.go", Line: 2, Severity: "low", Message: "m", Suggestion: "import \"os\"\nvar x = 1"},
	}

	inline, _ := splitFindingsForPosting(sampleDiff, findings)

	if len(inline) != 1 || strings.Contains(inline[0].Body, "```suggestion") {
		t.Fatalf("inline = %+v, want 1 comment without suggestion fence", inline)
	}
}

// Dòng kế bên chỉ là "}" thì lặp lại là bình thường, vẫn giữ nút suggestion.
func TestSplitFindingsForPosting_SuggestionRepeatsBraceOnly_KeepsFence(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -1,3 +1,3 @@\n" +
		" func f() {\n" +
		"-\ta()\n" +
		"+\tb()\n" +
		" }"
	findings := []Finding{
		{File: "main.go", Line: 2, Severity: "low", Message: "m", Suggestion: "\tif ok {\n\t\tb()\n\t}"},
	}

	inline, _ := splitFindingsForPosting(diff, findings)

	if len(inline) != 1 || !strings.Contains(inline[0].Body, "```suggestion") {
		t.Fatalf("inline = %+v, want suggestion fence kept", inline)
	}
}

// diff có 2 hunk, dùng cho các test existing_code (issue #71). Dòng file
// mới: 1 package main, 2 import "fmt", 3 var x = 1 | 10 func f() {,
// 11 return x, 12 }.
const twoHunkDiff = "diff --git a/main.go b/main.go\n" +
	"--- a/main.go\n" +
	"+++ b/main.go\n" +
	"@@ -1,2 +1,3 @@\n" +
	" package main\n" +
	"+import \"fmt\"\n" +
	" var x = 1\n" +
	"@@ -9,3 +10,3 @@\n" +
	" func f() {\n" +
	"-\treturn 0\n" +
	"+\treturn x\n" +
	" }"

// Claude báo lệch dòng nhưng existing_code đúng: gắn theo nội dung.
func TestSplitFindingsForPosting_ExistingCode_FixesWrongLine(t *testing.T) {
	findings := []Finding{
		{File: "main.go", Line: 3, Severity: "low", Message: "m", ExistingCode: "\treturn x"},
	}

	inline, general := splitFindingsForPosting(twoHunkDiff, findings)

	if len(general) != 0 || len(inline) != 1 {
		t.Fatalf("got %d inline %d general, want 1 and 0", len(inline), len(general))
	}
	if inline[0].Line != 11 {
		t.Errorf("inline line = %d, want 11", inline[0].Line)
	}
}

// existing_code nhiều dòng đặt luôn khoảng dòng cho suggestion, kể cả khi
// Claude không điền end_line.
func TestSplitFindingsForPosting_ExistingCode_SetsRange(t *testing.T) {
	findings := []Finding{
		{File: "main.go", Line: 10, Severity: "low", Message: "m", ExistingCode: "func f() {\n    return x", Suggestion: "func f() int {\n\treturn x"},
	}

	inline, _ := splitFindingsForPosting(twoHunkDiff, findings)

	if len(inline) != 1 || inline[0].StartLine != 10 || inline[0].Line != 11 {
		t.Fatalf("inline = %+v, want range 10..11", inline)
	}
}

// Đoạn code không có trong diff: không gắn inline, kể cả khi Line hợp lệ.
func TestSplitFindingsForPosting_ExistingCode_NotInDiff_GoesGeneral(t *testing.T) {
	findings := []Finding{
		{File: "main.go", Line: 2, Severity: "low", Message: "m", ExistingCode: "import \"os\""},
	}

	inline, general := splitFindingsForPosting(twoHunkDiff, findings)

	if len(inline) != 0 || len(general) != 1 {
		t.Fatalf("got %d inline %d general, want 0 and 1", len(inline), len(general))
	}
}

// Khớp nhiều chỗ: chọn chỗ gần Line nhất; không có Line thì không đoán.
func TestLocateExistingCode_MultipleMatches(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -1,3 +1,5 @@\n" +
		" x++\n" +
		"+y := 1\n" +
		" x++\n" +
		"+z := 2\n" +
		" x++"
	fd := buildFileDiffIndex(diff)["main.go"]
	want := []string{"x++"}

	if _, _, ok := locateExistingCode(fd, want, 4); ok {
		t.Error("hint 4 is equally far from lines 3 and 5: want ok=false")
	}
	if start, _, ok := locateExistingCode(fd, want, 1); !ok || start != 1 {
		t.Errorf("hint 1: got start=%d ok=%v, want 1", start, ok)
	}
	if _, _, ok := locateExistingCode(fd, want, 0); ok {
		t.Error("hint 0 with 3 matches: want ok=false")
	}
	if start, _, ok := locateExistingCode(fd, []string{"y := 1"}, 0); !ok || start != 2 {
		t.Errorf("hint 0 with a single match: got start=%d ok=%v, want 2", start, ok)
	}
}

// Claude để line=0 (không chắc số dòng) nhưng existing_code khớp đúng
// 1 chỗ: vẫn gắn inline.
func TestSplitFindingsForPosting_ExistingCodeWithoutLine_GoesInline(t *testing.T) {
	findings := []Finding{
		{File: "main.go", Severity: "low", Message: "m", ExistingCode: "return x"},
	}

	inline, general := splitFindingsForPosting(twoHunkDiff, findings)

	if len(general) != 0 || len(inline) != 1 || inline[0].Line != 11 {
		t.Fatalf("inline = %+v general = %d, want 1 inline at line 11", inline, len(general))
	}
}

// existing_code chỉ chép dòng đầu nhưng Claude báo end_line: giữ độ dài
// khoảng, dời theo chỗ khớp (line 3..4 báo lệch, khớp thật ở 10 → 10..11).
func TestSplitFindingsForPosting_ExistingCodeFirstLineOnly_ShiftsEndLine(t *testing.T) {
	findings := []Finding{
		{File: "main.go", Line: 9, EndLine: 10, Severity: "low", Message: "m", ExistingCode: "func f() {", Suggestion: "func f() int {\n\treturn x"},
	}

	inline, _ := splitFindingsForPosting(twoHunkDiff, findings)

	if len(inline) != 1 || inline[0].StartLine != 10 || inline[0].Line != 11 {
		t.Fatalf("inline = %+v, want range 10..11", inline)
	}
}

// Dòng trống giữa existing_code khớp với dòng context trống trong diff
// (diff ghi là 1 dấu cách).
func TestSplitFindingsForPosting_ExistingCodeWithBlankLine(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -1,3 +1,4 @@\n" +
		" a := 1\n" +
		" \n" +
		"+b := 2\n" +
		" c := 3"
	findings := []Finding{
		{File: "main.go", Line: 1, Severity: "low", Message: "m", ExistingCode: "a := 1\n\nb := 2"},
	}

	inline, _ := splitFindingsForPosting(diff, findings)

	if len(inline) != 1 || inline[0].StartLine != 1 || inline[0].Line != 3 {
		t.Fatalf("inline = %+v, want range 1..3", inline)
	}
}

// existing_code chỉ gồm "}" không đủ để định vị: bỏ qua, tin Line như cũ.
func TestSplitFindingsForPosting_ExistingCodeBraceOnly_UsesLine(t *testing.T) {
	findings := []Finding{
		{File: "main.go", Line: 2, Severity: "low", Message: "m", ExistingCode: "}"},
	}

	inline, _ := splitFindingsForPosting(twoHunkDiff, findings)

	if len(inline) != 1 || inline[0].Line != 2 {
		t.Fatalf("inline = %+v, want line 2", inline)
	}
}
