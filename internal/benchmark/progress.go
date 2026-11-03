package benchmark

import (
	"math"
	"math/bits"
	"runtime"
	"sync"
	"time"
)

const (
	throughputWindow = 200 * time.Millisecond
	histBuckets      = 40
)

// Progress is a point-in-time view of a running benchmark.
// E2E percentiles are histogram approximations, not the final sorted stats.
type Progress struct {
	Completed        int
	Total            int
	Elapsed          time.Duration
	ThroughputWindow float64
	E2EP50           time.Duration
	E2EP95           time.Duration
	Goroutines       int
	HeapBytes        uint64
}

// Monitor collects task completions so a caller can poll progress without
// waiting for the run to finish.
type Monitor struct {
	mu        sync.Mutex
	total     int
	started   time.Time
	completed int
	hist      histogram
	doneAt    []time.Time
}

// NewMonitor prepares progress for total tasks across every repetition.
func NewMonitor(total int) *Monitor {
	return &Monitor{
		total:   total,
		started: time.Now(),
	}
}

func (m *Monitor) record(e2e time.Duration, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completed++
	m.hist.add(e2e)
	m.doneAt = append(m.doneAt, at)
}

// Snapshot returns the current progress. ThroughputWindow is tasks per second
// over the last 200ms, or over the whole elapsed time when the run is younger.
func (m *Monitor) Snapshot() Progress {
	goroutines := runtime.NumGoroutine()
	heap := cachedHeap()

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.prune(now)
	elapsed := now.Sub(m.started)
	span := throughputWindow
	if elapsed < span {
		span = elapsed
	}
	rate := 0.0
	if span > 0 && len(m.doneAt) > 0 {
		rate = float64(len(m.doneAt)) / span.Seconds()
	}

	return Progress{
		Completed:        m.completed,
		Total:            m.total,
		Elapsed:          elapsed,
		ThroughputWindow: rate,
		E2EP50:           m.hist.percentile(50),
		E2EP95:           m.hist.percentile(95),
		Goroutines:       goroutines,
		HeapBytes:        heap,
	}
}

func (m *Monitor) prune(now time.Time) {
	cutoff := now.Add(-throughputWindow)
	drop := 0
	for drop < len(m.doneAt) && m.doneAt[drop].Before(cutoff) {
		drop++
	}
	if drop == 0 {
		return
	}
	copy(m.doneAt, m.doneAt[drop:])
	m.doneAt = m.doneAt[:len(m.doneAt)-drop]
}

// histogram is an exponential latency histogram in microseconds.
// Bucket i covers roughly [2^i, 2^(i+1)) microseconds.
type histogram struct {
	counts [histBuckets]uint64
	n      uint64
}

func (h *histogram) add(d time.Duration) {
	h.counts[bucket(d)]++
	h.n++
}

func (h *histogram) percentile(p float64) time.Duration {
	if h.n == 0 {
		return 0
	}
	target := uint64(math.Ceil((p / 100) * float64(h.n)))
	if target < 1 {
		target = 1
	}
	var cum uint64
	for i, count := range h.counts {
		cum += count
		if cum >= target {
			return bucketUpper(i)
		}
	}
	return bucketUpper(histBuckets - 1)
}

func bucket(d time.Duration) int {
	us := d.Microseconds()
	if us <= 1 {
		return 0
	}
	idx := bits.Len64(uint64(us)) - 1
	if idx >= histBuckets {
		return histBuckets - 1
	}
	return idx
}

func bucketUpper(i int) time.Duration {
	shift := i + 1
	if shift > 62 {
		shift = 62
	}
	return time.Duration(uint64(1)<<uint(shift)) * time.Microsecond
}
