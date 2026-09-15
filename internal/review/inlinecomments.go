package review

// pendingComment là 1 finding đã được XÁC THỰC khớp đúng với 1 dòng thật
// trong diff (qua parseFileHunks/FileDiff.LineAtNew, issue #24) — sẵn sàng
// gửi thành 1 inline comment qua GitHub Reviews API (issue #5). Body đã
// được render sẵn (renderFinding) để Job.postInlineComments không cần biết
// gì về Finding, chỉ việc gửi đi.
type pendingComment struct {
	Path string
	Line int
	Body string
}

// splitFindingsForPosting tách findings thành 2 nhóm:
//   - inline: có File+Line hợp lệ VÀ khớp đúng với 1 dòng thật trong diff.
//   - general: không có File/Line (nhận xét tổng quát, kiến trúc...), hoặc
//     có nhưng không đối chiếu được với diff thật.
//
// Bắt buộc đối chiếu lại với diff thật (không tin thẳng Finding.File/Line
// Claude tự báo) vì model có thể diễn giải lại thay vì copy nguyên văn số
// dòng — post nhầm dòng còn tệ hơn không post inline, nên finding không xác
// thực được rơi về hiển thị trong comment tổng hợp (general) thay vì bị bỏ
// luôn hay post sai chỗ trong im lặng (đúng ghi chú "fallback verify" của
// issue #24).
//
// diff phải là diff của ĐÚNG bundle đã gửi cho Reviewer sinh ra findings
// này (không phải diff của cả PR khi PR bị chia nhiều bundle) — Line trong
// Finding chỉ có nghĩa trong phạm vi diff Claude thực sự đã thấy.
func splitFindingsForPosting(diff string, findings []Finding) (inline []pendingComment, general []Finding) {
	index := buildFileDiffIndex(diff)

	for _, f := range findings {
		if f.File == "" || f.Line <= 0 {
			general = append(general, f)
			continue
		}
		fd, ok := index[f.File]
		if !ok {
			general = append(general, f)
			continue
		}
		if _, ok := fd.LineAtNew(f.Line); !ok {
			general = append(general, f)
			continue
		}
		inline = append(inline, pendingComment{Path: f.File, Line: f.Line, Body: renderFinding(f)})
	}

	return inline, general
}

// buildFileDiffIndex parse hunk-level từng file trong diff, index theo
// NewPath — dùng để tra cứu nhanh khi xác thực Finding.File/Line (xem
// splitFindingsForPosting). File không xác định được NewPath (vd bị xoá
// hoàn toàn — không có gì để comment "vào dòng mới" cả) không được đưa vào
// index, nên finding trỏ tới file đó tự động rơi về general.
func buildFileDiffIndex(diff string) map[string]FileDiff {
	index := make(map[string]FileDiff)
	for _, body := range splitDiffByFile(diff) {
		fd := parseFileHunks(body)
		if fd.NewPath != "" {
			index[fd.NewPath] = fd
		}
	}
	return index
}
