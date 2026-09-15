package review

import (
	"os"
	"path/filepath"
	"testing"
)

func writeGitignore(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, gitignoreFileName), []byte(content), 0o644); err != nil {
		t.Fatalf("cannot write %s: %v", gitignoreFileName, err)
	}
	return dir
}

func TestLoadGitignorePatterns_NoFile_ReturnsNilNoError(t *testing.T) {
	dir := t.TempDir() // không có .gitignore

	patterns, err := loadGitignorePatterns(dir)
	if err != nil {
		t.Fatalf("loadGitignorePatterns() unexpected error: %v", err)
	}
	if patterns != nil {
		t.Errorf("loadGitignorePatterns() = %v, want nil", patterns)
	}
}

func TestLoadGitignorePatterns_EmptyFile_ReturnsNilNoError(t *testing.T) {
	dir := writeGitignore(t, "")

	patterns, err := loadGitignorePatterns(dir)
	if err != nil {
		t.Fatalf("loadGitignorePatterns() unexpected error: %v", err)
	}
	if patterns != nil {
		t.Errorf("loadGitignorePatterns() = %v, want nil", patterns)
	}
}

func TestLoadGitignorePatterns_ValidFile_ParsesPatterns(t *testing.T) {
	dir := writeGitignore(t, "coverage/\n*.log\n")

	patterns, err := loadGitignorePatterns(dir)
	if err != nil {
		t.Fatalf("loadGitignorePatterns() unexpected error: %v", err)
	}

	want := []string{"coverage/", ".log"}
	if len(patterns) != len(want) {
		t.Fatalf("patterns = %v, want %v", patterns, want)
	}
	for i, p := range want {
		if patterns[i] != p {
			t.Errorf("patterns[%d] = %q, want %q", i, patterns[i], p)
		}
	}
}

func TestParseGitignorePatterns(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string
	}{
		{name: "blank line ignored", line: "\n\n  \n", want: nil},
		{name: "comment ignored", line: "# a comment", want: nil},
		{name: "negation ignored", line: "!important.log", want: nil},
		{name: "trailing-slash directory kept as contains pattern", line: "coverage/", want: []string{"coverage/"}},
		{name: "root-anchored leading slash stripped", line: "/dist", want: []string{"dist"}},
		{name: "nested path kept as contains pattern", line: "src/generated/", want: []string{"src/generated/"}},
		{name: "plain basename matched by suffix", line: "coverage.log", want: []string{"coverage.log"}},
		{name: "leading-star extension converted to suffix", line: "*.log", want: []string{".log"}},
		{name: "leading-star full name converted to suffix", line: "*.turbo", want: []string{".turbo"}},
		{name: "bare star unsupported, skipped", line: "*", want: nil},
		{name: "middle wildcard unsupported, skipped", line: "file*.txt", want: nil},
		{name: "question mark wildcard unsupported, skipped", line: "file?.txt", want: nil},
		{name: "bracket wildcard unsupported, skipped", line: "[a-z].txt", want: nil},
		{name: "surrounding whitespace trimmed", line: "  build/  ", want: []string{"build/"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseGitignorePatterns(tt.line)
			if len(got) != len(tt.want) {
				t.Fatalf("parseGitignorePatterns(%q) = %v, want %v", tt.line, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseGitignorePatterns(%q)[%d] = %q, want %q", tt.line, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseGitignorePatterns_MultipleLinesCombine(t *testing.T) {
	data := `# generated
coverage/
*.log

!keep-this.log
node_modules/
plain.txt
`
	got := parseGitignorePatterns(data)
	want := []string{"coverage/", ".log", "node_modules/", "plain.txt"}
	if len(got) != len(want) {
		t.Fatalf("parseGitignorePatterns() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
