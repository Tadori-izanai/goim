package gateway

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/Terry-Mao/goim/internal/gateway/conf"
)

func testAckService(retryInterval, maxRetries int) *ackService {
	return newAckService(&conf.ACK{
		RetryInterval: retryInterval,
		MaxRetries:    maxRetries,
	})
}

func TestAck_ImmediateAck(t *testing.T) {
	as := testAckService(1, 3)
	var pushCount atomic.Int32
	push := func(op int32, mid int64, body []byte) {
		pushCount.Add(1)
	}

	done := make(chan struct{})
	go func() {
		as.Track("msg1", 100, 2001, []byte("hello"), push)
		close(done)
	}()

	// Ack immediately
	time.Sleep(10 * time.Millisecond)
	as.removePending("msg1")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Track goroutine did not exit after Ack")
	}

	if pushCount.Load() != 0 {
		t.Fatalf("expected 0 retries, got %d", pushCount.Load())
	}

	// pending should be cleaned up
	as.mu.Lock()
	defer as.mu.Unlock()

	if len(as.pending) != 0 {
		t.Fatalf("expected empty pending, got %d", len(as.pending))
	}
	if len(as.midMap) != 0 {
		t.Fatalf("expected empty midMap, got %d", len(as.midMap))
	}
}

func TestAck_RetriesAndGivesUp(t *testing.T) {
	// Short interval for fast test
	as := testAckService(1, 3)
	var pushCount atomic.Int32
	push := func(op int32, mid int64, body []byte) {
		pushCount.Add(1)
	}

	done := make(chan struct{})
	go func() {
		as.Track("msg2", 200, 2001, []byte("hello"), push)
		close(done)
	}()

	// Wait for all retries to exhaust
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Track goroutine did not exit after max retries")
	}

	// MaxRetries=3: first push is in SendMessage, Track retries MaxRetries-1=2 times
	if pushCount.Load() != 2 {
		t.Fatalf("expected 2 retries, got %d", pushCount.Load())
	}

	// pending should be cleaned up
	as.mu.Lock()
	defer as.mu.Unlock()
	if len(as.pending) != 0 {
		t.Fatalf("expected empty pending after max retries, got %d", len(as.pending))
	}
}

func TestAck_AckDuringRetry(t *testing.T) {
	as := testAckService(1, 5)
	var pushCount atomic.Int32
	push := func(op int32, mid int64, body []byte) {
		pushCount.Add(1)
	}

	done := make(chan struct{})
	go func() {
		as.Track("msg3", 300, 2001, []byte("hello"), push)
		close(done)
	}()

	// Wait for first retry, then Ack
	time.Sleep(1500 * time.Millisecond)
	as.removePending("msg3")

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Track goroutine did not exit after Ack")
	}

	// Should have retried once before Ack
	if pushCount.Load() < 1 {
		t.Fatalf("expected at least 1 retry before Ack, got %d", pushCount.Load())
	}
}

func TestAck_UserOffline(t *testing.T) {
	as := testAckService(10, 5) // long interval so timers don't fire
	push := func(op int32, mid int64, body []byte) {}

	done1 := make(chan struct{})
	done2 := make(chan struct{})
	go func() {
		as.Track("msg4", 400, 2001, []byte("a"), push)
		close(done1)
	}()
	go func() {
		as.Track("msg5", 400, 2001, []byte("b"), push)
		close(done2)
	}()

	time.Sleep(20 * time.Millisecond)

	// Both should be pending
	as.mu.Lock()
	if len(as.pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(as.pending))
	}
	as.mu.Unlock()

	// UserOffline clears all for mid=400
	as.clearMid(400)

	select {
	case <-done1:
	case <-time.After(2 * time.Second):
		t.Fatal("Track goroutine 1 did not exit after UserOffline")
	}
	select {
	case <-done2:
	case <-time.After(2 * time.Second):
		t.Fatal("Track goroutine 2 did not exit after UserOffline")
	}

	as.mu.Lock()
	defer as.mu.Unlock()
	if len(as.pending) != 0 {
		t.Fatalf("expected empty pending, got %d", len(as.pending))
	}
	if len(as.midMap) != 0 {
		t.Fatalf("expected empty midMap, got %d", len(as.midMap))
	}
}

func TestAck_DoubleAckNoPanic(t *testing.T) {
	as := testAckService(10, 3)
	push := func(op int32, mid int64, body []byte) {}

	go as.Track("msg6", 500, 2001, []byte("x"), push)
	time.Sleep(10 * time.Millisecond)

	// Double Ack should not panic
	as.removePending("msg6")
	as.removePending("msg6")
}

func TestAck_AckNonexistentNoPanic(t *testing.T) {
	as := testAckService(1, 3)
	// Ack a message that was never tracked
	as.removePending("nonexistent")
}

func TestAck_UserOfflineNonexistentNoPanic(t *testing.T) {
	as := testAckService(1, 3)
	// UserOffline for a mid that has no pending
	as.clearMid(999)
}

func TestAck_MultipleMids(t *testing.T) {
	as := testAckService(10, 5)
	push := func(op int32, mid int64, body []byte) {}

	done1 := make(chan struct{})
	done2 := make(chan struct{})
	go func() {
		as.Track("msg7", 600, 2001, []byte("a"), push)
		close(done1)
	}()
	go func() {
		as.Track("msg8", 700, 2001, []byte("b"), push)
		close(done2)
	}()

	time.Sleep(20 * time.Millisecond)

	// UserOffline for mid=600 should only clear msg7
	as.clearMid(600)

	select {
	case <-done1:
	case <-time.After(2 * time.Second):
		t.Fatal("Track for mid=600 did not exit")
	}

	// msg8 (mid=700) should still be pending
	as.mu.Lock()
	if _, exists := as.pending["msg8"]; !exists {
		t.Fatal("expected msg8 still pending")
	}
	as.mu.Unlock()

	// Clean up
	as.removePending("msg8")
	<-done2
}
