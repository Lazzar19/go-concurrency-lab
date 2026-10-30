package benchmark

import (
	"math"
	"runtime"
	"sort"
	"sync"
	"time"
)

// Environment is the runtime context required to interpret a result.
type Environment struct {
	GoVersion  string `json:"go_version"`
	GOMAXPROCS int    `json:"gomaxprocs"`
	NumCPU     int    `json:"num_cpu"`
}

// LatencyStats is one distribution. Percentiles are computed from that run's
// samples only and must not be averaged across runs.
type LatencyStats struct {
	Min time.Duration `json:"min"`
	Max time.Duration `json:"max"`
	P50 time.Duration `json:"p50"`
	P90 time.Duration `json:"p90"`
	P95 time.Duration `json:"p95"`
	P99 time.Duration `json:"p99"`
}

// Resources tracks goroutines and heap around a single run.
type Resources struct {
	GoroutinesStart int    `json:"goroutines_start"`
	GoroutinesEnd   int    `json:"goroutines_end"`
	GoroutinesPeak  int    `json:"goroutines_peak"`
	HeapStart       uint64 `json:"heap_start"`
	HeapEnd         uint64 `json:"heap_end"`
	HeapPeak        uint64 `json:"heap_peak"`
}

// TaskResult is one finished task.
// E2E is submission to completion, including queue wait.
// Service is the work function only.
type TaskResult struct {
	ID      int           `json:"id"`
	E2E     time.Duration `json:"e2e"`
	Service time.Duration `json:"service"`
}

// RunResult is one repetition. Runs are kept separate.
type RunResult struct {
	TasksCompleted int           `json:"tasks_completed"`
	Duration       time.Duration `json:"duration"`
	Throughput     float64       `json:"throughput"`
	E2E            LatencyStats  `json:"e2e"`
	Service        LatencyStats  `json:"service"`
	Resources      Resources     `json:"resources"`
	Tasks          []TaskResult  `json:"tasks"`
}

// Result is the outcome of Config.Runs repetitions.
type Result struct {
	Environment Environment `json:"environment"`
	Config      Config      `json:"config"`
	Runs        []RunResult `json:"runs"`
}

func currentEnvironment() Environment {
	return Environment{
		GoVersion:  runtime.Version(),
		GOMAXPROCS: runtime.GOMAXPROCS(0),
		NumCPU:     runtime.NumCPU(),
	}
}

func latencyStats(samples []time.Duration) LatencyStats {
	if len(samples) == 0 {
		return LatencyStats{}
	}
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return LatencyStats{
		Min: sorted[0],
		Max: sorted[len(sorted)-1],
		P50: percentile(sorted, 50),
		P90: percentile(sorted, 90),
		P95: percentile(sorted, 95),
		P99: percentile(sorted, 99),
	}
}

// percentile uses the nearest-rank method on an ascending slice.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	rank := int(math.Ceil((p/100)*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

type sampler struct {
	mu     sync.Mutex
	stopCh chan struct{}
	done   chan struct{}
	gStart int
	gEnd   int
	gPeak  int
	hStart uint64
	hEnd   uint64
	hPeak  uint64
}

func startSampler() *sampler {
	g, h := readResources()
	s := &sampler{
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
		gStart: g,
		gPeak:  g,
		hStart: h,
		hPeak:  h,
	}
	go s.loop()
	return s
}

func (s *sampler) loop() {
	defer close(s.done)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	ticks := 0
	for {
		select {
		case <-s.stopCh:
			s.record(true)
			return
		case <-ticker.C:
			ticks++
			// NumGoroutine is cheap. ReadMemStats stops the world, so heap is
			// sampled less often than goroutines.
			if ticks%5 == 0 {
				s.record(true)
			} else {
				s.record(false)
			}
		}
	}
}

func (s *sampler) record(includeHeap bool) {
	g := runtime.NumGoroutine()
	s.mu.Lock()
	defer s.mu.Unlock()
	if g > s.gPeak {
		s.gPeak = g
	}
	if includeHeap {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		if ms.HeapAlloc > s.hPeak {
			s.hPeak = ms.HeapAlloc
		}
		s.hEnd = ms.HeapAlloc
	}
	s.gEnd = g
}

func (s *sampler) stop() Resources {
	close(s.stopCh)
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return Resources{
		GoroutinesStart: s.gStart,
		GoroutinesEnd:   s.gEnd,
		GoroutinesPeak:  s.gPeak,
		HeapStart:       s.hStart,
		HeapEnd:         s.hEnd,
		HeapPeak:        s.hPeak,
	}
}

func readResources() (goroutines int, heap uint64) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return runtime.NumGoroutine(), ms.HeapAlloc
}
