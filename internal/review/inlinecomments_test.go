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
