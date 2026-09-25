package review

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
)

// gitlinkMode là file mode git dùng cho 1 submodule (gitlink) trong diff.
const gitlinkMode = "160000"

// gitmodulesFile là file khai báo submodule ở gốc repo.
const gitmodulesFile = ".gitmodules"

// submoduleChange là 1 submodule đổi commit trong diff (issue #93).
// OldSHA rỗng: submodule mới thêm. NewSHA rỗng: submodule bị gỡ.
type submoduleChange struct {
	Path   string
	OldSHA string
	NewSHA string
}

// bringsCode báo thay đổi này có đưa code mới vào repo không — gỡ
// submodule thì không có gì để review.
func (c submoduleChange) bringsCode() bool {
	return c.NewSHA != ""
}

// splitSubmoduleChanges tách các file diff là gitlink khỏi diff. Diff của 1
// gitlink chỉ có dòng "Subproject commit <sha>": bot không có code của
// submodule (clone không fetch submodule), nên gửi nó cho Claude chỉ làm
// Claude kết luận "không có vấn đề" về code nó chưa từng thấy.
//
// rest là diff còn lại (file thường), giữ nguyên thứ tự. Diff không tách
// được theo "diff --git" thì trả nguyên diff, không có change nào.
func splitSubmoduleChanges(diff string) (rest string, changes []submoduleChange) {
	files := splitDiffByFile(diff)
	if len(files) == 0 {
		return diff, nil
	}
	kept := make([]string, 0, len(files))
	for _, body := range files {
		change, ok := parseGitlinkDiff(body)
		if !ok {
			kept = append(kept, body)
			continue
		}
		changes = append(changes, change)
	}
	if len(changes) == 0 {
		return diff, nil
	}
	return strings.Join(kept, "\n"), changes
}

// parseGitlinkDiff nhận diff của 1 file là gitlink khi phần header (trước
// hunk đầu tiên) có mode 160000: "index a..b 160000", "new file mode
// 160000" hoặc "deleted file mode 160000".
func parseGitlinkDiff(body string) (submoduleChange, bool) {
	header, hunks, _ := strings.Cut(body, "\n@@")
	isGitlink := false
	for _, line := range strings.Split(header, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[len(fields)-1] == gitlinkMode &&
			(fields[0] == "index" || strings.HasSuffix(line, "file mode "+gitlinkMode)) {
			isGitlink = true
			break
		}
	}
	if !isGitlink {
		return submoduleChange{}, false
	}

	change := submoduleChange{Path: extractFilePath(body)}
	for _, line := range strings.Split(hunks, "\n") {
		switch {
		case strings.HasPrefix(line, "-Subproject commit "):
			change.OldSHA = strings.TrimSpace(strings.TrimPrefix(line, "-Subproject commit "))
		case strings.HasPrefix(line, "+Subproject commit "):
			change.NewSHA = strings.TrimSpace(strings.TrimPrefix(line, "+Subproject commit "))
		}
	}
	return change, true
}

// parseGitmodules đọc .gitmodules (định dạng git config) thành map path ->
// url. Chỉ cần 2 key này; section không có path bị bỏ qua.
func parseGitmodules(data []byte) map[string]string {
	urls := map[string]string{}
	var path, url string
	flush := func() {
		if path != "" {
			urls[path] = url
		}
		path, url = "", ""
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			flush()
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "path":
			path = strings.TrimSpace(value)
		case "url":
			url = strings.TrimSpace(value)
		}
	}
	flush()
	return urls
}

// gitmodulesURLFindings so URL submodule giữa base và head, trả 1 finding
// security cho mỗi submodule đã có ở base mà PR đổi URL. Đổi nguồn của 1
// submodule là cách thay code dependency mà diff của repo không cho thấy —
// dấu hiệu tấn công supply chain, cần người review để ý (issue #93).
// Submodule mới thêm hoặc bị gỡ không tính.
func gitmodulesURLFindings(base, head map[string]string) []Finding {
	var findings []Finding
	for path, oldURL := range base {
		newURL, ok := head[path]
		if !ok || newURL == oldURL {
			continue
		}
		findings = append(findings, Finding{
			File:     gitmodulesFile,
			Category: "security",
			Severity: "high",
			Message: fmt.Sprintf("PR đổi nguồn của submodule `%s` từ `%s` sang `%s`. Code của submodule sẽ được lấy từ nguồn mới, và diff của repo này không cho thấy code đó. "+
				"Hãy xác nhận nguồn mới là đáng tin trước khi merge.", path, oldURL, newURL),
		})
	}
	// Map không có thứ tự: sắp theo message (chứa path) để comment ổn định.
	sort.Slice(findings, func(i, j int) bool { return findings[i].Message < findings[j].Message })
	return findings
}

// readHeadGitmodules đọc .gitmodules trong thư mục clone (head). Không có
// file thì trả map rỗng. Đọc qua os.Root: file do tác giả PR kiểm soát,
// symlink trỏ ra ngoài thư mục clone (vd tới file bí mật trên server) bị
// từ chối thay vì bị đọc rồi đưa vào comment.
func readHeadGitmodules(dir string) (map[string]string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	data, err := root.ReadFile(gitmodulesFile)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	return parseGitmodules(data), nil
}

// submoduleNote render ghi chú về các submodule có code mới mà bot không
// review. Rỗng nếu không có.
func submoduleNote(changes []submoduleChange) string {
	var lines []string
	for _, c := range changes {
		if !c.bringsCode() {
			continue
		}
		if c.OldSHA == "" {
			lines = append(lines, fmt.Sprintf("- `%s`: thêm mới tại `%s`", c.Path, shortSHA(c.NewSHA)))
		} else {
			lines = append(lines, fmt.Sprintf("- `%s`: `%s` → `%s`", c.Path, shortSHA(c.OldSHA), shortSHA(c.NewSHA)))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "⚠️ PR đổi submodule. Bot chỉ review code của repo này, **nội dung submodule chưa được review**:\n" + strings.Join(lines, "\n")
}

// unreviewedSubmoduleCount đếm submodule đưa code mới vào mà bot không xem.
func unreviewedSubmoduleCount(changes []submoduleChange) int {
	n := 0
	for _, c := range changes {
		if c.bringsCode() {
			n++
		}
	}
	return n
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// submoduleURLFindings kiểm tra PR có đổi URL của submodule đã có không.
// Chỉ chạy khi diff đầy đủ của PR đụng tới .gitmodules (thêm 1 lệnh gọi
// GitHub API). .gitmodules ở base đọc qua API; ở head đọc từ thư mục clone.
//
// Lỗi đọc trả note thay vì im lặng: bỏ qua lặng lẽ thì người review tưởng
// URL đã được kiểm tra.
func (j *Job) submoduleURLFindings(dir, baseSHA, fullDiff string) (findings []Finding, note string) {
	const failNote = "⚠️ Không kiểm tra được thay đổi URL submodule trong `.gitmodules`. Xem log server."

	if !diffTouchesFile(fullDiff, gitmodulesFile) {
		return nil, ""
	}
	if baseSHA == "" {
		fmt.Println("Check .gitmodules: PR không có base SHA")
		return nil, failNote
	}
	data, found, err := j.GitHub.GetFileContent(j.RepoFullName, gitmodulesFile, baseSHA)
	if err != nil {
		fmt.Println("Get base .gitmodules error:", err)
		return nil, failNote
	}
	if !found {
		// Base chưa có submodule nào: mọi submodule đều mới thêm.
		return nil, ""
	}
	head, err := readHeadGitmodules(dir)
	if err != nil {
		fmt.Println("Read head .gitmodules error:", err)
		return nil, failNote
	}
	return gitmodulesURLFindings(parseGitmodules(data), head), ""
}

// diffTouchesFile báo diff có đổi file path không.
func diffTouchesFile(diff, path string) bool {
	for _, body := range splitDiffByFile(diff) {
		if extractFilePath(body) == path {
			return true
		}
	}
	return false
}
