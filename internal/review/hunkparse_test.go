package review

import (
	"testing"
)

func TestParseFileHunks_SimpleModify(t *testing.T) {
	body := "diff --git a/main.go b/main.go\n" +
		"index abc123..def456 100644\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -1,4 +1,5 @@\n" +
		" package main\n" +
		"-func old() {}\n" +
		"+func new() {}\n" +
		"+func extra() {}\n" +
		" \n" +
		" var x = 1"

	fd := parseFileHunks(body)

	if fd.OldPath != "main.go" || fd.NewPath != "main.go" {
		t.Fatalf("paths = %q/%q, want main.go/main.go", fd.OldPath, fd.NewPath)
	}
	if len(fd.Hunks) != 1 {
		t.Fatalf("got %d hunks, want 1", len(fd.Hunks))
	}
	h := fd.Hunks[0]
	if h.OldStart != 1 || h.OldLines != 4 || h.NewStart != 1 || h.NewLines != 5 {
		t.Errorf("hunk range = -%d,%d +%d,%d, want -1,4 +1,5", h.OldStart, h.OldLines, h.NewStart, h.NewLines)
	}
	if len(h.Lines) != 6 {
		t.Fatalf("got %d lines, want 6: %+v", len(h.Lines), h.Lines)
	}

	want := []HunkLine{
		{Kind: LineContext, Content: "package main", OldLine: 1, NewLine: 1},
		{Kind: LineRemoved, Content: "func old() {}", OldLine: 2},
		{Kind: LineAdded, Content: "func new() {}", NewLine: 2},
		{Kind: LineAdded, Content: "func extra() {}", NewLine: 3},
		{Kind: LineContext, Content: "", OldLine: 3, NewLine: 4},
		{Kind: LineContext, Content: "var x = 1", OldLine: 4, NewLine: 5},
	}
	for i, w := range want {
		if h.Lines[i] != w {
			t.Errorf("line[%d] = %+v, want %+v", i, h.Lines[i], w)
		}
	}
}

func TestParseFileHunks_NewFile(t *testing.T) {
	body := "diff --git a/new.go b/new.go\n" +
		"new file mode 100644\n" +
		"index 0000000..abc123\n" +
		"--- /dev/null\n" +
		"+++ b/new.go\n" +
		"@@ -0,0 +1,2 @@\n" +
		"+package foo\n" +
		"+var x = 1"

	fd := parseFileHunks(body)

	if fd.OldPath != "" {
		t.Errorf("OldPath = %q, want \"\" (file doesn't exist on old side)", fd.OldPath)
	}
	if fd.NewPath != "new.go" {
		t.Errorf("NewPath = %q, want new.go", fd.NewPath)
	}
	if len(fd.Hunks) != 1 {
		t.Fatalf("got %d hunks, want 1", len(fd.Hunks))
	}
	h := fd.Hunks[0]
	if h.OldStart != 0 || h.OldLines != 0 || h.NewStart != 1 || h.NewLines != 2 {
		t.Errorf("hunk range = -%d,%d +%d,%d, want -0,0 +1,2", h.OldStart, h.OldLines, h.NewStart, h.NewLines)
	}
	for _, l := range h.Lines {
		if l.Kind != LineAdded || l.OldLine != 0 {
			t.Errorf("expected every line to be LineAdded with OldLine=0, got %+v", l)
		}
	}
}

func TestParseFileHunks_DeletedFile(t *testing.T) {
	body := "diff --git a/old.go b/old.go\n" +
		"deleted file mode 100644\n" +
		"index abc123..0000000\n" +
		"--- a/old.go\n" +
		"+++ /dev/null\n" +
		"@@ -1,2 +0,0 @@\n" +
		"-package foo\n" +
		"-var x = 1"

	fd := parseFileHunks(body)

	if fd.OldPath != "old.go" {
		t.Errorf("OldPath = %q, want old.go", fd.OldPath)
	}
	if fd.NewPath != "" {
		t.Errorf("NewPath = %q, want \"\" (file doesn't exist on new side)", fd.NewPath)
	}
	if len(fd.Hunks) != 1 {
		t.Fatalf("got %d hunks, want 1", len(fd.Hunks))
	}
	for _, l := range fd.Hunks[0].Lines {
		if l.Kind != LineRemoved || l.NewLine != 0 {
			t.Errorf("expected every line to be LineRemoved with NewLine=0, got %+v", l)
		}
	}
}

func TestParseFileHunks_RenameWithContentChange(t *testing.T) {
	body := "diff --git a/old_name.go b/new_name.go\n" +
		"similarity index 95%\n" +
		"rename from old_name.go\n" +
		"rename to new_name.go\n" +
		"index abc123..def456 100644\n" +
		"--- a/old_name.go\n" +
		"+++ b/new_name.go\n" +
		"@@ -1,1 +1,1 @@\n" +
		"-foo\n" +
		"+bar"

	fd := parseFileHunks(body)

	if fd.OldPath != "old_name.go" || fd.NewPath != "new_name.go" {
		t.Fatalf("paths = %q/%q, want old_name.go/new_name.go", fd.OldPath, fd.NewPath)
	}
	if len(fd.Hunks) != 1 || len(fd.Hunks[0].Lines) != 2 {
		t.Fatalf("expected 1 hunk with 2 lines, got %+v", fd.Hunks)
	}
}

func TestParseFileHunks_PureRename_NoHunks(t *testing.T) {
	body := "diff --git a/old_name.go b/new_name.go\n" +
		"similarity index 100%\n" +
		"rename from old_name.go\n" +
		"rename to new_name.go"

	fd := parseFileHunks(body)

	if fd.OldPath != "old_name.go" || fd.NewPath != "new_name.go" {
		t.Fatalf("paths = %q/%q, want old_name.go/new_name.go", fd.OldPath, fd.NewPath)
	}
	if len(fd.Hunks) != 0 {
		t.Errorf("expected no hunks for a pure rename, got %d", len(fd.Hunks))
	}
}

func TestParseFileHunks_BinaryFile_NoHunksNoPanic(t *testing.T) {
	body := "diff --git a/image.png b/image.png\n" +
		"index abc123..def456 100644\n" +
		"Binary files a/image.png and b/image.png differ"

	fd := parseFileHunks(body)

	// Không có "--- "/"+++ " để đọc chính xác hơn — fallback về path lấy
	// từ dòng "diff --git" là kết quả tốt nhất có thể có.
	if fd.OldPath != "image.png" || fd.NewPath != "image.png" {
		t.Errorf("paths = %q/%q, want image.png/image.png", fd.OldPath, fd.NewPath)
	}
	if len(fd.Hunks) != 0 {
		t.Errorf("expected no hunks for a binary file diff, got %d", len(fd.Hunks))
	}
}

func TestParseFileHunks_MultipleHunks(t *testing.T) {
	body := "diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -1,2 +1,2 @@\n" +
		" package main\n" +
		"-var a = 1\n" +
		"+var a = 2\n" +
		"@@ -10,2 +10,2 @@\n" +
		" var b = 1\n" +
		"-var c = 1\n" +
		"+var c = 2"

	fd := parseFileHunks(body)

	if len(fd.Hunks) != 2 {
		t.Fatalf("got %d hunks, want 2", len(fd.Hunks))
	}
	if fd.Hunks[0].OldStart != 1 || fd.Hunks[1].OldStart != 10 {
		t.Errorf("hunk starts = %d, %d, want 1, 10", fd.Hunks[0].OldStart, fd.Hunks[1].OldStart)
	}
	// Số dòng ở hunk 2 phải tính lại từ NewStart/OldStart của chính nó
	// (10), không cộng dồn từ hunk 1.
	if fd.Hunks[1].Lines[0].OldLine != 10 || fd.Hunks[1].Lines[0].NewLine != 10 {
		t.Errorf("hunk 2 first line = old:%d new:%d, want old:10 new:10", fd.Hunks[1].Lines[0].OldLine, fd.Hunks[1].Lines[0].NewLine)
	}
}

func TestParseFileHunks_HunkHeaderWithoutCount_DefaultsToOne(t *testing.T) {
	// "@@ -5 +5 @@" (không có ",N") nghĩa là đúng 1 dòng thay đổi, theo spec
	// unified diff.
	body := "diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -5 +5 @@\n" +
		"-old\n" +
		"+new"

	fd := parseFileHunks(body)

	if len(fd.Hunks) != 1 {
		t.Fatalf("got %d hunks, want 1", len(fd.Hunks))
	}
	h := fd.Hunks[0]
	if h.OldLines != 1 || h.NewLines != 1 {
		t.Errorf("OldLines/NewLines = %d/%d, want 1/1", h.OldLines, h.NewLines)
	}
	if len(h.Lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(h.Lines))
	}
}

func TestParseFileHunks_NoNewlineAtEndOfFileMarker_DoesNotBreakCounting(t *testing.T) {
	body := "diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -1,1 +1,1 @@\n" +
		"-old\n" +
		"\\ No newline at end of file\n" +
		"+new\n" +
		"\\ No newline at end of file"

	fd := parseFileHunks(body)

	if len(fd.Hunks) != 1 {
		t.Fatalf("got %d hunks, want 1", len(fd.Hunks))
	}
	h := fd.Hunks[0]
	if len(h.Lines) != 2 {
		t.Fatalf("got %d content lines (want marker lines excluded), got %+v", len(h.Lines), h.Lines)
	}
	if h.Lines[0].Kind != LineRemoved || h.Lines[1].Kind != LineAdded {
		t.Errorf("unexpected line kinds: %+v", h.Lines)
	}
}

func TestParseFileHunks_GarbageWithoutHeader_ReturnsZeroValue(t *testing.T) {
	fd := parseFileHunks("just some text\nno header here")

	if fd.OldPath != "" || fd.NewPath != "" || len(fd.Hunks) != 0 {
		t.Errorf("expected zero-value FileDiff, got %+v", fd)
	}
}

func TestParseFileHunks_Empty(t *testing.T) {
	fd := parseFileHunks("")

	if fd.OldPath != "" || fd.NewPath != "" || len(fd.Hunks) != 0 {
		t.Errorf("expected zero-value FileDiff, got %+v", fd)
	}
}

func TestParseFileHunks_TruncatedMidHunk_NoPanicStopsCleanly(t *testing.T) {
	// Mô phỏng truncateDiff cắt ngay giữa 1 hunk — dòng cuối không còn khớp
	// prefix +/-/space nào (là phần của truncationNotice).
	body := "diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -1,5 +1,5 @@\n" +
		" line1\n" +
		"-line2\n" +
		truncationNotice

	fd := parseFileHunks(body)

	if len(fd.Hunks) != 1 {
		t.Fatalf("got %d hunks, want 1 (partial)", len(fd.Hunks))
	}
	h := fd.Hunks[0]
	if len(h.Lines) != 2 {
		t.Fatalf("expected 2 lines parsed before the cut, got %d: %+v", len(h.Lines), h.Lines)
	}
	if h.Lines[0].Kind != LineContext || h.Lines[1].Kind != LineRemoved {
		t.Errorf("unexpected line kinds before cut: %+v", h.Lines)
	}
}

func TestParseFileHunks_MalformedHunkHeader_SkippedSafely(t *testing.T) {
	body := "diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ this is not a valid header @@\n" +
		"@@ -1,1 +1,1 @@\n" +
		"-old\n" +
		"+new"

	fd := parseFileHunks(body)

	// Header lỗi bị bỏ qua, hunk hợp lệ ngay sau đó vẫn parse được bình
	// thường — không panic, không làm hỏng cả file.
	if len(fd.Hunks) != 1 {
		t.Fatalf("got %d hunks, want 1 (malformed header skipped)", len(fd.Hunks))
	}
}

func TestFileDiff_LineAtNew(t *testing.T) {
	body := "diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -1,2 +1,3 @@\n" +
		" package main\n" +
		"+import \"fmt\"\n" +
		" var x = 1"

	fd := parseFileHunks(body)

	l, ok := fd.LineAtNew(2)
	if !ok || l.Content != "import \"fmt\"" || l.Kind != LineAdded {
		t.Errorf("LineAtNew(2) = %+v, ok=%v, want the added import line", l, ok)
	}

	l, ok = fd.LineAtNew(1)
	if !ok || l.Content != "package main" || l.Kind != LineContext {
		t.Errorf("LineAtNew(1) = %+v, ok=%v, want the context line", l, ok)
	}

	if _, ok := fd.LineAtNew(999); ok {
		t.Error("LineAtNew(999) should not be found (outside any hunk)")
	}
}

func TestFileDiff_LineAtOld(t *testing.T) {
	body := "diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -1,2 +1,1 @@\n" +
		"-var a = 1\n" +
		" var b = 1"

	fd := parseFileHunks(body)

	l, ok := fd.LineAtOld(1)
	if !ok || l.Content != "var a = 1" || l.Kind != LineRemoved {
		t.Errorf("LineAtOld(1) = %+v, ok=%v, want the removed line", l, ok)
	}

	// Dòng removed (NewLine=0) không được khớp bởi LineAtNew.
	if _, ok := fd.LineAtNew(0); ok {
		t.Error("LineAtNew(0) should never match (0 is not a valid line number)")
	}
}
