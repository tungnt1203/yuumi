package review

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestExtractChangedSymbols_GoFunctionCall(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -1,1 +1,1 @@\n" +
		"-oldFunc(x)\n" +
		"+newFunc(x)"

	got := extractChangedSymbols(diff)

	want := []string{"oldFunc", "newFunc"}
	if !slices.Equal(got, want) {
		t.Errorf("extractChangedSymbols() = %v, want %v", got, want)
	}
}

func TestExtractChangedSymbols_GoMethodWithReceiver(t *testing.T) {
	diff := "diff --git a/client.go b/client.go\n" +
		"+func (c *Client) DoThing(ctx context.Context) error {"

	got := extractChangedSymbols(diff)

	if !slices.Contains(got, "DoThing") {
		t.Errorf("extractChangedSymbols() = %v, want it to include %q", got, "DoThing")
	}
}

func TestExtractChangedSymbols_GoTypeDecl(t *testing.T) {
	diff := "diff --git a/types.go b/types.go\n" +
		"+type Reviewer interface {\n" +
		"+type Job struct {"

	got := extractChangedSymbols(diff)

	for _, want := range []string{"Reviewer", "Job"} {
		if !slices.Contains(got, want) {
			t.Errorf("extractChangedSymbols() = %v, want it to include %q", got, want)
		}
	}
}

func TestExtractChangedSymbols_IgnoresContextAndUnchangedLines(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n" +
		" unchangedCall(x)\n" + // context line (leading space), not +/-
		"+addedCall(x)"

	got := extractChangedSymbols(diff)

	if slices.Contains(got, "unchangedCall") {
		t.Errorf("extractChangedSymbols() = %v, should NOT include symbols from context lines", got)
	}
	if !slices.Contains(got, "addedCall") {
		t.Errorf("extractChangedSymbols() = %v, want it to include %q", got, "addedCall")
	}
}

func TestExtractChangedSymbols_IgnoresDiffHeaderLines(t *testing.T) {
	// "+++ b/main.go" / "--- a/main.go" bắt đầu bằng +/- nhưng là header
	// path, không phải nội dung code — không được coi "main" là symbol.
	diff := "diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -1,1 +1,1 @@\n" +
		"-x()\n" +
		"+y()"

	got := extractChangedSymbols(diff)

	want := []string{"x", "y"}
	if !slices.Equal(got, want) {
		t.Errorf("extractChangedSymbols() = %v, want %v (header lines excluded)", got, want)
	}
}

func TestExtractChangedSymbols_ExcludesCommonKeywords(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n" +
		"+if (x) {\n" +
		"+for (i = 0; i < 10; i++) {\n" +
		"+realCall(x)"

	got := extractChangedSymbols(diff)

	for _, kw := range []string{"if", "for"} {
		if slices.Contains(got, kw) {
			t.Errorf("extractChangedSymbols() = %v, should exclude keyword %q", got, kw)
		}
	}
	if !slices.Contains(got, "realCall") {
		t.Errorf("extractChangedSymbols() = %v, want it to include %q", got, "realCall")
	}
}

func TestExtractChangedSymbols_Deduplicates(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n" +
		"-foo(1)\n" +
		"+foo(2)\n" +
		"+foo(3)"

	got := extractChangedSymbols(diff)

	want := []string{"foo"}
	if !slices.Equal(got, want) {
		t.Errorf("extractChangedSymbols() = %v, want %v (deduplicated)", got, want)
	}
}

func TestExtractChangedSymbols_EmptyDiff(t *testing.T) {
	if got := extractChangedSymbols(""); len(got) != 0 {
		t.Errorf("extractChangedSymbols(\"\") = %v, want empty", got)
	}
}

func TestExtractChangedSymbols_NoCodeLikeContent(t *testing.T) {
	diff := "diff --git a/README.md b/README.md\n" +
		"+Hello world, this is just prose."

	got := extractChangedSymbols(diff)
	if len(got) != 0 {
		t.Errorf("extractChangedSymbols() = %v, want empty for prose without any identifier(", got)
	}
}

func TestChangedSymbolsNote_Empty_ReturnsEmpty(t *testing.T) {
	if got := changedSymbolsNote(""); got != "" {
		t.Errorf("changedSymbolsNote(\"\") = %q, want empty", got)
	}
}

func TestChangedSymbolsNote_IncludesSymbolsAndInstruction(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n+doSomething(x)"

	got := changedSymbolsNote(diff)

	if !strings.Contains(got, "doSomething") {
		t.Errorf("changedSymbolsNote() = %q, want it to list the symbol", got)
	}
	if !strings.Contains(got, "grep") {
		t.Errorf("changedSymbolsNote() = %q, want it to instruct grepping for usages", got)
	}
}

func TestChangedSymbolsNote_CapsLongList(t *testing.T) {
	var b strings.Builder
	b.WriteString("diff --git a/main.go b/main.go\n")
	for i := range maxChangedSymbols + 5 {
		fmt.Fprintf(&b, "+sym%d()\n", i)
	}

	got := changedSymbolsNote(b.String())

	// Đếm riêng "symN" (tên symbol thật) — không dùng strings.Count(got,
	// "sym") vì câu ghi chú "... symbol khác)" cũng chứa substring "sym",
	// đếm lẫn sẽ sai.
	symbolMentions := regexp.MustCompile(`\bsym\d+\b`).FindAllString(got, -1)
	if len(symbolMentions) != maxChangedSymbols {
		t.Errorf("changedSymbolsNote() should cap the shown list at %d symbols, got %d: %q", maxChangedSymbols, len(symbolMentions), got)
	}
	if !strings.Contains(got, "5") {
		t.Errorf("changedSymbolsNote() should mention the remaining 5 symbols not shown, got: %q", got)
	}
}
