package review

import (
	"strings"
	"testing"
)

func TestLanguageRulesForDiff_GoFile_IncludesGoRules(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n+package main"

	got := languageRulesForDiff(diff)

	if !strings.Contains(got, "Go:") || !strings.Contains(got, "Race condition") {
		t.Errorf("languageRulesForDiff() = %q, want it to include Go rules", got)
	}
}

func TestLanguageRulesForDiff_PythonFile_IncludesPythonRules(t *testing.T) {
	diff := "diff --git a/app.py b/app.py\n+def f(): pass"

	got := languageRulesForDiff(diff)

	if !strings.Contains(got, "Python:") || !strings.Contains(got, "Mutable default argument") {
		t.Errorf("languageRulesForDiff() = %q, want it to include Python rules", got)
	}
}

func TestLanguageRulesForDiff_UnknownExtension_ReturnsEmpty(t *testing.T) {
	diff := "diff --git a/README.md b/README.md\n+# hello"

	got := languageRulesForDiff(diff)

	if got != "" {
		t.Errorf("languageRulesForDiff() = %q, want empty for a file type with no default rule", got)
	}
}

func TestLanguageRulesForDiff_EmptyDiff_ReturnsEmpty(t *testing.T) {
	if got := languageRulesForDiff(""); got != "" {
		t.Errorf("languageRulesForDiff(\"\") = %q, want empty", got)
	}
}

func TestLanguageRulesForDiff_MixedLanguages_IncludesBoth_NoDuplication(t *testing.T) {
	diff := "diff --git a/main.go b/main.go\n+package main\n" +
		"diff --git a/migrate.sql b/migrate.sql\n+SELECT 1"

	got := languageRulesForDiff(diff)

	if !strings.Contains(got, "Go:") || !strings.Contains(got, "SQL:") {
		t.Errorf("languageRulesForDiff() = %q, want it to include both Go and SQL rules", got)
	}
}

func TestLanguageRulesForDiff_MultipleFilesSameLanguage_RuleNotDuplicated(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n+package a\n" +
		"diff --git a/b.go b/b.go\n+package b"

	got := languageRulesForDiff(diff)

	if strings.Count(got, "Go:") != 1 {
		t.Errorf("languageRulesForDiff() = %q, want the Go rule block to appear exactly once even with 2 .go files", got)
	}
}

func TestLanguageRulesForDiff_TSXFile_MatchesJSTSRule(t *testing.T) {
	diff := "diff --git a/App.tsx b/App.tsx\n+export default function App() {}"

	got := languageRulesForDiff(diff)

	if !strings.Contains(got, "JavaScript/TypeScript:") {
		t.Errorf("languageRulesForDiff() = %q, want it to include JS/TS rules for .tsx", got)
	}
}

func TestLanguageRulesForDiff_StableOrder(t *testing.T) {
	// Thứ tự file trong diff ngược với thứ tự defaultLanguageRules — output
	// vẫn phải theo đúng thứ tự defaultLanguageRules (Go trước SQL), không
	// phải thứ tự file xuất hiện trong diff.
	diff := "diff --git a/migrate.sql b/migrate.sql\n+SELECT 1\n" +
		"diff --git a/main.go b/main.go\n+package main"

	got := languageRulesForDiff(diff)

	if strings.Index(got, "Go:") > strings.Index(got, "SQL:") {
		t.Errorf("languageRulesForDiff() = %q, want Go rules before SQL rules regardless of file order in diff", got)
	}
}
