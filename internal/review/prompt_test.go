package review

import (
	"strings"
	"testing"
)

func TestBuildReviewPrompt(t *testing.T) {
	tests := []struct {
		name        string
		userCommand string
		diff        string
		wantContain []string
		wantAbsent  []string
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildReviewPrompt(tt.userCommand, tt.diff)

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

func TestBundleNote(t *testing.T) {
	got := bundleNote(2, 5)

	for _, want := range []string{"2/5", "chia làm 5 phần"} {
		if !strings.Contains(got, want) {
			t.Errorf("bundleNote(2, 5) missing %q in output: %q", want, got)
		}
	}
}
