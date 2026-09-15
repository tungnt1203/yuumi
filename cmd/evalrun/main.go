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
		result, err := evalrunner.Run(reviewer, f)
		if err != nil {
			fmt.Fprintln(os.Stderr, "["+f.Name+"] review lỗi:", err)
			exitCode = 1
			continue
		}

		fmt.Printf("(attempts=%d, num_turns=%d)\n\n", result.Attempts, result.NumTurns)
		fmt.Println("--- Response ---")
		fmt.Println(result.Response)
		if result.Expected != "" {
			fmt.Println("--- Kỳ vọng (expected.md) — tự đối chiếu với Response ở trên ---")
			fmt.Println(result.Expected)
		}
	}

	os.Exit(exitCode)
}
