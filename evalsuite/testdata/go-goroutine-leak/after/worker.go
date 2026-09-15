package worker

import "context"

// Poller định kỳ gọi fn cho tới khi ctx bị cancel.
type Poller struct {
	fn func()

	events chan string
}

func NewPoller(fn func()) *Poller {
	return &Poller{fn: fn, events: make(chan string)}
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

// Notify gửi 1 event tới goroutine nền để log lại, không chặn caller.
func (p *Poller) Notify(event string) {
	go func() {
		for {
			msg := <-p.events
			println(msg)
		}
	}()
	p.events <- event
}
