// Lệnh evalrun chạy bộ fixture đánh giá chất lượng review (issue #10) và in
// kết quả ra stdout để đối chiếu THỦ CÔNG với checklist expected.md của
// từng fixture — xem evalsuite/README.md để biết cách thêm fixture mới và
// ghi lại kết quả vào evalsuite/results.md.
//
// Dùng `claude` CLI thật (cùng binary review.Job dùng ở production) nên cần
// đã cài đặt và authenticate sẵn — không chạy được trong CI/`go test`
// thông thường, đây là công cụ chạy tay khi cần đánh giá 1 thay đổi
// prompt/model.
//
//	go run ./cmd/evalrun                # chạy toàn bộ fixture
//	go run ./cmd/evalrun sql-injection-go go-goroutine-leak
//	go run ./cmd/evalrun -budget 100000 # đổi ngân sách bundle (issue #73)
package main

import (
	"flag"
	"fmt"
	"os"
	"slices"

	"github.com/tungnt1203/yuumi/internal/claudecli"
	"github.com/tungnt1203/yuumi/internal/evalrunner"
)

func main() {
	fixturesDir := flag.String("fixtures", "evalsuite/testdata", "thư mục chứa các fixture")
	budget := flag.Int("budget", 0, "ngân sách ký tự mỗi bundle, 0 = mặc định của review.Job")
	flag.Parse()
	only := flag.Args()

	fixtures, err := evalrunner.LoadFixtures(*fixturesDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load fixtures:", err)
		os.Exit(1)
	}

	reviewer := claudecli.NewReviewer()
	exitCode := 0

	for _, f := range fixtures {
		if len(only) > 0 && !slices.Contains(only, f.Name) {
			continue
		}

		fmt.Printf("\n========== %s ==========\n", f.Name)
		result, err := evalrunner.Run(reviewer, f, *budget)
		if err != nil {
			fmt.Fprintln(os.Stderr, "["+f.Name+"] dựng fixture lỗi:", err)
			exitCode = 1
			continue
		}

		u := result.Usage
		fmt.Printf("(diff=%d ký tự, bundles=%d, input=%d, cache write=%d, cache read=%d, output=%d, cost=$%.4f)\n",
			len(result.Diff), len(result.Bundles), u.InputTokens, u.CacheCreationInputTokens,
			u.CacheReadInputTokens, u.OutputTokens, u.CostUSD)
		for i, b := range result.Bundles {
			fmt.Printf("\n--- Response bundle %d/%d (attempts=%d, num_turns=%d, cost=$%.4f) ---\n",
				i+1, len(result.Bundles), b.Stats.Attempts, b.Stats.NumTurns, b.Stats.Usage.CostUSD)
			if b.Err != nil {
				fmt.Fprintln(os.Stderr, "["+f.Name+"] review lỗi:", b.Err)
				exitCode = 1
				continue
			}
			fmt.Println(b.Response)
		}
		if result.Expected != "" {
			fmt.Println("--- Kỳ vọng (expected.md) — tự đối chiếu với Response ở trên ---")
			fmt.Println(result.Expected)
		}
	}

	os.Exit(exitCode)
}
