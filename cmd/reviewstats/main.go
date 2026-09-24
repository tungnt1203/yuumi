// Lệnh reviewstats đọc thư mục review log (xem reviewlog.FileLogger) và in
// tổng số lần gọi Claude CLI, token, chi phí theo repo và theo ngày (issue
// #63). Chạy tay trên máy chủ đang giữ log; không mở thành endpoint HTTP vì
// số liệu chi phí không nên lộ ra URL public của webhook server.
//
//	go run ./cmd/reviewstats                  # đọc $REVIEW_LOG_DIR hoặc logs/reviews
//	go run ./cmd/reviewstats -dir /var/log/yuumi -json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"text/tabwriter"
	"time"

	"github.com/tungnt1203/yuumi/internal/reviewlog"
)

func main() {
	dir := flag.String("dir", os.Getenv("REVIEW_LOG_DIR"), "thư mục review log (rỗng thì dùng mặc định của reviewlog)")
	asJSON := flag.Bool("json", false, "in kết quả dạng JSON")
	flag.Parse()

	s, err := reviewlog.Summarize(*dir, time.Local)
	if err != nil {
		fmt.Fprintln(os.Stderr, "reviewstats:", err)
		os.Exit(1)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(s); err != nil {
			fmt.Fprintln(os.Stderr, "reviewstats:", err)
			os.Exit(1)
		}
		return
	}

	printTable("Theo repo", s.ByRepo, s.Total)
	fmt.Println()
	printTable("Theo ngày", s.ByDay, s.Total)

	if s.Skipped > 0 {
		fmt.Fprintf(os.Stderr, "\n%d file log không đọc được, đã bỏ qua\n", s.Skipped)
	}
	if s.NoUsage > 0 {
		fmt.Fprintf(os.Stderr, "%d lần gọi ghi trước khi có usage, token/chi phí tính là 0\n", s.NoUsage)
	}
}

// printTable in 1 bảng: mỗi key 1 dòng (sắp theo key) và dòng tổng ở cuối.
func printTable(title string, groups map[string]reviewlog.Totals, total reviewlog.Totals) {
	fmt.Println(title)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "\tcalls\tinput\tcache write\tcache read\toutput\tcost (USD)")
	for _, key := range slices.Sorted(maps.Keys(groups)) {
		label := key
		if label == "" {
			label = "(không rõ)"
		}
		printRow(w, label, groups[key])
	}
	printRow(w, "TỔNG", total)
	w.Flush()
}

func printRow(w *tabwriter.Writer, label string, t reviewlog.Totals) {
	fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%.4f\n",
		label, t.Calls, t.InputTokens, t.CacheCreationInputTokens, t.CacheReadInputTokens, t.OutputTokens, t.CostUSD)
}
