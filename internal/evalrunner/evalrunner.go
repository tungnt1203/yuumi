// Package evalrunner chạy bộ fixture đánh giá chất lượng review (issue
// #10): mỗi fixture là 1 cặp thư mục before/after mô phỏng 1 PR có bug đã
// biết trước, cùng 1 file expected.md liệt kê bug kỳ vọng bot phải bắt
// được. Runner tự dựng diff thật (qua git, giống hệt shape diff GitHub trả
// về) rồi gọi thẳng review.Reviewer + review.BuildReviewPrompt — bỏ qua
// toàn bộ webhook/GitHub API vì mục đích ở đây là đánh giá CHẤT LƯỢNG
// prompt/model, không phải lại test luồng nhận webhook (đã có review.Job).
//
// Đối chiếu response với expected.md hiện làm THỦ CÔNG (đọc output, tự so
// sánh) — issue #10 explicitly cho phép bắt đầu bằng checklist tay, tự động
// hoá (assert JSON response chứa đúng category/keyword kỳ vọng) là bước sau
// nếu thực tế thấy cần, một khi đã tích luỹ đủ fixture để việc đó đáng công.
package evalrunner

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/tungnt1203/yuumi/internal/review"
	"github.com/tungnt1203/yuumi/internal/sandbox"
)

// Fixture là 1 fixture đánh giá: thư mục con của fixtures root, chứa
// before/ (state trước PR), after/ (state sau PR — chính là bug được cấy),
// và expected.md (checklist bug kỳ vọng bot phải bắt được).
type Fixture struct {
	Name string
	Dir  string
}

// LoadFixtures liệt kê mọi fixture con trong root (mỗi thư mục con là 1
// fixture), sắp theo tên để kết quả chạy deterministic.
func LoadFixtures(root string) ([]Fixture, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read fixtures dir %s: %w", root, err)
	}

	var fixtures []Fixture
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fixtures = append(fixtures, Fixture{Name: e.Name(), Dir: filepath.Join(root, e.Name())})
	}
	sort.Slice(fixtures, func(i, j int) bool { return fixtures[i].Name < fixtures[j].Name })
	return fixtures, nil
}

// Result là kết quả chạy 1 fixture — đủ dữ liệu để in ra cho người review
// tự đối chiếu với Expected (issue #10 giai đoạn đầu là checklist tay).
type Result struct {
	Fixture  Fixture
	Diff     string
	Expected string // nội dung expected.md, rỗng nếu fixture không có file này
	Response string
	Attempts int
	NumTurns int
	Usage    review.Usage
}

// BuildFixtureDiff dựng diff thật (unified diff, đúng shape git/GitHub trả
// về) giữa before/ và after/ của 1 fixture, bằng cách tạo 1 git repo tạm:
// commit before/, ghi đè bằng after/, rồi "git diff" phần chưa commit.
//
// Trả về dir (thư mục tạm ở trạng thái AFTER — dùng làm cwd cho
// Reviewer.Review, giống hệt cách review.Job dùng repo đã checkout) và
// cleanup để dọn dẹp. Không cần mạng, không phụ thuộc gitrepo.CloneRepo.
func BuildFixtureDiff(fixtureDir string) (diff string, dir string, cleanup func(), err error) {
	dir, err = os.MkdirTemp("", "yuumi-eval-*")
	if err != nil {
		return "", "", nil, err
	}
	cleanup = func() { os.RemoveAll(dir) }

	beforeDir := filepath.Join(fixtureDir, "before")
	afterDir := filepath.Join(fixtureDir, "after")

	if err := copyTree(beforeDir, dir); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("copy before/: %w", err)
	}
	if err := runGit(dir, "init", "-q"); err != nil {
		cleanup()
		return "", "", nil, err
	}
	if err := runGit(dir, "-c", "user.email=eval@yuumi.local", "-c", "user.name=yuumi-eval", "add", "-A"); err != nil {
		cleanup()
		return "", "", nil, err
	}
	if err := runGit(dir, "-c", "user.email=eval@yuumi.local", "-c", "user.name=yuumi-eval", "commit", "-q", "-m", "before"); err != nil {
		cleanup()
		return "", "", nil, err
	}

	if err := copyTree(afterDir, dir); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("copy after/: %w", err)
	}

	out, err := exec.Command("git", "-C", dir, "diff").Output()
	if err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("git diff: %w", err)
	}

	return string(out), dir, cleanup, nil
}

// Run chạy 1 fixture: dựng diff, gọi reviewer.Review với đúng prompt sản
// phẩm dùng thật (review.BuildReviewPrompt, không có repo instructions hay
// static check report — fixture đủ nhỏ để không cần bundle/primer).
func Run(reviewer review.Reviewer, f Fixture) (Result, error) {
	diff, dir, cleanup, err := BuildFixtureDiff(f.Dir)
	if err != nil {
		return Result{Fixture: f}, err
	}
	defer cleanup()

	expected, _ := os.ReadFile(filepath.Join(f.Dir, "expected.md"))

	prompt := review.BuildReviewPrompt("review", diff, "", "", "")
	response, stats, err := reviewer.Review(prompt, sandbox.Local(dir))
	if err != nil {
		return Result{Fixture: f, Diff: diff, Expected: string(expected)}, err
	}

	return Result{
		Fixture:  f,
		Diff:     diff,
		Expected: string(expected),
		Response: response,
		Attempts: stats.Attempts,
		NumTurns: stats.NumTurns,
		Usage:    stats.Usage,
	}, nil
}

// runGit chạy 1 lệnh git với cwd = dir, gộp stderr vào error message để dễ
// debug khi fixture setup sai (không phải lỗi của model đang được đánh giá).
func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v: %w\n%s", args, err, out)
	}
	return nil
}

// copyTree copy toàn bộ file/thư mục con từ src sang dst (dst đã tồn tại
// sẵn). Chỉ hỗ trợ thêm/ghi đè file — fixture hiện tại không cần case after/
// XOÁ file so với before/, giữ đơn giản, chỉ mở rộng khi thực tế cần.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
