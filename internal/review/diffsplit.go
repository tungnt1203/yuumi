package review

import (
	"path"
	"strings"
)

// truncationNotice được nối vào cuối 1 file diff bị cắt bớt vì quá lớn, để
// Claude (và người đọc log/comment sau này) biết phần còn lại của file đó
// chưa được xem qua — tránh review kết luận nhầm là đã kiểm tra toàn bộ.
const truncationNotice = "\n... (diff đã bị cắt bớt vì quá lớn, phần còn lại chưa được review) ...\n"

// defaultIgnoredPathPatterns là các file/thư mục không đáng review: máy
// sinh ra (lock file, generated) hoặc không phải code (binary, ảnh, font).
// Review chúng vừa tốn bundle/token vừa không có giá trị. Pattern có "/"
// được match theo kiểu "path chứa thư mục này"; pattern không có "/" được
// match theo đuôi file (đủ cho tên file cố định như "go.sum" lẫn đuôi file
// như ".min.js").
var defaultIgnoredPathPatterns = []string{
	// thư mục dependency/build output
	"vendor/", "node_modules/", "dist/", "build/", "target/", ".next/",
	// cache/artifact runtime — không phải code người viết, sinh lại được
	"__pycache__/", "__snapshots__/",
	// lock file phổ biến — máy sinh ra, không phải code người viết
	"go.sum", "go.work.sum", "package-lock.json", "yarn.lock", "pnpm-lock.yaml",
	"Cargo.lock", "Gemfile.lock", "poetry.lock", "composer.lock",
	// generated/binary/minified thường gặp
	".min.js", ".min.css", ".svg", ".png", ".jpg", ".jpeg", ".gif", ".ico", ".woff", ".woff2", ".pdf",
	".pyc", ".snap",
	// code sinh tự động từ IDL (protobuf) — review file .proto gốc là đủ
	".pb.go", "_pb2.py",
}

// isIgnoredPath báo path có khớp defaultIgnoredPathPatterns hoặc
// extraPatterns không. extraPatterns đến từ cấu hình riêng của repo
// (.yuumi.yml "exclude" — xem loadRepoConfig, issue #7) GỘP với .gitignore
// thật của repo (xem loadGitignorePatterns, issue #4): GỘP THÊM vào default
// chứ không thay thế, vì default vẫn luôn đúng (lock file/vendor không
// đáng review ở mọi repo) — 2 nguồn kia chỉ dùng để loại trừ thêm những gì
// đặc thù riêng của repo đó.
func isIgnoredPath(path string, extraPatterns []string) bool {
	return matchesAnyPattern(path, defaultIgnoredPathPatterns) || matchesAnyPattern(path, extraPatterns)
}

// matchesAnyPattern kiểm tra path có khớp pattern nào trong patterns không,
// theo cùng quy tắc match của isIgnoredPath (tách riêng để dùng chung cho
// cả default lẫn extra patterns).
func matchesAnyPattern(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(pattern, "/") {
			if strings.Contains(path, pattern) {
				return true
			}
			continue
		}
		if strings.HasSuffix(path, pattern) {
			return true
		}
	}
	return false
}

// extractFilePath lấy đường dẫn file từ dòng đầu của 1 file diff, dạng
// "diff --git a/<path> b/<path>". Trả "" nếu không parse được (fail-safe —
// khi đó isIgnoredPath("") luôn false, file coi như không bị lọc thay vì
// làm hỏng cả bundling).
func extractFilePath(fileDiffBody string) string {
	firstLine, _, _ := strings.Cut(fileDiffBody, "\n")
	if !strings.HasPrefix(firstLine, "diff --git ") {
		return ""
	}
	fields := strings.Fields(firstLine)
	if len(fields) < 3 {
		return ""
	}
	return strings.TrimPrefix(fields[2], "a/")
}

// splitDiffByFile tách 1 unified diff (gộp nhiều file) thành từng phần theo
// file. Git luôn bắt đầu diff của mỗi file bằng 1 dòng "diff --git a/... b/...",
// nên chỉ cần cắt theo dòng đó là đủ, không cần parse hunk/header chi tiết.
func splitDiffByFile(diff string) []string {
	lines := strings.Split(diff, "\n")
	var files []string
	var cur strings.Builder
	has := false

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			if has {
				files = append(files, cur.String())
				cur.Reset()
			}
			has = true
			cur.WriteString(line)
			continue
		}
		if !has {
			// Nội dung trước dòng "diff --git" đầu tiên — không nên xảy ra
			// với diff GitHub trả về, bỏ qua thay vì panic để hàm luôn an toàn.
			continue
		}
		cur.WriteString("\n")
		cur.WriteString(line)
	}
	if has {
		files = append(files, cur.String())
	}
	return files
}

// keptFile là 1 file diff còn lại sau bước lọc ignore, kèm path của nó —
// cần cả path (không chỉ body) để groupByDirectory biết file nào cùng thư
// mục với file nào.
type keptFile struct {
	path string
	body string
}

// groupByDirectory sắp lại files sao cho các file cùng thư mục cha trực
// tiếp (path.Dir) đứng liền nhau, thay vì giữ nguyên thứ tự rải rác trong
// diff gốc. Dùng "cùng thư mục" làm heuristic cho "liên quan" — đơn giản,
// không cần biết convention riêng của từng ngôn ngữ, và với Go (ngôn ngữ
// của chính project này) heuristic này giải quyết trọn vẹn case hay gặp
// nhất: file impl + file test luôn cùng thư mục.
//
// Dùng package "path" (không phải "path/filepath") vì path lấy từ git diff
// luôn dùng "/" bất kể OS chạy bot.
//
// Giữ thứ tự thư mục theo lần xuất hiện đầu tiên và thứ tự tương đối giữa
// các file trong cùng 1 thư mục — kết quả deterministic, không xáo trộn vô
// nghĩa so với input.
func groupByDirectory(files []keptFile) []string {
	var dirOrder []string
	groups := make(map[string][]string, len(files))

	for _, f := range files {
		dir := path.Dir(f.path)
		if _, ok := groups[dir]; !ok {
			dirOrder = append(dirOrder, dir)
		}
		groups[dir] = append(groups[dir], f.body)
	}

	result := make([]string, 0, len(files))
	for _, dir := range dirOrder {
		result = append(result, groups[dir]...)
	}
	return result
}

// truncateDiff cắt bớt diff của 1 file quá lớn xuống budgetChars, giữ lại
// phần đầu (thường có mật độ thông tin cao nhất: header + các hunk đầu) và
// đánh dấu rõ đã bị cắt.
func truncateDiff(body string, budgetChars int) string {
	if len(body) <= budgetChars {
		return body
	}
	return body[:budgetChars] + truncationNotice
}

// bundleDiffs chia diff của cả PR thành các "bundle" — mỗi bundle là 1 nhóm
// file sẽ được review riêng trong 1 lần gọi Reviewer.Review, để PR lớn không
// bị nhồi nguyên vào 1 prompt (xem issue #3). skipped là danh sách path đã
// bị loại vì khớp defaultIgnoredPathPatterns hoặc extraIgnoredPatterns
// (không nằm trong bundle nào). extraIgnoredPatterns là danh sách đã GỘP
// SẴN từ .yuumi.yml của repo (issue #7) và .gitignore thật của repo (issue
// #4) — Job.Run chịu trách nhiệm gộp trước khi gọi hàm này; nil nếu repo
// không có cả hai.
//
// Chiến lược, theo thứ tự ưu tiên:
//  1. Tách theo file rồi lọc bỏ file không đáng review (defaultIgnoredPathPatterns
//     + extraIgnoredPatterns) TRƯỚC khi tính budget — PR nhỏ có kèm go.sum/vendor
//     cũng phải được lọc, không chỉ PR lớn.
//  2. Nếu các file còn lại (sau lọc) đã nằm trong budgetChars: gộp thành 1
//     bundle — tuyệt đại đa số PR (nhỏ/vừa, không dính file bị lọc) đi qua
//     nhánh này, kết quả giống hệt hành vi trước khi có bundling.
//  3. Nếu vượt budget: gom file vào bundle theo kiểu greedy bin-packing —
//     không cố tìm cách chia tối ưu, ưu tiên đơn giản/ổn định vì đây là
//     đường ít gặp.
//  4. Nếu 1 file tự nó đã vượt budget (vd file generated/lock lớn lọt lưới
//     defaultIgnoredPathPatterns), file đó được tách thành bundle riêng và
//     bị truncateDiff — đảm bảo không bao giờ có 1 bundle vượt budget.
func bundleDiffs(diff string, budgetChars int, extraIgnoredPatterns []string) (bundles []string, skipped []string) {
	if strings.TrimSpace(diff) == "" {
		return nil, nil
	}

	files := splitDiffByFile(diff)
	if len(files) == 0 {
		// Không parse được theo "diff --git " (định dạng lạ/không như kỳ
		// vọng) — không lọc được gì, thà gửi nguyên/cắt bớt còn hơn không
		// review gì cả.
		return []string{truncateDiff(diff, budgetChars)}, nil
	}

	keptFiles := make([]keptFile, 0, len(files))
	for _, body := range files {
		p := extractFilePath(body)
		if p != "" && isIgnoredPath(p, extraIgnoredPatterns) {
			skipped = append(skipped, p)
			continue
		}
		keptFiles = append(keptFiles, keptFile{path: p, body: body})
	}
	if len(keptFiles) == 0 {
		return nil, skipped
	}

	// Sắp lại để file cùng thư mục đứng liền nhau — greedy pack bên dưới
	// nhờ đó tự nhiên nhóm chúng vào cùng 1 bundle khi vừa budget, thay vì
	// tách rời chỉ vì tình cờ nằm cách xa nhau trong diff gốc.
	kept := groupByDirectory(keptFiles)

	totalLen := 0
	for _, body := range kept {
		totalLen += len(body)
	}
	if totalLen <= budgetChars {
		return []string{strings.Join(kept, "\n")}, skipped
	}

	var cur strings.Builder
	curLen := 0

	flush := func() {
		if curLen == 0 {
			return
		}
		bundles = append(bundles, cur.String())
		cur.Reset()
		curLen = 0
	}

	for _, body := range kept {
		if len(body) > budgetChars {
			flush()
			bundles = append(bundles, truncateDiff(body, budgetChars))
			continue
		}
		if curLen > 0 && curLen+len(body) > budgetChars {
			flush()
		}
		if curLen > 0 {
			cur.WriteString("\n")
		}
		cur.WriteString(body)
		curLen += len(body)
	}
	flush()

	return bundles, skipped
}
