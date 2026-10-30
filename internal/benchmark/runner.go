// Package benchmark runs one synthetic workload under a single concurrency
// strategy and records latency, throughput, and resource usage.
//
// The process-wide rule that only one benchmark may run at a time belongs to
// the HTTP server. This package stays reentrant so tests can call it directly.
package benchmark

import (
	"context"
	"time"
)

// Run executes cfg.Runs repetitions. On cancellation the result includes
// whatever repetitions and tasks finished, and the error is ctx.Err().
func Run(ctx context.Context, cfg Config) (Result, error) {
	if err := cfg.Validate(); err != nil {
		return Result{}, err
	}

	result := Result{
		Environment: currentEnvironment(),
		Config:      cfg,
		Runs:        make([]RunResult, 0, cfg.Runs),
	}
	for range cfg.Runs {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		run, err := execute(ctx, cfg)
		result.Runs = append(result.Runs, run)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func execute(ctx context.Context, cfg Config) (RunResult, error) {
	rec := &recorder{}
	sampler := startSampler()
	work := cfg.newWork()

	started := time.Now()
	err := dispatch(ctx, cfg, work, rec)
	duration := time.Since(started)
	resources := sampler.stop()

	return summarize(rec.snapshot(), duration, resources), err
}

func dispatch(ctx context.Context, cfg Config, work workFunc, rec *recorder) error {
	switch cfg.Strategy {
	case StrategySequential:
		return runSequential(ctx, cfg, work, rec)
	case StrategyFanout:
		return runFanout(ctx, cfg, work, rec)
	case StrategySemaphore:
		return runSemaphore(ctx, cfg, work, rec)
	case StrategyWorkerPool:
		return runWorkerPool(ctx, cfg, work, rec)
	default:
		return ErrInvalidConfig
	}
}

func summarize(items []observation, wall time.Duration, resources Resources) RunResult {
	tasks := make([]TaskResult, len(items))
	e2e := make([]time.Duration, len(items))
	service := make([]time.Duration, len(items))
	for i, item := range items {
		e2e[i] = item.end.Sub(item.submit)
		service[i] = item.end.Sub(item.start)
		tasks[i] = TaskResult{ID: item.id, E2E: e2e[i], Service: service[i]}
	}

	throughput := 0.0
	if wall > 0 && len(items) > 0 {
		throughput = float64(len(items)) / wall.Seconds()
	}

	return RunResult{
		TasksCompleted: len(items),
		Duration:       wall,
		Throughput:     throughput,
		E2E:            latencyStats(e2e),
		Service:        latencyStats(service),
		Resources:      resources,
		Tasks:          tasks,
	}
}
