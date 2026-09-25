package review

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestParseFindings_StructuredOutputObject(t *testing.T) {
	text := `{"findings":[{"category":"bug","severity":"high","message":"nil pointer khi x == nil","suggestion":"if x != nil { ... }"}]}`

	findings, dropped, ok := parseFindings(text)
	if !ok || dropped != 0 {
		t.Fatalf("parseFindings() ok=%v dropped=%d, want ok and nothing dropped", ok, dropped)
	}
	want := Finding{Category: "bug", Severity: "high", Message: "nil pointer khi x == nil", Suggestion: "if x != nil { ... }"}
	if len(findings) != 1 || findings[0] != want {
		t.Errorf("findings = %+v, want [%+v]", findings, want)
	}
}

// Mảng trần là định dạng BundleCache lưu (saveCachedBundle).
func TestParseFindings_BareArray(t *testing.T) {
	findings, _, ok := parseFindings(`[{"category":"style","severity":"low","message":"đặt tên chưa rõ"}]`)
	if !ok || len(findings) != 1 || findings[0].Category != "style" {
		t.Errorf("parseFindings() = %+v ok=%v, want 1 style finding", findings, ok)
	}
}

func TestParseFindings_EmptyFindings_OkButNoFindings(t *testing.T) {
	for _, text := range []string{`{"findings":[]}`, "[]"} {
		findings, _, ok := parseFindings(text)
		if !ok || findings == nil || len(findings) != 0 {
			t.Errorf("parseFindings(%q) = %#v ok=%v, want empty non-nil and ok", text, findings, ok)
		}
	}
}

// 1 finding hỏng không được kéo cả kết quả xuống (issue #72).
func TestParseFindings_DropsOnlyInvalidItems(t *testing.T) {
	text := `{"findings":[
		{"category":"bug","severity":"high","message":"giữ lại"},
		{"category":"bug","severity":"high","line":"12","message":"line là chuỗi"},
		{"category":"bug","severity":"low","message":"  "}
	]}`

	findings, dropped, ok := parseFindings(text)
	if !ok {
		t.Fatal("parseFindings() ok = false, want true")
	}
	if dropped != 2 || len(findings) != 1 || findings[0].Message != "giữ lại" {
		t.Errorf("parseFindings() = %+v dropped=%d, want only the valid finding and 2 dropped", findings, dropped)
	}
}

func TestParseFindings_NotOk(t *testing.T) {
	for _, text := range []string{
		"",
		"Code trông ổn, không có vấn đề gì.",
		"[{\"category\": broken json",
		`{"note": "see [1] and [2]"}`,
		`{"findings": null}`,
		"Kết quả:\n```json\n[]\n```",
	} {
		if _, _, ok := parseFindings(text); ok {
			t.Errorf("parseFindings(%q) ok = true, want false", text)
		}
	}
}

// Schema gửi cho CLI phải là JSON hợp lệ và mọi field của nó phải map được
// vào Finding — đổi tên json tag mà quên sửa schema thì field đó im lặng
// thành rỗng.
func TestFindingsSchema_MatchesFinding(t *testing.T) {
	var schema struct {
		Properties struct {
			Findings struct {
				Items struct {
					Properties map[string]json.RawMessage `json:"properties"`
					Required   []string                   `json:"required"`
				} `json:"items"`
			} `json:"findings"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(FindingsSchema), &schema); err != nil {
		t.Fatalf("FindingsSchema is not valid JSON: %v", err)
	}

	tags := map[string]bool{}
	ft := reflect.TypeOf(Finding{})
	for i := 0; i < ft.NumField(); i++ {
		tags[strings.Split(ft.Field(i).Tag.Get("json"), ",")[0]] = true
	}

	props := schema.Properties.Findings.Items.Properties
	for name := range props {
		if !tags[name] {
			t.Errorf("schema property %q has no matching Finding json tag", name)
		}
	}
	for tag := range tags {
		if _, ok := props[tag]; !ok {
			t.Errorf("Finding field %q is missing from FindingsSchema", tag)
		}
	}
	for _, req := range schema.Properties.Findings.Items.Required {
		if _, ok := props[req]; !ok {
			t.Errorf("required field %q is not a schema property", req)
		}
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
	if strings.Contains(got, "```suggestion") {
		t.Errorf("renderFinding() should keep a plain code block for comments not attached to a diff line, got: %q", got)
	}
}

func TestFormatSuggestion_SingleLine(t *testing.T) {
	got, ok := formatSuggestion(`fmt.Errorf("do X: %w", err)`)
	want := "```suggestion\nfmt.Errorf(\"do X: %w\", err)\n```"
	if !ok || got != want {
		t.Errorf("formatSuggestion() = %q, %v, want %q, true", got, ok, want)
	}
}

func TestFormatSuggestion_MultiLine(t *testing.T) {
	got, ok := formatSuggestion("if err != nil {\n\treturn err\n}\n")
	want := "```suggestion\nif err != nil {\n\treturn err\n}\n```"
	if !ok || got != want {
		t.Errorf("formatSuggestion() = %q, %v, want %q, true", got, ok, want)
	}
}

func TestFormatSuggestion_Empty(t *testing.T) {
	for _, suggestion := range []string{"", "   ", "\n\t\n"} {
		if got, ok := formatSuggestion(suggestion); ok || got != "" {
			t.Errorf("formatSuggestion(%q) = %q, %v, want empty, false", suggestion, got, ok)
		}
	}
}

func TestFormatSuggestion_BodyContainsFence_RefusesSuggestionBlock(t *testing.T) {
	got, ok := formatSuggestion("before\n```\ncode\n```\nafter")
	if ok || got != "" {
		t.Errorf("formatSuggestion() = %q, %v, want empty, false when body contains a fence", got, ok)
	}
}

func TestFormatSuggestion_InfoStringDoesNotCloseFence(t *testing.T) {
	got, ok := formatSuggestion("ví dụ\n```python\nprint(1)")
	want := "```suggestion\nví dụ\n```python\nprint(1)\n```"
	if !ok || got != want {
		t.Errorf("formatSuggestion() = %q, %v, want %q, true", got, ok, want)
	}
}

func TestRenderInlineFinding_FenceInSuggestion_FallsBackToPlainBlock(t *testing.T) {
	f := Finding{Severity: "low", Message: "m", Suggestion: "giữ\n```\nkhối\n```"}

	got := renderInlineFinding(f)

	want := "**Gợi ý sửa:**\n````\ngiữ\n```\nkhối\n```\n````"
	if !strings.Contains(got, want) {
		t.Errorf("renderInlineFinding() = %q, want outer fence longer than the inner ``` so the block stays intact", got)
	}
	if strings.Contains(got, "```suggestion") {
		t.Errorf("renderInlineFinding() = %q, want a plain code block when suggestion contains a closing fence", got)
	}
}

func TestRenderInlineFinding_UsesSuggestionFence(t *testing.T) {
	f := Finding{Severity: "medium", Message: "thiếu error wrapping", Suggestion: `fmt.Errorf("do X: %w", err)`}

	got := renderInlineFinding(f)

	if !strings.Contains(got, "```suggestion\nfmt.Errorf(\"do X: %w\", err)\n```") {
		t.Errorf("renderInlineFinding() missing suggestion fence, got: %q", got)
	}
	if strings.Contains(got, "Gợi ý sửa") {
		t.Errorf("renderInlineFinding() should not add the plain-code-block label, got: %q", got)
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

func TestRenderFindings_GroupsByFileIntoDetailsBlocks(t *testing.T) {
	findings := []Finding{
		{File: "a.go", Severity: "high", Message: "issue in a"},
		{File: "b.go", Severity: "low", Message: "issue in b"},
		{Severity: "medium", Message: "general issue"},
	}

	got := renderFindings(findings)

	for _, want := range []string{
		"<summary>📄 `a.go` (1)</summary>",
		"<summary>📄 `b.go` (1)</summary>",
		"<summary>📝 Nhận xét chung (1)</summary>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("renderFindings() missing %q, got: %q", want, got)
		}
	}

	// Nhóm "Nhận xét chung" phải nằm SAU 2 nhóm file, dù finding của nó xuất
	// hiện ở giữa input (xem groupFindingsByFile).
	iA := strings.Index(got, "a.go")
	iGeneral := strings.Index(got, "Nhận xét chung")
	iB := strings.Index(got, "b.go")
	if !(iA < iGeneral && iB < iGeneral) {
		t.Errorf("expected \"Nhận xét chung\" group last, got order in: %s", got)
	}
}

func TestGroupFindingsByFile_PreservesFirstSeenFileOrder(t *testing.T) {
	findings := []Finding{
		{File: "z.go", Message: "1"},
		{File: "a.go", Message: "2"},
		{File: "z.go", Message: "3"},
	}

	order, groups := groupFindingsByFile(findings)

	if len(order) != 2 || order[0] != "z.go" || order[1] != "a.go" {
		t.Errorf("groupFindingsByFile() order = %v, want [z.go a.go]", order)
	}
	if len(groups["z.go"]) != 2 {
		t.Errorf("groups[\"z.go\"] = %v, want 2 findings", groups["z.go"])
	}
}

func TestRenderReviewHeader_Empty_ShowsNoIssuesMessage(t *testing.T) {
	got := renderReviewHeader(nil, false)

	if !strings.Contains(got, "## 🟣 Yuumi Review") || !strings.Contains(got, "Không phát hiện vấn đề") {
		t.Errorf("renderReviewHeader(nil, false) = %q, want title + no-issues message", got)
	}
}

func TestRenderReviewHeader_CountsBySeverity(t *testing.T) {
	findings := []Finding{
		{Severity: "critical", Message: "1"},
		{Severity: "critical", Message: "2"},
		{Severity: "low", Message: "3"},
	}

	got := renderReviewHeader(findings, false)

	if !strings.Contains(got, "| 🔴 CRITICAL | 2 |") {
		t.Errorf("renderReviewHeader() missing critical count row, got: %q", got)
	}
	if !strings.Contains(got, "| 🔵 LOW | 1 |") {
		t.Errorf("renderReviewHeader() missing low count row, got: %q", got)
	}
	if strings.Contains(got, "HIGH") || strings.Contains(got, "MEDIUM") {
		t.Errorf("renderReviewHeader() should omit severities with 0 count, got: %q", got)
	}
	if !strings.Contains(got, "Tổng: 3 góp ý") {
		t.Errorf("renderReviewHeader() missing total count, got: %q", got)
	}
}

func TestRenderReviewHeader_UnknownSeverity_CountedAsKhac(t *testing.T) {
	findings := []Finding{{Severity: "weird", Message: "m"}}

	got := renderReviewHeader(findings, false)

	if !strings.Contains(got, "| ⚪ Khác | 1 |") {
		t.Errorf("renderReviewHeader() should count unknown severity under \"Khác\", got: %q", got)
	}
	if !strings.Contains(got, "Tổng: 1 góp ý") {
		t.Errorf("renderReviewHeader() missing total count, got: %q", got)
	}
}

func TestRenderReviewHeader_Partial_WarnsCountIsIncomplete(t *testing.T) {
	findings := []Finding{{Severity: "high", Message: "m"}}

	got := renderReviewHeader(findings, true)

	warn := strings.Index(got, "Đã cấu trúc được 1 góp ý")
	table := strings.Index(got, "| Mức độ |")
	if warn == -1 || table == -1 || warn > table {
		t.Errorf("renderReviewHeader(partial=true) should warn before the table, got: %q", got)
	}
	if !strings.Contains(got, "Tổng: 1 góp ý") {
		t.Errorf("renderReviewHeader(partial=true) should still show the count it does have, got: %q", got)
	}
	if strings.Contains(got, "Không phát hiện vấn đề") {
		t.Errorf("renderReviewHeader(partial=true) should not claim the PR is clean, got: %q", got)
	}
}

func TestRenderReviewHeader_PartialEmpty_DoesNotClaimClean(t *testing.T) {
	got := renderReviewHeader(nil, true)

	if strings.Contains(got, "Không phát hiện vấn đề") {
		t.Errorf("renderReviewHeader(nil, true) claimed the PR is clean, got: %q", got)
	}
	if !strings.Contains(got, "Review chưa đủ để kết luận") {
		t.Errorf("renderReviewHeader(nil, true) = %q, want an inconclusive warning", got)
	}
}

func TestRenderReviewHeader_NotPartial_NoWarning(t *testing.T) {
	findings := []Finding{{Severity: "high", Message: "m"}}

	got := renderReviewHeader(findings, false)

	if strings.Contains(got, "⚠️") || strings.Contains(got, "chưa đếm") {
		t.Errorf("renderReviewHeader(partial=false) should not show the incomplete-count warning, got: %q", got)
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
