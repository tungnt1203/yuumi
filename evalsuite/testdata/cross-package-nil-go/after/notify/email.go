// Package notify gửi email thông báo cho khách hàng. Nội dung được render
// bằng html/template để tự escape dữ liệu người dùng nhập.
package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/mail"
	"strings"
	"time"
)

// ErrInvalidRecipient trả về khi địa chỉ người nhận không hợp lệ.
var ErrInvalidRecipient = errors.New("notify: invalid recipient")

// Message là 1 email đã render xong, sẵn sàng gửi.
type Message struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

// Sender gửi 1 email. Implementation thật dùng SMTP; test dùng fake.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// Mailer render và gửi các email thông báo, có retry cho lỗi tạm thời.
type Mailer struct {
	sender     Sender
	from       string
	maxRetries int
	backoff    time.Duration
	sleep      func(context.Context, time.Duration) error
}

// NewMailer tạo Mailer gửi qua sender, với địa chỉ gửi from.
func NewMailer(sender Sender, from string) *Mailer {
	return &Mailer{sender: sender, from: from, maxRetries: 3, backoff: 500 * time.Millisecond, sleep: sleepCtx}
}

var welcomeTmpl = template.Must(template.New("welcome").Parse(`<!doctype html>
<html><body>
<p>Xin chào {{.Name}},</p>
<p>Tài khoản của bạn đã được tạo lúc {{.CreatedAt.Format "15:04 02/01/2006"}}.</p>
{{if .VerifyURL}}<p><a href="{{.VerifyURL}}">Xác nhận email</a></p>{{end}}
<p>— Đội ngũ Shop</p>
</body></html>`))

// WelcomeData là dữ liệu cho email chào mừng.
type WelcomeData struct {
	Name      string
	Email     string
	CreatedAt time.Time
	VerifyURL string
}

// RenderWelcome render email chào mừng. Text là bản thuần văn bản cho
// client không hiển thị HTML.
func RenderWelcome(d WelcomeData) (Message, error) {
	addr, err := mail.ParseAddress(d.Email)
	if err != nil {
		return Message{}, fmt.Errorf("%w: %v", ErrInvalidRecipient, err)
	}
	var html bytes.Buffer
	if err := welcomeTmpl.Execute(&html, d); err != nil {
		return Message{}, fmt.Errorf("notify: render welcome: %w", err)
	}
	var text strings.Builder
	fmt.Fprintf(&text, "Xin chào %s,\n\nTài khoản của bạn đã được tạo lúc %s.\n",
		d.Name, d.CreatedAt.Format("15:04 02/01/2006"))
	if d.VerifyURL != "" {
		fmt.Fprintf(&text, "Xác nhận email: %s\n", d.VerifyURL)
	}
	text.WriteString("\n— Đội ngũ Shop\n")
	return Message{
		To:      addr.Address,
		Subject: "Chào mừng bạn đến với Shop",
		HTML:    html.String(),
		Text:    text.String(),
	}, nil
}

// TemporaryError đánh dấu lỗi gửi có thể thử lại (vd SMTP 4xx).
type TemporaryError struct{ Err error }

func (e *TemporaryError) Error() string { return "notify: temporary: " + e.Err.Error() }
func (e *TemporaryError) Unwrap() error { return e.Err }

// Send gửi m, thử lại tối đa maxRetries lần với backoff tăng gấp đôi khi
// lỗi là TemporaryError. Lỗi khác hoặc ctx bị huỷ thì dừng ngay.
func (m *Mailer) Send(ctx context.Context, msg Message) error {
	wait := m.backoff
	var lastErr error
	for attempt := 0; attempt <= m.maxRetries; attempt++ {
		if attempt > 0 {
			if err := m.sleep(ctx, wait); err != nil {
				return err
			}
			wait *= 2
		}
		err := m.sender.Send(ctx, msg)
		if err == nil {
			return nil
		}
		var tmp *TemporaryError
		if !errors.As(err, &tmp) {
			return err
		}
		lastErr = err
	}
	return fmt.Errorf("notify: giving up after %d retries: %w", m.maxRetries, lastErr)
}

// sleepCtx chờ d, trả lỗi của ctx nếu bị huỷ trước khi hết thời gian.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
