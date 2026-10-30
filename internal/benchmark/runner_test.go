package benchmark

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestStrategiesCompleteWithoutDuplicates(t *testing.T) {
	strategies := []Strategy{
		StrategySequential,
		StrategyFanout,
		StrategySemaphore,
		StrategyWorkerPool,
	}
	queueSizes := []int{0, 32}

	for _, strategy := range strategies {
		for _, queueSize := range queueSizes {
			if strategy != StrategyWorkerPool && queueSize != 0 {
				continue
			}
			name := string(strategy)
			if strategy == StrategyWorkerPool {
				name = name + "/queue_" + strconv.Itoa(queueSize)
			}
			t.Run(name, func(t *testing.T) {
				cfg := Config{
					Strategy:    strategy,
					Workload:    WorkloadCPU,
					Tasks:       40,
					Concurrency: 4,
					Iterations:  30,
					Bytes:       32,
					QueueSize:   queueSize,
					Runs:        2,
				}
				got, err := Run(context.Background(), cfg)
				if err != nil {
					t.Fatalf("Run: %v", err)
				}
				assertResult(t, cfg, got)
			})
		}
	}
}

func TestSleepWorkload(t *testing.T) {
	cfg := Config{
		Strategy:    StrategySemaphore,
		Workload:    WorkloadSleep,
		Tasks:       6,
		Concurrency: 3,
		TaskMS:      5,
		Runs:        1,
	}
	got, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertResult(t, cfg, got)
}

func TestCPUBytesOnly(t *testing.T) {
	cfg := Config{
		Strategy: StrategyFanout,
		Workload: WorkloadCPU,
		Tasks:    8,
		Bytes:    64,
		Runs:     1,
	}
	got, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertResult(t, cfg, got)
}

func TestCancelStopsRemainingTasks(t *testing.T) {
	cfg := Config{
		Strategy:    StrategyWorkerPool,
		Workload:    WorkloadSleep,
		Tasks:       20,
		Concurrency: 1,
		TaskMS:      400,
		QueueSize:   0,
		Runs:        1,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	got, err := Run(ctx, cfg)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if len(got.Runs) != 1 {
		t.Fatalf("runs = %d, want 1 partial run", len(got.Runs))
	}
	if got.Runs[0].TasksCompleted >= cfg.Tasks {
		t.Fatalf("completed = %d, want fewer than %d", got.Runs[0].TasksCompleted, cfg.Tasks)
	}
	assertUniqueTasks(t, got.Runs[0], cfg.Tasks)
}

func TestValidateRejectsBadConfig(t *testing.T) {
	cases := []Config{
		{Strategy: "nope", Workload: WorkloadCPU, Tasks: 1, Iterations: 1, Runs: 1},
		{Strategy: StrategySequential, Workload: WorkloadCPU, Tasks: 0, Iterations: 1, Runs: 1},
		{Strategy: StrategySequential, Workload: WorkloadCPU, Tasks: MaxTasks + 1, Iterations: 1, Runs: 1},
		{Strategy: StrategyWorkerPool, Workload: WorkloadCPU, Tasks: 1, Iterations: 1, Concurrency: 0, Runs: 1},
		{Strategy: StrategySemaphore, Workload: WorkloadCPU, Tasks: 1, Iterations: 1, Concurrency: MaxConcurrency + 1, Runs: 1},
		{Strategy: StrategySequential, Workload: WorkloadCPU, Tasks: 1, Runs: 1},
		{Strategy: StrategySequential, Workload: WorkloadSleep, Tasks: 1, TaskMS: 0, Runs: 1},
		{Strategy: StrategySequential, Workload: WorkloadCPU, Tasks: 1, Iterations: 1, Runs: 0},
		{Strategy: StrategyWorkerPool, Workload: WorkloadCPU, Tasks: 1, Iterations: 1, Concurrency: 1, QueueSize: -1, Runs: 1},
	}
	for _, cfg := range cases {
		if err := cfg.Validate(); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("Validate(%+v) = %v, want ErrInvalidConfig", cfg, err)
		}
	}
}

func TestLatencyStatsOrder(t *testing.T) {
	stats := latencyStats([]time.Duration{5, 1, 4, 2, 3})
	if stats.Min != 1 || stats.Max != 5 {
		t.Fatalf("min/max = %s/%s", stats.Min, stats.Max)
	}
	if !(stats.Min <= stats.P50 && stats.P50 <= stats.P90 && stats.P90 <= stats.P95 && stats.P95 <= stats.P99 && stats.P99 <= stats.Max) {
		t.Fatalf("percentiles out of order: %+v", stats)
	}
}

func assertResult(t *testing.T, cfg Config, got Result) {
	t.Helper()
	if got.Environment.GoVersion == "" || got.Environment.GOMAXPROCS < 1 || got.Environment.NumCPU < 1 {
		t.Fatalf("environment = %+v", got.Environment)
	}
	if len(got.Runs) != cfg.Runs {
		t.Fatalf("runs = %d, want %d", len(got.Runs), cfg.Runs)
	}
	for _, run := range got.Runs {
		if run.TasksCompleted != cfg.Tasks {
			t.Fatalf("completed = %d, want %d", run.TasksCompleted, cfg.Tasks)
		}
		if run.Duration <= 0 || run.Throughput <= 0 {
			t.Fatalf("duration %s throughput %v", run.Duration, run.Throughput)
		}
		if run.Resources.GoroutinesPeak < 1 || run.Resources.HeapPeak == 0 {
			t.Fatalf("resources = %+v", run.Resources)
		}
		assertUniqueTasks(t, run, cfg.Tasks)
		if run.E2E.P99 < run.E2E.P50 || run.Service.P99 < run.Service.P50 {
			t.Fatalf("percentiles regressed: e2e %+v service %+v", run.E2E, run.Service)
		}
	}
}

func assertUniqueTasks(t *testing.T, run RunResult, tasks int) {
	t.Helper()
	seen := make(map[int]struct{}, len(run.Tasks))
	for _, task := range run.Tasks {
		if task.ID < 0 || task.ID >= tasks {
			t.Fatalf("task id %d out of range", task.ID)
		}
		if _, ok := seen[task.ID]; ok {
			t.Fatalf("duplicate task id %d", task.ID)
		}
		seen[task.ID] = struct{}{}
		if task.E2E < task.Service {
			t.Fatalf("task %d e2e %s < service %s", task.ID, task.E2E, task.Service)
		}
	}
}
