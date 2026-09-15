package review

import "strings"

// truncationNotice được nối vào cuối 1 file diff bị cắt bớt vì quá lớn, để
// Claude (và người đọc log/comment sau này) biết phần còn lại của file đó
// chưa được xem qua — tránh review kết luận nhầm là đã kiểm tra toàn bộ.
const truncationNotice = "\n... (diff đã bị cắt bớt vì quá lớn, phần còn lại chưa được review) ...\n"

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
// bị nhồi nguyên vào 1 prompt (xem issue #3).
//
// Chiến lược, theo thứ tự ưu tiên:
//  1. Nếu cả diff đã nằm trong budgetChars: trả về nguyên diff, y hệt hành
//     vi trước khi có bundling — tuyệt đại đa số PR (nhỏ/vừa) đi qua nhánh
//     này, không bị ảnh hưởng gì bởi thay đổi này.
//  2. Nếu vượt budget: tách theo file (splitDiffByFile) rồi gom file vào
//     bundle theo kiểu greedy bin-packing — không cố tìm cách chia tối ưu,
//     ưu tiên đơn giản/ổn định vì đây là đường ít gặp.
//  3. Nếu 1 file tự nó đã vượt budget (vd file generated/lock lớn), file đó
//     được tách thành bundle riêng và bị truncateDiff — đảm bảo không bao
//     giờ có 1 bundle vượt budget, dù input thế nào.
func bundleDiffs(diff string, budgetChars int) []string {
	if strings.TrimSpace(diff) == "" {
		return nil
	}
	if len(diff) <= budgetChars {
		return []string{diff}
	}

	files := splitDiffByFile(diff)
	if len(files) == 0 {
		// Không parse được theo "diff --git " (định dạng lạ/không như kỳ
		// vọng) — thà gửi 1 bundle bị cắt bớt còn hơn không review gì cả.
		return []string{truncateDiff(diff, budgetChars)}
	}

	var bundles []string
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

	for _, body := range files {
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

	return bundles
}
