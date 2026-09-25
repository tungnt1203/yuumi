// Package billing tính tiền hoá đơn. Mọi số tiền dùng đơn vị nhỏ nhất của
// tiền tệ (VND không có số lẻ, USD tính theo cent) và kiểu int64 để tránh
// sai số của số thực.
package billing

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Các lỗi kiểm tra đầu vào của hoá đơn.
var (
	ErrNoLines          = errors.New("billing: invoice has no lines")
	ErrNegativeQuantity = errors.New("billing: quantity must be positive")
	ErrNegativePrice    = errors.New("billing: unit price must not be negative")
	ErrMixedCurrency    = errors.New("billing: lines use different currencies")
	ErrDiscountTooLarge = errors.New("billing: discount exceeds subtotal")
)

// Line là 1 dòng hàng trong hoá đơn.
type Line struct {
	SKU       string
	Name      string
	Quantity  int64
	UnitPrice int64 // đơn vị nhỏ nhất của tiền tệ
	Currency  string
	Taxable   bool
}

// Amount trả thành tiền của dòng, chưa thuế, chưa giảm giá.
func (l Line) Amount() int64 {
	return l.Quantity * l.UnitPrice
}

// Discount là giảm giá áp lên cả hoá đơn: Percent (0-100) hoặc Fixed, không
// dùng cả hai cùng lúc.
type Discount struct {
	Code    string
	Percent int64
	Fixed   int64
}

// Invoice là hoá đơn đã tính xong.
type Invoice struct {
	Number    string
	IssuedAt  time.Time
	Currency  string
	Lines     []Line
	Subtotal  int64
	Discount  int64
	Tax       int64
	Total     int64
	TaxRateBP int64 // thuế suất theo basis point: 1000 = 10%
}

// Calculator tính hoá đơn với 1 thuế suất cố định.
type Calculator struct {
	// TaxRateBP là thuế suất theo basis point (1/100 của 1%), vd 1000 = 10%.
	TaxRateBP int64
	now       func() time.Time
}

// NewCalculator tạo Calculator với thuế suất cho trước.
func NewCalculator(taxRateBP int64) *Calculator {
	return &Calculator{TaxRateBP: taxRateBP, now: time.Now}
}

// Build kiểm tra các dòng hàng rồi tính subtotal, giảm giá, thuế và tổng.
// Giảm giá được phân bổ trước khi tính thuế, chỉ trên phần hàng chịu thuế
// theo đúng tỉ lệ của nó trong subtotal.
func (c *Calculator) Build(number string, lines []Line, discount *Discount) (Invoice, error) {
	if len(lines) == 0 {
		return Invoice{}, ErrNoLines
	}
	currency := lines[0].Currency
	var subtotal, taxable int64
	for _, l := range lines {
		if l.Quantity <= 0 {
			return Invoice{}, fmt.Errorf("%w: sku %s", ErrNegativeQuantity, l.SKU)
		}
		if l.UnitPrice < 0 {
			return Invoice{}, fmt.Errorf("%w: sku %s", ErrNegativePrice, l.SKU)
		}
		if l.Currency != currency {
			return Invoice{}, ErrMixedCurrency
		}
		subtotal += l.Amount()
		if l.Taxable {
			taxable += l.Amount()
		}
	}

	off, err := discountAmount(subtotal, discount)
	if err != nil {
		return Invoice{}, err
	}

	// Phần giảm giá rơi vào hàng chịu thuế, theo tỉ lệ taxable/subtotal.
	var taxableOff int64
	if subtotal > 0 {
		taxableOff = off * taxable / subtotal
	}
	tax := roundHalfUp((taxable-taxableOff)*c.TaxRateBP, 10_000)

	sorted := make([]Line, len(lines))
	copy(sorted, lines)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].SKU < sorted[j].SKU })

	return Invoice{
		Number:    number,
		IssuedAt:  c.now(),
		Currency:  currency,
		Lines:     sorted,
		Subtotal:  subtotal,
		Discount:  off,
		Tax:       tax,
		Total:     subtotal - off + tax,
		TaxRateBP: c.TaxRateBP,
	}, nil
}

// discountAmount tính số tiền được giảm. Không có mã giảm giá thì trả 0.
func discountAmount(subtotal int64, d *Discount) (int64, error) {
	if d == nil {
		return 0, nil
	}
	var off int64
	switch {
	case d.Percent > 0:
		if d.Percent > 100 {
			return 0, ErrDiscountTooLarge
		}
		off = roundHalfUp(subtotal*d.Percent, 100)
	case d.Fixed > 0:
		off = d.Fixed
	}
	if off > subtotal {
		return 0, ErrDiscountTooLarge
	}
	return off, nil
}

// roundHalfUp chia n cho d rồi làm tròn nửa lên. n và d đều không âm.
func roundHalfUp(n, d int64) int64 {
	return (n + d/2) / d
}

// Summary render hoá đơn thành vài dòng text cho email xác nhận.
func (inv Invoice) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Hoá đơn %s (%s)\n", inv.Number, inv.IssuedAt.Format("2006-01-02"))
	for _, l := range inv.Lines {
		fmt.Fprintf(&b, "  %-10s %-24s x%d  %d %s\n", l.SKU, l.Name, l.Quantity, l.Amount(), inv.Currency)
	}
	fmt.Fprintf(&b, "Tạm tính: %d %s\n", inv.Subtotal, inv.Currency)
	if inv.Discount > 0 {
		fmt.Fprintf(&b, "Giảm giá: -%d %s\n", inv.Discount, inv.Currency)
	}
	fmt.Fprintf(&b, "Thuế: %d %s\n", inv.Tax, inv.Currency)
	fmt.Fprintf(&b, "Tổng cộng: %d %s\n", inv.Total, inv.Currency)
	return b.String()
}
