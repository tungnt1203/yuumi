// Package report xuất báo cáo doanh thu ra CSV cho bộ phận kế toán.
package report

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrEmptyRange trả về khi khoảng thời gian không hợp lệ.
var ErrEmptyRange = errors.New("report: from must be before to")

// Sale là 1 giao dịch bán hàng đã hoàn tất.
type Sale struct {
	OrderID  string
	SKU      string
	Quantity int64
	Revenue  int64 // đơn vị nhỏ nhất của tiền tệ
	Currency string
	At       time.Time
}

// DailyRow là tổng doanh thu của 1 SKU trong 1 ngày.
type DailyRow struct {
	Day      string // YYYY-MM-DD theo múi giờ của báo cáo
	SKU      string
	Orders   int
	Quantity int64
	Revenue  int64
	Currency string
}

// Aggregate gom sales trong [from, to) thành từng dòng theo (ngày, SKU,
// tiền tệ). Ngày tính theo loc để khớp giờ làm việc của kế toán. Kết quả
// sắp theo ngày rồi SKU để file CSV ổn định giữa các lần xuất.
func Aggregate(sales []Sale, from, to time.Time, loc *time.Location) ([]DailyRow, error) {
	if !from.Before(to) {
		return nil, ErrEmptyRange
	}
	if loc == nil {
		loc = time.UTC
	}
	type key struct{ day, sku, currency string }
	rows := map[key]*DailyRow{}
	orders := map[key]map[string]bool{}

	for _, s := range sales {
		if s.At.Before(from) || !s.At.Before(to) {
			continue
		}
		k := key{s.At.In(loc).Format("2006-01-02"), s.SKU, s.Currency}
		r, ok := rows[k]
		if !ok {
			r = &DailyRow{Day: k.day, SKU: k.sku, Currency: k.currency}
			rows[k] = r
			orders[k] = map[string]bool{}
		}
		r.Quantity += s.Quantity
		r.Revenue += s.Revenue
		orders[k][s.OrderID] = true
	}

	out := make([]DailyRow, 0, len(rows))
	for k, r := range rows {
		r.Orders = len(orders[k])
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Day != out[j].Day {
			return out[i].Day < out[j].Day
		}
		if out[i].SKU != out[j].SKU {
			return out[i].SKU < out[j].SKU
		}
		return out[i].Currency < out[j].Currency
	})
	return out, nil
}

// csvHeader là tiêu đề cột, giữ thứ tự cố định vì file được import vào
// phần mềm kế toán theo vị trí cột.
var csvHeader = []string{"day", "sku", "orders", "quantity", "revenue", "currency"}

// WriteCSV ghi rows ra w theo định dạng CSV chuẩn (RFC 4180). Ô bắt đầu
// bằng ký tự công thức (=, +, -, @) được thêm dấu ' ở đầu để Excel không
// chạy nó như công thức (CSV injection).
func WriteCSV(w io.Writer, rows []DailyRow) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(csvHeader); err != nil {
		return fmt.Errorf("report: write header: %w", err)
	}
	for _, r := range rows {
		record := []string{
			r.Day,
			safeCell(r.SKU),
			strconv.Itoa(r.Orders),
			strconv.FormatInt(r.Quantity, 10),
			strconv.FormatInt(r.Revenue, 10),
			safeCell(r.Currency),
		}
		if err := cw.Write(record); err != nil {
			return fmt.Errorf("report: write row %s/%s: %w", r.Day, r.SKU, err)
		}
	}
	cw.Flush()
	return cw.Error()
}

// safeCell chặn CSV injection cho ô text do người dùng nhập.
func safeCell(s string) string {
	if s != "" && strings.ContainsRune("=+-@", rune(s[0])) {
		return "'" + s
	}
	return s
}

// Totals cộng doanh thu theo từng tiền tệ, dùng cho dòng tổng cuối báo cáo.
func Totals(rows []DailyRow) map[string]int64 {
	out := map[string]int64{}
	for _, r := range rows {
		out[r.Currency] += r.Revenue
	}
	return out
}
