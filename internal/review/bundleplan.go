package review

// BundlePlan là kết quả chia diff của 1 PR thành các lần gọi Reviewer:
// Prompts[i] là prompt đầy đủ của bundle thứ i. Skipped là các file bị lọc
// (không nằm trong bundle nào, xem bundleDiffs).
//
// Prompts rỗng nghĩa là diff có nội dung nhưng mọi file đều bị lọc: không
// có gì để review. Diff rỗng thật vẫn cho đúng 1 prompt (diff ""), để
// BuildReviewPrompt chèn hướng dẫn tự đọc file state thay thế.
type BundlePlan struct {
	Prompts []string
	Skipped []string
}

// BuildBundlePlan dựng prompt cho từng bundle theo đúng cách Job.Run review
// 1 PR: chia diff theo budgetChars (<=0 dùng defaultBundleBudgetChars),
// thêm primer dùng chung khi có nhiều bundle (issue #18) và bundleNote vào
// đầu diff của từng bundle.
//
// Export để evalrunner đo chất lượng trên đúng đường production khi đổi
// ngân sách bundle (issue #73), thay vì tự dựng 1 prompt riêng dễ lệch.
//
// dir là repo đã checkout (buildPrimer dò README trên đĩa).
// extraIgnoredPatterns gộp từ .yuumi.yml và .gitignore (xem Job.Run).
func BuildBundlePlan(dir, userCommand, diff string, budgetChars int, extraIgnoredPatterns []string, staticReport, repoInstructions string) BundlePlan {
	if budgetChars <= 0 {
		budgetChars = defaultBundleBudgetChars
	}

	bundles, skipped := bundleDiffs(diff, budgetChars, extraIgnoredPatterns)
	if len(bundles) == 0 {
		if len(skipped) > 0 {
			return BundlePlan{Skipped: skipped}
		}
		bundles = []string{""}
	}

	// primer chỉ đáng tổng hợp khi PR THẬT SỰ bị chia nhiều bundle — PR bình
	// thường (1 bundle, đại đa số) đã thấy nguyên diff của mình rồi, primer
	// liệt kê lại đúng những file nó đang thấy không mang thêm giá trị gì
	// (issue #18).
	var primer string
	if len(bundles) > 1 {
		primer = buildPrimer(dir, changedFilePaths(diff, extraIgnoredPatterns))
	}

	prompts := make([]string, len(bundles))
	for i, bundleDiff := range bundles {
		promptDiff := bundleDiff
		if len(bundles) > 1 && bundleDiff != "" {
			promptDiff = bundleNote(i+1, len(bundles)) + bundleDiff
		}
		prompts[i] = BuildReviewPrompt(userCommand, promptDiff, staticReport, repoInstructions, primer)
	}
	return BundlePlan{Prompts: prompts, Skipped: skipped}
}
