package review

import (
	"fmt"
	"strings"
)

// BuildReviewPrompt assembles the final prompt sent to the Claude CLI.
//
// userCommand is whatever the requester typed after mentioning the bot
// (e.g. "review", "review kỹ phần error handling"). diff is the real PR
// diff fetched from GitHub (see githubapi.GetPullRequestDiff) — it may be
// empty if fetching the diff failed, in which case Claude falls back to
// reading the checked-out file state only.
//
// The prompt explicitly tells Claude to read surrounding project files
// (README, related packages/conventions) before judging the diff, instead
// of reviewing the changed lines in isolation.
//
// staticCheckNote là kết quả check tĩnh tự động (gofmt/go vet — xem
// staticCheckReport, issue #8), rỗng nếu không detect được toolchain hoặc
// không có gì để báo cáo. Chèn vào prompt như context có sẵn để Claude khỏi
// phải tự đọc code ra mới bắt được các lỗi máy đã bắt tốt hơn, tập trung
// review logic/thiết kế.
//
// repoInstructions là hướng dẫn review riêng của repo, đọc từ .yuumi.yml
// (xem loadRepoConfig, issue #7) — rỗng nếu repo không có file cấu hình.
// Chèn ngay sau yêu cầu của người review vì cùng là "cách review nên làm",
// người review gõ trực tiếp lúc mention bot còn repoInstructions là mặc
// định cố định của cả repo.
//
// Ngoài ra, prompt tự chèn thêm rule mặc định theo TỪNG loại file có trong
// diff (xem languageRulesForDiff, issue #25) — không cần repo nào cấu hình
// gì cả, khác repoInstructions ở trên. Đặt sau repoInstructions và có ghi
// chú rõ độ ưu tiên thấp hơn, vì rule của repo (nếu có) phản ánh đúng ý
// người maintain repo đó hơn rule chung bot tự đoán.
//
// Prompt cũng tự chèn danh sách symbol (hàm/type/method) trích best-effort
// từ dòng thay đổi trong diff, kèm hướng dẫn tự grep tìm nơi dùng ở nơi
// khác trong repo trước khi kết luận không có breaking change (xem
// changedSymbolsNote, issue #19) — bù cho heuristic "cùng thư mục"
// (groupByDirectory, diffsplit.go) vốn bỏ sót case 1 đổi signature ảnh
// hưởng file ở package khác.
func BuildReviewPrompt(userCommand string, diff string, staticCheckNote string, repoInstructions string) string {
	var b strings.Builder

	b.WriteString("Bạn đang review một Pull Request trong repo hiện tại (thư mục làm việc chính là repo đã checkout).\n\n")
	b.WriteString("Yêu cầu từ người review: ")
	b.WriteString(userCommand)
	b.WriteString("\n\n")

	if strings.TrimSpace(repoInstructions) != "" {
		b.WriteString("Hướng dẫn review riêng cho repo này (cấu hình trong .yuumi.yml):\n")
		b.WriteString(repoInstructions)
		b.WriteString("\n\n")
	}

	if rules := languageRulesForDiff(diff); rules != "" {
		b.WriteString("Ngoài ra, chú ý thêm các điểm sau theo từng loại file có trong diff (rule mặc định — nếu xung đột với hướng dẫn riêng của repo ở trên thì hướng dẫn của repo được ưu tiên hơn):\n")
		b.WriteString(rules)
		b.WriteString("\n\n")
	}

	if note := changedSymbolsNote(diff); note != "" {
		b.WriteString(note)
		b.WriteString("\n\n")
	}

	if strings.TrimSpace(diff) != "" {
		b.WriteString("Đây là diff thật của PR (unified diff), review tập trung vào đúng các dòng thay đổi này:\n\n")
		b.WriteString("```diff\n")
		b.WriteString(diff)
		b.WriteString("\n```\n\n")
	} else {
		b.WriteString("Không lấy được diff thật của PR (có thể do lỗi gọi GitHub API). Hãy tự xác định phần thay đổi bằng cách đọc commit message và các file trong repo.\n\n")
	}

	if strings.TrimSpace(staticCheckNote) != "" {
		b.WriteString(staticCheckNote)
		b.WriteString("\n\n")
	}

	b.WriteString("Trước khi kết luận, hãy đọc thêm các file liên quan trong repo (README, package/module xung quanh các file đã đổi) để hiểu đúng kiến trúc và convention của project — đừng chỉ nhìn diff một cách cô lập.\n\n")

	b.WriteString(resultFormatInstructions)

	return b.String()
}

// resultFormatInstructions yêu cầu Claude trả kết quả dưới dạng JSON array
// có cấu trúc thay vì văn xuôi tự do — mỗi phần tử là 1 finding với
// file/line/category/severity/message/suggestion, để comment hiển thị phân
// loại rõ ràng theo mức độ quan trọng, và gắn được trực tiếp vào đúng dòng
// code qua GitHub Reviews API khi xác định được vị trí (xem
// parseFindings/renderFindings, issue #26; splitFindingsForPosting, issue
// #5).
//
// Yêu cầu "CHỈ trả JSON, không bọc trong ```, không kèm giải thích ngoài
// JSON" vì parseFindings cần parse được text; Claude đôi khi vẫn không tuân
// thủ tuyệt đối (thêm vài câu trước/sau, hoặc bọc code fence) — parseFindings
// đã tự trích phần "[...]" để chịu được sai lệch nhỏ đó, và nếu vẫn không
// parse được thì fallback hiển thị nguyên văn, không chặn/hỏng cả lần
// review (xem Job.reviewBundles).
//
// "line" phải là số dòng trong FILE MỚI (sau khi áp dụng thay đổi của PR),
// đúng như xuất hiện ở khối diff bên trên — không phải số thứ tự trong toàn
// bộ file, và không suy đoán nếu không chắc. splitFindingsForPosting sẽ tự
// đối chiếu lại với diff thật; file/line sai hoặc không khớp không làm hỏng
// gì cả, finding đó chỉ đơn giản rơi về hiển thị trong comment tổng hợp
// thay vì gắn inline — nên Claude cứ để trống nếu không chắc còn hơn đoán
// bừa.
//
// Ví dụ JSON dưới đây để "line" là 1 SỐ KHÔNG BỌC NGOẶC KÉP (0) — cố tình,
// vì Finding.Line là int: nếu ví dụ cũng bọc ngoặc kép như mọi field string
// khác (file/category/severity/message/suggestion), Claude rất dễ bắt
// chước style đó và trả "line":"12" (chuỗi), làm hỏng luôn cả json.Unmarshal
// của TOÀN BỘ array (không chỉ finding đó) — đây từng là bug thật, xem PR
// review issue #5.
const resultFormatInstructions = `Trả kết quả CHỈ dưới dạng 1 JSON array (không thêm giải thích ngoài JSON, không bọc trong markdown code fence), mỗi phần tử là 1 finding theo đúng format sau — chú ý "line" LUÔN là số, KHÔNG bọc trong dấu ngoặc kép:
[{"file":"đường dẫn file đúng như trong diff, chuỗi rỗng nếu là nhận xét tổng quát không gắn với 1 dòng cụ thể","line":0,"category":"bug|security|performance|maintainability|test|style|documentation","severity":"critical|high|medium|low","message":"mô tả ngắn gọn vấn đề","suggestion":"đoạn code gợi ý sửa cụ thể, để chuỗi rỗng nếu không áp dụng được"}]
"line" là số dòng trong file MỚI (sau khi áp dụng thay đổi của PR) đúng như xuất hiện ở khối diff bên trên, không phải số thứ tự trong toàn bộ file — để 0 nếu không chắc hoặc là nhận xét tổng quát, đừng đoán bừa.
Nếu code không có vấn đề gì đáng chú ý, trả về mảng rỗng: []
`

// bundleNote được chèn vào đầu diff khi PR quá lớn và bị chia thành nhiều
// bundle (xem bundleDiffs) — báo cho Claude biết nó chỉ đang thấy 1 phần
// của PR, để không kết luận nhầm (vd "PR chỉ sửa 3 file") khi thực ra còn
// các phần khác đang được review riêng.
func bundleNote(index, total int) string {
	return fmt.Sprintf(
		"[Lưu ý: PR này khá lớn nên được chia làm %d phần để review, đây là phần %d/%d. "+
			"Diff dưới đây KHÔNG phải toàn bộ PR — chỉ nhận xét dựa trên phần được giao, "+
			"đừng kết luận về những file không xuất hiện ở đây.]\n\n",
		total, index, total,
	)
}
