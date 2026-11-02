package benchmark

import (
	"context"
	"testing"
	"time"
)

func TestMonitorHistogramAndWindow(t *testing.T) {
	mon := NewMonitor(10)
	now := time.Now()
	for i := range 10 {
		mon.record(time.Duration(i+1)*time.Millisecond, now)
	}
	got := mon.Snapshot()
	if got.Completed != 10 || got.Total != 10 {
		t.Fatalf("completed/total = %d/%d", got.Completed, got.Total)
	}
	if got.E2EP50 <= 0 || got.E2EP95 < got.E2EP50 || got.E2EP95 > 32*time.Millisecond {
		t.Fatalf("histogram percentiles = %s %s", got.E2EP50, got.E2EP95)
	}
	if got.ThroughputWindow <= 0 {
		t.Fatal("expected windowed throughput")
	}
	if got.Goroutines < 1 || got.HeapBytes == 0 {
		t.Fatalf("resources = %d goroutines, %d heap", got.Goroutines, got.HeapBytes)
	}
}

func TestRunObservedPublishesProgress(t *testing.T) {
	cfg := Config{
		Strategy:   StrategySequential,
		Workload:   WorkloadCPU,
		Tasks:      5,
		Iterations: 20,
		Runs:       2,
	}
	mon := NewMonitor(cfg.Tasks * cfg.Runs)
	got, err := RunObserved(context.Background(), cfg, mon)
	if err != nil {
		t.Fatalf("RunObserved: %v", err)
	}
	if len(got.Runs) != 2 {
		t.Fatalf("runs = %d", len(got.Runs))
	}
	snap := mon.Snapshot()
	if snap.Completed != 10 {
		t.Fatalf("completed = %d, want 10", snap.Completed)
	}
	for _, run := range got.Runs {
		for _, task := range run.Tasks {
			if task.At < 0 {
				t.Fatalf("task %d at %s", task.ID, task.At)
			}
		}
	}
}
