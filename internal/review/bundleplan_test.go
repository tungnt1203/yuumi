package review

import (
	"strings"
	"testing"
)

func TestBuildBundlePlan_SmallDiff_OnePromptWithoutNoteOrPrimer(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n+x"

	plan := BuildBundlePlan(t.TempDir(), "review", diff, 0, nil, "", "")

	if len(plan.Prompts) != 1 {
		t.Fatalf("got %d prompts, want 1", len(plan.Prompts))
	}
	if strings.Contains(plan.Prompts[0], "Lưu ý: PR này khá lớn") || strings.Contains(plan.Prompts[0], "Toàn bộ") {
		t.Errorf("a single bundle should have no bundle note or primer:\n%s", plan.Prompts[0])
	}
}

func TestBuildBundlePlan_OverBudget_SplitsWithNoteAndPrimer(t *testing.T) {
	fileA := "diff --git a/x/a.go b/x/a.go\n+" + strings.Repeat("a", 30)
	fileB := "diff --git a/y/b.go b/y/b.go\n+" + strings.Repeat("b", 30)

	plan := BuildBundlePlan(t.TempDir(), "review", fileA+"\n"+fileB, len(fileA)+1, nil, "", "")

	if len(plan.Prompts) != 2 {
		t.Fatalf("got %d prompts, want 2", len(plan.Prompts))
	}
	for i, p := range plan.Prompts {
		if !strings.Contains(p, bundleNote(i+1, 2)) {
			t.Errorf("prompt %d missing its bundle note", i+1)
		}
		// Primer liệt kê mọi file của PR, kể cả file ở bundle khác.
		if !strings.Contains(p, "x/a.go") || !strings.Contains(p, "y/b.go") {
			t.Errorf("prompt %d should list every changed file via the primer", i+1)
		}
	}
}

// Mọi file đều bị lọc: không có gì để review, không tốn lần gọi nào.
func TestBuildBundlePlan_AllFilesSkipped_NoPrompts(t *testing.T) {
	plan := BuildBundlePlan(t.TempDir(), "review", "diff --git a/go.sum b/go.sum\n+h1:abc", 0, nil, "", "")

	if len(plan.Prompts) != 0 {
		t.Errorf("got %d prompts, want 0", len(plan.Prompts))
	}
	if len(plan.Skipped) != 1 || plan.Skipped[0] != "go.sum" {
		t.Errorf("skipped = %v, want [go.sum]", plan.Skipped)
	}
}

// Diff rỗng thật vẫn review 1 lần, để prompt chèn hướng dẫn tự đọc file.
func TestBuildBundlePlan_EmptyDiff_OneFallbackPrompt(t *testing.T) {
	plan := BuildBundlePlan(t.TempDir(), "review", "", 0, nil, "", "")

	if len(plan.Prompts) != 1 || !strings.Contains(plan.Prompts[0], "Không lấy được diff thật") {
		t.Errorf("want one fallback prompt, got %#v", plan.Prompts)
	}
}
