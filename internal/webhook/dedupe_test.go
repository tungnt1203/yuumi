package webhook

import (
	"sync"
	"testing"
)

func TestSeenComments_MarkIfNew_FirstTimeTrueThenFalse(t *testing.T) {
	s := NewSeenComments()

	if !s.MarkIfNew(1) {
		t.Error("first MarkIfNew(1) = false, want true")
	}
	if s.MarkIfNew(1) {
		t.Error("second MarkIfNew(1) = true, want false (duplicate)")
	}
}

func TestSeenComments_DifferentIDsAreIndependent(t *testing.T) {
	s := NewSeenComments()

	if !s.MarkIfNew(1) || !s.MarkIfNew(2) || !s.MarkIfNew(3) {
		t.Error("different IDs should each be new the first time")
	}
	if s.MarkIfNew(1) || s.MarkIfNew(2) {
		t.Error("previously marked IDs should stay marked")
	}
}

// TestSeenComments_ConcurrentSameID mô phỏng đúng kịch bản webhook redeliver
// gần như đồng thời: chỉ đúng 1 goroutine được nhận "true" cho cùng 1 ID,
// dù nhiều goroutine gọi MarkIfNew cùng lúc. Chạy với -race để bắt data race
// thật nếu implementation không lock đúng.
func TestSeenComments_ConcurrentSameID(t *testing.T) {
	s := NewSeenComments()
	const attempts = 50

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		trueCount int
	)

	wg.Add(attempts)
	for range attempts {
		go func() {
			defer wg.Done()
			if s.MarkIfNew(42) {
				mu.Lock()
				trueCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if trueCount != 1 {
		t.Errorf("trueCount = %d, want exactly 1 (only the first caller should see 'new')", trueCount)
	}
}
