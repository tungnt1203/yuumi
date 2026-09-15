package review

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDispatcher_LimitsConcurrency(t *testing.T) {
	const maxConcurrent = 3
	const totalJobs = maxConcurrent + 2

	d := NewDispatcher(maxConcurrent)

	var (
		current   atomic.Int64
		maxSeen   atomic.Int64
		completed atomic.Int64
		wg        sync.WaitGroup
	)

	wg.Add(totalJobs)
	for range totalJobs {
		d.Submit(func() {
			defer wg.Done()

			n := current.Add(1)
			for {
				old := maxSeen.Load()
				if n <= old || maxSeen.CompareAndSwap(old, n) {
					break
				}
			}

			// Giữ job "đang chạy" đủ lâu để các job khác kịp submit và bị
			// chặn ở semaphore, nếu không giới hạn concurrency sẽ không lộ ra.
			time.Sleep(20 * time.Millisecond)

			current.Add(-1)
			completed.Add(1)
		})
	}

	wg.Wait()

	if got := completed.Load(); got != totalJobs {
		t.Fatalf("completed = %d, want %d (all jobs should eventually run)", got, totalJobs)
	}
	if got := maxSeen.Load(); got > maxConcurrent {
		t.Errorf("observed max concurrent = %d, want <= %d", got, maxConcurrent)
	} else if got < 1 {
		t.Errorf("observed max concurrent = %d, want at least 1 (dispatcher never ran anything?)", got)
	}
}

func TestDispatcher_ZeroOrNegativeUsesDefault(t *testing.T) {
	for _, n := range []int{0, -1, -100} {
		d := NewDispatcher(n)
		if cap(d.sem) != defaultMaxConcurrentJobs {
			t.Errorf("NewDispatcher(%d): sem capacity = %d, want default %d", n, cap(d.sem), defaultMaxConcurrentJobs)
		}
	}
}

func TestDispatcher_SubmitDoesNotBlockCaller(t *testing.T) {
	// maxConcurrent=1 và job đầu tiên "chạy mãi" (chờ tín hiệu) — nếu Submit
	// tự nó block để chờ slot thì lần Submit thứ 2 sẽ treo test này.
	d := NewDispatcher(1)
	release := make(chan struct{})

	d.Submit(func() { <-release })

	done := make(chan struct{})
	go func() {
		d.Submit(func() {})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Submit blocked the caller instead of spawning a goroutine")
	}

	close(release)
}
