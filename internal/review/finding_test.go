package review

import (
	"strings"
	"testing"
)

func TestParseFindings_ValidArray(t *testing.T) {
	text := `[{"category":"bug","severity":"high","message":"nil pointer khi x == nil","suggestion":"if x != nil { ... }"}]`

	findings, ok := parseFindings(text)
	if !ok {
		t.Fatal("parseFindings() ok = false, want true")
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}
	want := Finding{Category: "bug", Severity: "high", Message: "nil pointer khi x == nil", Suggestion: "if x != nil { ... }"}
	if findings[0] != want {
		t.Errorf("findings[0] = %+v, want %+v", findings[0], want)
	}
}

func TestParseFindings_EmptyArray_OkButNoFindings(t *testing.T) {
	findings, ok := parseFindings("[]")
	if !ok {
		t.Fatal("parseFindings(\"[]\") ok = false, want true")
	}
	if len(findings) != 0 {
		t.Errorf("got %d findings, want 0", len(findings))
	}
}

func TestParseFindings_WrappedInProseAndCodeFence(t *testing.T) {
	// Claude đôi khi vẫn thêm câu mở đầu hoặc bọc ```json ... ``` dù đã dặn
	// không làm vậy — extractJSONArray phải chịu được.
	text := "Đây là kết quả review:\n```json\n" +
		`[{"category":"style","severity":"low","message":"đặt tên biến chưa rõ nghĩa"}]` +
		"\n```\nHết."

	findings, ok := parseFindings(text)
	if !ok {
		t.Fatal("parseFindings() ok = false, want true")
	}
	if len(findings) != 1 || findings[0].Category != "style" {
		t.Errorf("findings = %+v", findings)
	}
}

func TestParseFindings_FreeformText_NotOk(t *testing.T) {
	if _, ok := parseFindings("Code trông ổn, không có vấn đề gì."); ok {
		t.Error("parseFindings() ok = true for freeform text without any [], want false")
	}
}

func TestParseFindings_InvalidJSON_NotOk(t *testing.T) {
	if _, ok := parseFindings("[{\"category\": broken json"); ok {
		t.Error("parseFindings() ok = true for invalid JSON, want false")
	}
}

func TestParseFindings_JSONObjectNotArray_NotOk(t *testing.T) {
	// Có "[" và "]" trong text nhưng top-level không phải array — vd Claude
	// trả 1 object đơn lẻ thay vì mảng.
	if _, ok := parseFindings(`{"note": "see [1] and [2] for details"}`); ok {
		t.Error("parseFindings() ok = true for a non-array JSON value, want false")
	}
}

func TestRenderBundleSummary_NoFindings(t *testing.T) {
	got := renderBundleSummary(nil, nil)
	if !strings.Contains(got, "Không có vấn đề") {
		t.Errorf("renderBundleSummary(nil, nil) = %q, want a no-issues message", got)
	}
}

func TestRenderBundleSummary_AllWentInline_NotesCountNoDuplicateText(t *testing.T) {
	findings := []Finding{
		{File: "main.go", Line: 1, Severity: "high", Message: "inline finding"},
	}
	got := renderBundleSummary(findings, nil) // general rỗng: tất cả đã đi inline

	if !strings.Contains(got, "1") || !strings.Contains(got, "gắn trực tiếp") {
		t.Errorf("renderBundleSummary() = %q, want a note mentioning 1 inline finding", got)
	}
	if strings.Contains(got, "inline finding") {
		t.Errorf("renderBundleSummary() should not repeat the finding's own text when it already went inline, got: %q", got)
	}
}

func TestRenderBundleSummary_MixedInlineAndGeneral(t *testing.T) {
	findings := []Finding{
		{File: "main.go", Line: 1, Severity: "high", Message: "inline finding"},
		{Severity: "low", Message: "general finding"},
	}
	general := []Finding{findings[1]}

	got := renderBundleSummary(findings, general)

	if !strings.Contains(got, "1") || !strings.Contains(got, "gắn trực tiếp") {
		t.Errorf("renderBundleSummary() missing inline-count note, got: %q", got)
	}
	if !strings.Contains(got, "general finding") {
		t.Errorf("renderBundleSummary() missing general finding text, got: %q", got)
	}
}

func TestRenderBundleSummary_NoneWentInline_SameAsRenderFindings(t *testing.T) {
	findings := []Finding{
		{Severity: "low", Message: "general only"},
	}

	got := renderBundleSummary(findings, findings)

	if got != renderFindings(findings) {
		t.Errorf("renderBundleSummary() = %q, want it to equal renderFindings() when nothing went inline", got)
	}
}

func TestRenderFindings_Empty(t *testing.T) {
	got := renderFindings(nil)
	if !strings.Contains(got, "Không có vấn đề") {
		t.Errorf("renderFindings(nil) = %q, want a no-issues message", got)
	}
}

func TestRenderFindings_SortsBySeverity_CriticalFirst(t *testing.T) {
	findings := []Finding{
		{Severity: "low", Message: "low issue"},
		{Severity: "critical", Message: "critical issue"},
		{Severity: "medium", Message: "medium issue"},
	}

	got := renderFindings(findings)

	iCritical := strings.Index(got, "critical issue")
	iMedium := strings.Index(got, "medium issue")
	iLow := strings.Index(got, "low issue")
	if !(iCritical < iMedium && iMedium < iLow) {
		t.Errorf("expected order critical < medium < low, got positions %d, %d, %d:\n%s", iCritical, iMedium, iLow, got)
	}
}

func TestRenderFindings_StableOrderWithinSameSeverity(t *testing.T) {
	findings := []Finding{
		{Severity: "high", Message: "first"},
		{Severity: "high", Message: "second"},
	}

	got := renderFindings(findings)

	if strings.Index(got, "first") > strings.Index(got, "second") {
		t.Errorf("expected original order preserved within same severity, got:\n%s", got)
	}
}

func TestRenderFinding_IncludesIconLabelCategoryMessage(t *testing.T) {
	f := Finding{Category: "security", Severity: "critical", Message: "SQL injection ở query build bằng string concat"}

	got := renderFinding(f)

	for _, want := range []string{"🔴", "CRITICAL", "`security`", "SQL injection ở query build bằng string concat"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderFinding() missing %q, got: %q", want, got)
		}
	}
}

func TestRenderFinding_WithSuggestion_IncludesCodeBlock(t *testing.T) {
	f := Finding{Severity: "medium", Message: "thiếu error wrapping", Suggestion: `fmt.Errorf("do X: %w", err)`}

	got := renderFinding(f)

	if !strings.Contains(got, "Gợi ý sửa") || !strings.Contains(got, "```\nfmt.Errorf(\"do X: %w\", err)\n```") {
		t.Errorf("renderFinding() missing suggestion code block, got: %q", got)
	}
}

func TestRenderFinding_NoSuggestion_NoCodeBlock(t *testing.T) {
	f := Finding{Severity: "low", Message: "chỉ là góp ý style"}

	got := renderFinding(f)

	if strings.Contains(got, "Gợi ý sửa") || strings.Contains(got, "```") {
		t.Errorf("renderFinding() should not include a suggestion block when Suggestion is empty, got: %q", got)
	}
}

func TestRenderFinding_UnknownSeverity_FallbackIconAndUppercaseLabel(t *testing.T) {
	f := Finding{Severity: "weird", Message: "m"}

	got := renderFinding(f)

	if !strings.Contains(got, "⚪") || !strings.Contains(got, "WEIRD") {
		t.Errorf("renderFinding() with unknown severity = %q, want fallback icon and uppercased label", got)
	}
}

func TestRenderFinding_EmptySeverity_LabelIsNA(t *testing.T) {
	f := Finding{Message: "m"}

	got := renderFinding(f)

	if !strings.Contains(got, "N/A") {
		t.Errorf("renderFinding() with empty severity = %q, want N/A label", got)
	}
}

func TestRenderFinding_EmptyCategory_NoCategoryTag(t *testing.T) {
	f := Finding{Severity: "high", Message: "m"}

	got := renderFinding(f)

	if strings.Contains(got, "``") {
		t.Errorf("renderFinding() with empty category should not render a category tag, got: %q", got)
	}
}

func TestSeverityRankOf_UnknownGoesLast(t *testing.T) {
	if severityRankOf("unknown") <= severityRankOf("low") {
		t.Error("severityRankOf(\"unknown\") should rank after every known severity")
	}
	if severityRankOf("CRITICAL") != severityRankOf("critical") {
		t.Error("severityRankOf should be case-insensitive")
	}
}
