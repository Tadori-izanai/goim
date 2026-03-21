package pkg

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics collects latency, throughput, and loss statistics for benchmarks.
type Metrics struct {
	latencies []int64 // nanoseconds
	sent      atomic.Int64
	received  atomic.Int64
	mu        sync.Mutex
	start     time.Time
	stopCh    chan struct{}
}

func NewMetrics() *Metrics {
	return &Metrics{
		start:  time.Now(),
		stopCh: make(chan struct{}),
	}
}

// RecordLatency records a latency sample. sendTsNano is the sender's time.Now().UnixNano().
func (m *Metrics) RecordLatency(sendTsNano int64) {
	lat := time.Now().UnixNano() - sendTsNano
	if lat < 0 {
		lat = 0
	}
	m.mu.Lock()
	m.latencies = append(m.latencies, lat)
	m.mu.Unlock()
	m.received.Add(1)
}

func (m *Metrics) IncSent()                 { m.sent.Add(1) }
func (m *Metrics) IncSentGroup(delta int64) { m.sent.Add(delta) }
func (m *Metrics) IncReceived()             { m.received.Add(1) }

func (m *Metrics) Sent() int64     { return m.sent.Load() }
func (m *Metrics) Received() int64 { return m.received.Load() }

// StartLiveReport prints throughput every interval until Stop is called.
func (m *Metrics) StartLiveReport(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		var lastRecv int64
		for {
			select {
			case <-ticker.C:
				recv := m.received.Load()
				sent := m.sent.Load()
				diff := recv - lastRecv
				lastRecv = recv
				rate := float64(diff) / interval.Seconds()
				fmt.Printf("  [live] sent=%d recv=%d  +%.0f msg/s\n", sent, recv, rate)
			case <-m.stopCh:
				return
			}
		}
	}()
}

// Stop stops the live reporter.
func (m *Metrics) Stop() {
	select {
	case <-m.stopCh:
	default:
		close(m.stopCh)
	}
}

// Report prints the final statistics.
func (m *Metrics) Report() {
	m.Stop()
	duration := time.Since(m.start).Seconds()
	sent := m.sent.Load()
	recv := m.received.Load()

	m.mu.Lock()
	lats := make([]int64, len(m.latencies))
	copy(lats, m.latencies)
	m.mu.Unlock()

	sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })

	var lossRate float64
	if sent > 0 {
		lossRate = float64(sent-recv) / float64(sent) * 100
	}
	throughput := float64(recv) / duration

	sep := strings.Repeat("═", 44)
	fmt.Println()
	fmt.Println(sep)
	fmt.Println("  Benchmark Report")
	fmt.Println(sep)
	fmt.Printf("  Duration:    %.1fs\n", duration)
	fmt.Printf("  Sent:        %d\n", sent)
	fmt.Printf("  Received:    %d\n", recv)
	fmt.Printf("  Loss:        %.2f%%\n", lossRate)
	fmt.Printf("  Throughput:  %.1f msg/s\n", throughput)

	if len(lats) > 0 {
		fmt.Println()
		fmt.Println("  Latency (ms):")
		fmt.Printf("    P50:   %.1f\n", percentile(lats, 0.50))
		fmt.Printf("    P95:   %.1f\n", percentile(lats, 0.95))
		fmt.Printf("    P99:   %.1f\n", percentile(lats, 0.99))
		fmt.Printf("    Max:   %.1f\n", nsToMs(lats[len(lats)-1]))
	}
	fmt.Println(sep)
}

// percentile returns the p-th percentile latency in milliseconds.
func percentile(sorted []int64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return nsToMs(sorted[idx])
}

func nsToMs(ns int64) float64 {
	return float64(ns) / 1e6
}
