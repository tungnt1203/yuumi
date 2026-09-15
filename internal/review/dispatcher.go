package review

// defaultMaxConcurrentJobs là số job tối đa chạy đồng thời khi Dispatcher
// không được cấu hình maxConcurrent (<=0). Mỗi job có thể spawn tới vài
// process `git`/`claude` thật (clone + review từng bundle), nên cần chặn
// trên tránh 1 đợt webhook dồn dập làm quá tải máy chạy bot.
const defaultMaxConcurrentJobs = 3

// Dispatcher chạy các hàm (thường là Job.Run) trong goroutine riêng, giới
// hạn số lượng chạy đồng thời bằng 1 semaphore.
//
// Submit nhận func() thay vì *Job để có thể test Dispatcher độc lập bằng
// closure đơn giản, không cần fake GitHubClient/Reviewer/Cloner.
type Dispatcher struct {
	sem chan struct{}
}

// NewDispatcher tạo Dispatcher cho phép tối đa maxConcurrent job chạy cùng
// lúc. maxConcurrent <=0 dùng defaultMaxConcurrentJobs.
func NewDispatcher(maxConcurrent int) *Dispatcher {
	if maxConcurrent <= 0 {
		maxConcurrent = defaultMaxConcurrentJobs
	}
	return &Dispatcher{sem: make(chan struct{}, maxConcurrent)}
}

// Submit chạy fn trong 1 goroutine mới. Submit tự nó không bao giờ block
// (chỉ spawn goroutine rồi return ngay) — việc chờ đến khi có "slot" trống
// diễn ra bên trong goroutine đó, nên caller (vd HTTP handler xử lý
// webhook) luôn phản hồi nhanh dù hàng đợi job đang đầy.
func (d *Dispatcher) Submit(fn func()) {
	go func() {
		d.sem <- struct{}{}
		defer func() { <-d.sem }()
		fn()
	}()
}
