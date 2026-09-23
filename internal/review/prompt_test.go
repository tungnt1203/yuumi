package review

import (
	"strings"
	"testing"
)

func TestBuildReviewPrompt(t *testing.T) {
	tests := []struct {
		name             string
		userCommand      string
		diff             string
		staticCheckNote  string
		repoInstructions string
		wantContain      []string
		wantAbsent       []string
	}{
		{
			name:        "has diff",
			userCommand: "review kỹ phần error handling",
			diff:        "diff --git a/main.go b/main.go\n+fmt.Println(\"hi\")",
			wantContain: []string{
				"review kỹ phần error handling",
				"diff --git a/main.go b/main.go",
				"```diff",
			},
			wantAbsent: []string{
				"Không lấy được diff thật",
			},
		},
		{
			name:        "empty diff falls back",
			userCommand: "review",
			diff:        "",
			wantContain: []string{
				"review",
				"Không lấy được diff thật",
			},
			wantAbsent: []string{
				"```diff",
			},
		},
		{
			name:        "blank diff (whitespace only) falls back",
			userCommand: "review",
			diff:        "   \n  ",
			wantContain: []string{
				"Không lấy được diff thật",
			},
		},
		{
			name:            "static check note included when present",
			userCommand:     "review",
			diff:            "diff --git a/main.go b/main.go\n+fmt.Println(\"hi\")",
			staticCheckNote: "gofmt (file chưa format đúng chuẩn):\nmain.go",
			wantContain: []string{
				"gofmt (file chưa format đúng chuẩn):\nmain.go",
			},
		},
		{
			name:            "no static check section when note is blank",
			userCommand:     "review",
			diff:            "diff --git a/main.go b/main.go\n+fmt.Println(\"hi\")",
			staticCheckNote: "   ",
			wantAbsent: []string{
				"gofmt",
				"go vet",
			},
		},
		{
			name:             "repo instructions included when present",
			userCommand:      "review",
			diff:             "diff --git a/main.go b/main.go\n+fmt.Println(\"hi\")",
			repoInstructions: "Luôn yêu cầu unit test cho hàm export.",
			wantContain: []string{
				"Luôn yêu cầu unit test cho hàm export.",
				".yuumi.yml",
			},
		},
		{
			name:             "no repo instructions section when blank",
			userCommand:      "review",
			diff:             "diff --git a/main.go b/main.go\n+fmt.Println(\"hi\")",
			repoInstructions: "   ",
			wantAbsent: []string{
				".yuumi.yml",
			},
		},
		{
			name:        "language default rules included for a recognized file type",
			userCommand: "review",
			diff:        "diff --git a/main.go b/main.go\n+fmt.Println(\"hi\")",
			wantContain: []string{
				"Race condition",
			},
		},
		{
			name:        "no language rules section for an unrecognized file type",
			userCommand: "review",
			diff:        "diff --git a/README.md b/README.md\n+# hi",
			wantAbsent: []string{
				"Race condition",
				"Mutable default argument",
			},
		},
		{
			name:        "changed symbols listed and grep instruction included",
			userCommand: "review",
			diff:        "diff --git a/main.go b/main.go\n+doSomething(x)",
			wantContain: []string{
				"doSomething",
				"grep",
			},
		},
		{
			name:        "no changed symbols section when diff has no code-like content",
			userCommand: "review",
			diff:        "diff --git a/README.md b/README.md\n+Hello world, this is just prose.",
			wantAbsent: []string{
				"grep",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildReviewPrompt(tt.userCommand, tt.diff, tt.staticCheckNote, tt.repoInstructions, "")

			for _, want := range tt.wantContain {
				if !strings.Contains(got, want) {
					t.Errorf("BuildReviewPrompt() missing %q in output:\n%s", want, got)
				}
			}
			for _, notWant := range tt.wantAbsent {
				if strings.Contains(got, notWant) {
					t.Errorf("BuildReviewPrompt() unexpectedly contains %q in output:\n%s", notWant, got)
				}
			}
		})
	}
}

// TestBuildReviewPrompt_LanguageRules_PlacedAfterRepoInstructions_WithPriorityNote
// đảm bảo khi cả repoInstructions lẫn rule mặc định theo loại file cùng có
// mặt, rule mặc định đứng SAU và có ghi chú rõ ưu tiên thấp hơn — đúng thứ
// tự ưu tiên issue #25 acceptance criteria yêu cầu (rule repo > rule mặc
// định bot tự có).
func TestBuildReviewPrompt_LanguageRules_PlacedAfterRepoInstructions_WithPriorityNote(t *testing.T) {
	got := BuildReviewPrompt("review", "diff --git a/main.go b/main.go\n+fmt.Println(1)", "", "Luôn yêu cầu unit test.", "")

	repoIdx := strings.Index(got, "Luôn yêu cầu unit test.")
	rulesIdx := strings.Index(got, "Race condition")
	if repoIdx == -1 || rulesIdx == -1 {
		t.Fatalf("expected both repo instructions and language rules present, got:\n%s", got)
	}
	if repoIdx > rulesIdx {
		t.Errorf("expected repo instructions to appear BEFORE default language rules, got:\n%s", got)
	}
	if !strings.Contains(got, "ưu tiên hơn") {
		t.Errorf("expected an explicit note that repo instructions take priority, got:\n%s", got)
	}
}

// TestBuildReviewPrompt_IncludesResultFormatInstructions đảm bảo prompt yêu
// cầu rõ ràng format JSON output có ví dụ cụ thể (file/line/category/
// severity/message/suggestion) — không phụ thuộc input nào, luôn phải có
// (xem resultFormatInstructions, issue #26 + #5).
func TestBuildReviewPrompt_IncludesResultFormatInstructions(t *testing.T) {
	got := BuildReviewPrompt("review", "diff --git a/x b/x\n+y", "", "", "")

	for _, want := range []string{
		"JSON array",
		`"file"`,
		`"line"`,
		`"category"`,
		`"severity"`,
		`"message"`,
		`"suggestion"`,
		`"end_line"`,
		"critical|high|medium|low",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("BuildReviewPrompt() missing %q in result format instructions:\n%s", want, got)
		}
	}
}

func TestBuildFormatRepairPrompt_IncludesPreviousOutput(t *testing.T) {
	got := buildFormatRepairPrompt("bug ở dòng 60")

	if !isFormatRepairPrompt(got) {
		t.Fatalf("buildFormatRepairPrompt() = %q, want the format-repair prefix", got)
	}
	if !strings.Contains(got, "bug ở dòng 60") {
		t.Errorf("repair prompt missing the previous output:\n%s", got)
	}
	if !strings.Contains(got, `"line":0`) || !strings.Contains(got, `"end_line":0`) {
		t.Errorf("repair prompt must keep numeric line/end_line example:\n%s", got)
	}
	if strings.Contains(got, "```diff") || strings.Contains(got, "diff --git") {
		t.Errorf("repair prompt should not resend a diff:\n%s", got)
	}
	prev := strings.Index(got, "bug ở dòng 60")
	again := strings.LastIndex(got, "Nhắc lại")
	if prev == -1 || again < prev {
		t.Errorf("repair prompt should repeat the JSON-only reminder after the previous output:\n%s", got)
	}
}

// TestBuildReviewPrompt_Primer_ReplacesGenericReadMoreHint đảm bảo khi có
// primer (PR bị chia bundle, issue #18), prompt nhúng đúng nội dung primer
// và KHÔNG còn dặn chung chung "hãy tự đọc thêm" nữa — primer đã thay thế
// đúng vai trò đó bằng ngữ cảnh cụ thể (buildPrimer).
func TestBuildReviewPrompt_Primer_ReplacesGenericReadMoreHint(t *testing.T) {
	primer := "Toàn bộ 2 file bị thay đổi trong PR:\n- a.go\n- b.go\n\n"

	got := BuildReviewPrompt("review", "diff --git a/a.go b/a.go\n+x", "", "", primer)

	if !strings.Contains(got, primer) {
		t.Fatalf("expected prompt to contain the primer verbatim, got:\n%s", got)
	}
	if strings.Contains(got, "Trước khi kết luận, hãy đọc thêm") {
		t.Errorf("expected the generic \"read more\" hint to be replaced by the primer, got:\n%s", got)
	}
	if idx := strings.Index(got, primer); idx > strings.Index(got, "Yêu cầu từ người review") {
		t.Errorf("expected primer to appear at the start of the prompt, before the reviewer's command, got:\n%s", got)
	}
}

// TestBuildReviewPrompt_NoPrimer_KeepsGenericReadMoreHint đảm bảo hành vi cũ
// (PR không bị chia bundle, primer rỗng) không đổi.
func TestBuildReviewPrompt_NoPrimer_KeepsGenericReadMoreHint(t *testing.T) {
	got := BuildReviewPrompt("review", "diff --git a/a.go b/a.go\n+x", "", "", "")

	if !strings.Contains(got, "Trước khi kết luận, hãy đọc thêm") {
		t.Errorf("expected the generic \"read more\" hint when there is no primer, got:\n%s", got)
	}
}

func TestBundleNote(t *testing.T) {
	got := bundleNote(2, 5)

	for _, want := range []string{"2/5", "chia làm 5 phần"} {
		if !strings.Contains(got, want) {
			t.Errorf("bundleNote(2, 5) missing %q in output: %q", want, got)
		}
	}
}
