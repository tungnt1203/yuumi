package worker

import "context"

// Poller định kỳ gọi fn cho tới khi ctx bị cancel.
type Poller struct {
	fn func()
}

func NewPoller(fn func()) *Poller {
	return &Poller{fn: fn}
}

// Start chạy fn trong 1 goroutine riêng, dừng đúng lúc khi ctx bị cancel.
func (p *Poller) Start(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				p.fn()
			}
		}
	}()
}
