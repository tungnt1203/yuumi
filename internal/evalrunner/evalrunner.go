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

	// Bundles là kết quả từng lần gọi Reviewer, theo thứ tự bundle. Fixture
	// nhỏ chỉ có 1; fixture lớn hơn budget bị chia như PR thật (issue #73).
	Bundles []BundleResult

	// Usage cộng dồn token/chi phí của mọi bundle.
	Usage review.Usage
}

// BundleResult là kết quả review 1 bundle. Err khác nil nghĩa là lần gọi
// đó lỗi; các bundle khác vẫn chạy tiếp, giống review.Job.
type BundleResult struct {
	Response string
	Stats    review.CallStats
	Err      error
}

// BuildFixtureDiff dựng diff thật (unified diff, đúng shape git/GitHub trả
// về) giữa before/ và after/ của 1 fixture, bằng cách tạo 1 git repo tạm:
// commit before/, ghi đè bằng after/, stage hết rồi "git diff --cached".
// Phải stage trước: "git diff" thường bỏ qua file untracked, nên file mới
// chỉ có trong after/ sẽ lặng lẽ biến mất khỏi diff.
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

	if err := runGit(dir, "add", "-A"); err != nil {
		cleanup()
		return "", "", nil, err
	}
	out, err := exec.Command("git", "-C", dir, "diff", "--cached").Output()
	if err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("git diff: %w", err)
	}

	return string(out), dir, cleanup, nil
}

// Run chạy 1 fixture qua đúng đường chia bundle của production
// (review.BuildBundlePlan): diff lớn hơn budgetChars bị chia, có primer và
// bundleNote như PR thật. budgetChars <= 0 dùng ngân sách mặc định của
// review.Job — nhờ vậy so được chất lượng/chi phí giữa các ngân sách khác
// nhau trên cùng fixture (issue #73). Không có repo instructions hay static
// check report.
//
// error chỉ báo lỗi dựng fixture; lỗi của từng lần review nằm ở
// BundleResult.Err.
func Run(reviewer review.Reviewer, f Fixture, budgetChars int) (Result, error) {
	diff, dir, cleanup, err := BuildFixtureDiff(f.Dir)
	if err != nil {
		return Result{Fixture: f}, err
	}
	defer cleanup()

	expected, _ := os.ReadFile(filepath.Join(f.Dir, "expected.md"))
	result := Result{Fixture: f, Diff: diff, Expected: string(expected)}

	plan := review.BuildBundlePlan(dir, "review", diff, budgetChars, nil, "", "")
	for _, prompt := range plan.Prompts {
		response, stats, err := reviewer.Review(prompt, sandbox.Local(dir))
		result.Bundles = append(result.Bundles, BundleResult{Response: response, Stats: stats, Err: err})
		result.Usage = result.Usage.Add(stats.Usage)
	}
	return result, nil
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
