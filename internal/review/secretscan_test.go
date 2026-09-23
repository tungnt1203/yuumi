package review

import (
	"fmt"
	"strings"
	"testing"
)

// Giá trị secret trong test được ghép chuỗi lúc chạy, để chính file test
// không bị secret scanner (GitHub push protection...) nhận nhầm là lộ key.
var (
	fakeAWSKey    = "AKIA" + "IOSFODNN7EXAMPLE"
	fakePEMHeader = "-----BEGIN RSA " + "PRIVATE KEY-----"
	fakeGHToken   = "ghp_" + strings.Repeat("a1B2", 9)
)

func secretTestDiff(path string, added ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n", path, path, path, path)
	fmt.Fprintf(&b, "@@ -1,1 +1,%d @@\n package main\n", len(added)+1)
	for _, l := range added {
		fmt.Fprintf(&b, "+%s\n", l)
	}
	return b.String()
}

func TestScanSecrets_DetectsKnownPatterns(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantLabel string
	}{
		{"aws key", `const key = "` + fakeAWSKey + `"`, "AWS access key"},
		{"pem header", fakePEMHeader, "private key (PEM)"},
		{"github token", `token := "` + fakeGHToken + `"`, "GitHub token"},
		{"connection string", `dsn := "postgres://admin:hunter22@db:5432/app"`, "connection string có password"},
		{"password literal", `dbPassword := "s3cr3tPass"`, "password/secret gán string literal"},
		{"yaml password", `password: "s3cr3tPass"`, "password/secret gán string literal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits := scanSecrets(secretTestDiff("config.go", tt.line))
			if len(hits) != 1 {
				t.Fatalf("scanSecrets() = %v, want exactly 1 hit", hits)
			}
			if hits[0].label != tt.wantLabel {
				t.Errorf("label = %q, want %q", hits[0].label, tt.wantLabel)
			}
			if hits[0].file != "config.go" || hits[0].line != 2 {
				t.Errorf("position = %s:%d, want config.go:2", hits[0].file, hits[0].line)
			}
		})
	}
}

func TestScanSecrets_IgnoresSafeLines(t *testing.T) {
	diff := secretTestDiff("config.go",
		`password := os.Getenv("DB_PASSWORD")`,
		`password := ""`,
		`url := "https://example.com/path"`,
	)
	if hits := scanSecrets(diff); len(hits) != 0 {
		t.Errorf("scanSecrets() = %v, want no hits for env lookups / empty values / plain URLs", hits)
	}
}

func TestScanSecrets_OnlyAddedLines(t *testing.T) {
	diff := "diff --git a/c.go b/c.go\n--- a/c.go\n+++ b/c.go\n" +
		"@@ -1,2 +1,1 @@\n" +
		" const key = \"" + fakeAWSKey + "\"\n" +
		"-const old = \"" + fakeAWSKey + "\"\n"
	if hits := scanSecrets(diff); len(hits) != 0 {
		t.Errorf("scanSecrets() = %v, want no hits for context/removed lines", hits)
	}
}

func TestSecretScanNote_DoesNotEchoSecretValue(t *testing.T) {
	got := secretScanNote(secretTestDiff("config.go", `const key = "`+fakeAWSKey+`"`))

	if !strings.Contains(got, "config.go:2") || !strings.Contains(got, "AWS access key") {
		t.Errorf("secretScanNote() = %q, want file:line and label", got)
	}
	if strings.Contains(got, fakeAWSKey) {
		t.Errorf("secretScanNote() leaked the secret value: %q", got)
	}
}

func TestSecretScanNote_NoHits_ReturnsEmpty(t *testing.T) {
	if got := secretScanNote(secretTestDiff("main.go", `fmt.Println("hi")`)); got != "" {
		t.Errorf("secretScanNote() = %q, want empty", got)
	}
}

func TestSecretScanNote_CapsListedHits(t *testing.T) {
	lines := make([]string, maxSecretHits+3)
	for i := range lines {
		lines[i] = `const key = "` + fakeAWSKey + `"`
	}
	got := secretScanNote(secretTestDiff("keys.go", lines...))

	if n := strings.Count(got, "AWS access key"); n != maxSecretHits {
		t.Errorf("listed %d hits, want %d", n, maxSecretHits)
	}
	if !strings.Contains(got, "và 3 dòng khác") {
		t.Errorf("secretScanNote() = %q, want remaining count", got)
	}
}

func TestBuildReviewPrompt_SecretRule_AlwaysIncluded(t *testing.T) {
	// README.md không có rule ngôn ngữ nào — rule secret vẫn phải có mặt.
	got := BuildReviewPrompt("review", "diff --git a/README.md b/README.md\n+# hi", "", "", "")

	if !strings.Contains(got, "Secret/credential hardcode") {
		t.Errorf("BuildReviewPrompt() missing secret rule for a file type with no language rule:\n%s", got)
	}
}

func TestBuildReviewPrompt_IncludesSecretScanNote(t *testing.T) {
	got := BuildReviewPrompt("review", secretTestDiff("config.go", fakePEMHeader), "", "", "")

	if !strings.Contains(got, "config.go:2 — private key (PEM)") {
		t.Errorf("BuildReviewPrompt() missing secret scan note:\n%s", got)
	}
}
