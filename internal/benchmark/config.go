package benchmark

import (
	"fmt"
	"math"
)

const (
	// MaxTasks is the upper bound for one run and for tasks * runs together.
	MaxTasks = 100_000
	// MaxConcurrency is the upper bound for worker count and semaphore size.
	MaxConcurrency = 10_000
	// MaxRuns is the upper bound for repetitions of one config.
	MaxRuns = 20
	// MaxQueueSize is the upper bound for the worker-pool queue.
	// Other strategies ignore the field, but it is still capped.
	MaxQueueSize = 100_000
	// MaxIterations is the upper bound for CPU-bound work per task.
	MaxIterations = 100_000_000
	// MaxBytes is the upper bound for the CPU workload buffer.
	MaxBytes = 16 * 1024 * 1024
	// MaxTaskMS is the upper bound for one sleep task.
	MaxTaskMS = 60_000
)

// Strategy selects how tasks are scheduled.
type Strategy string

const (
	StrategySequential Strategy = "sequential"
	StrategyFanout     Strategy = "fanout"
	StrategySemaphore  Strategy = "semaphore"
	StrategyWorkerPool Strategy = "worker_pool"
)

// Workload selects the synthetic work each task performs.
type Workload string

const (
	// WorkloadCPU burns CPU for a fixed amount of work. Duration is an output.
	WorkloadCPU Workload = "cpu"
	// WorkloadSleep blocks for TaskMS and releases the processor.
	WorkloadSleep Workload = "sleep"
)

// ErrInvalidConfig is returned when a config cannot be run.
var ErrInvalidConfig = fmt.Errorf("invalid benchmark config")

// Config is the full input for one benchmark.
// QueueSize is used only by worker_pool. Concurrency is ignored by sequential
// and fanout.
type Config struct {
	Strategy    Strategy `json:"strategy"`
	Workload    Workload `json:"workload"`
	Tasks       int      `json:"tasks"`
	Concurrency int      `json:"concurrency,omitempty"`
	Iterations  int      `json:"iterations,omitempty"`
	Bytes       int      `json:"bytes,omitempty"`
	TaskMS      int      `json:"task_ms,omitempty"`
	QueueSize   int      `json:"queue_size,omitempty"`
	Runs        int      `json:"runs"`
}

// Validate checks the config against the engine rules and hard caps.
func (c Config) Validate() error {
	switch c.Strategy {
	case StrategySequential, StrategyFanout, StrategySemaphore, StrategyWorkerPool:
	default:
		return fmt.Errorf("%w: unknown strategy %q", ErrInvalidConfig, c.Strategy)
	}

	switch c.Workload {
	case WorkloadCPU, WorkloadSleep:
	default:
		return fmt.Errorf("%w: unknown workload %q", ErrInvalidConfig, c.Workload)
	}

	if c.Tasks < 1 || c.Tasks > MaxTasks {
		return fmt.Errorf("%w: tasks must be from 1 to %d", ErrInvalidConfig, MaxTasks)
	}
	if c.Runs < 1 || c.Runs > MaxRuns {
		return fmt.Errorf("%w: runs must be from 1 to %d", ErrInvalidConfig, MaxRuns)
	}
	if c.QueueSize < 0 || c.QueueSize > MaxQueueSize {
		return fmt.Errorf("%w: queue_size must be from 0 to %d", ErrInvalidConfig, MaxQueueSize)
	}
	if c.Iterations > MaxIterations {
		return fmt.Errorf("%w: iterations must be at most %d", ErrInvalidConfig, MaxIterations)
	}
	if c.Bytes > MaxBytes {
		return fmt.Errorf("%w: bytes must be at most %d", ErrInvalidConfig, MaxBytes)
	}
	if c.TaskMS > MaxTaskMS {
		return fmt.Errorf("%w: task_ms must be at most %d", ErrInvalidConfig, MaxTaskMS)
	}
	if err := c.sampleBudget(); err != nil {
		return err
	}

	switch c.Strategy {
	case StrategySemaphore, StrategyWorkerPool:
		if c.Concurrency < 1 || c.Concurrency > MaxConcurrency {
			return fmt.Errorf("%w: concurrency must be from 1 to %d", ErrInvalidConfig, MaxConcurrency)
		}
	default:
		if c.Concurrency < 0 || c.Concurrency > MaxConcurrency {
			return fmt.Errorf("%w: concurrency must be from 0 to %d", ErrInvalidConfig, MaxConcurrency)
		}
	}

	switch c.Workload {
	case WorkloadCPU:
		if c.Iterations < 0 || c.Bytes < 0 {
			return fmt.Errorf("%w: iterations and bytes must be >= 0", ErrInvalidConfig)
		}
		if c.Iterations < 1 && c.Bytes < 1 {
			return fmt.Errorf("%w: cpu workload requires iterations or bytes", ErrInvalidConfig)
		}
	case WorkloadSleep:
		if c.TaskMS < 1 {
			return fmt.Errorf("%w: sleep workload requires task_ms >= 1", ErrInvalidConfig)
		}
	}

	return nil
}

// sampleBudget rejects a tasks * runs product that overflows int or exceeds
// MaxTasks. The product sizes the progress monitor and the retained samples.
func (c Config) sampleBudget() error {
	if c.Runs > 0 && c.Tasks > math.MaxInt/c.Runs {
		return fmt.Errorf("%w: tasks * runs overflows", ErrInvalidConfig)
	}
	if c.Tasks*c.Runs > MaxTasks {
		return fmt.Errorf("%w: tasks * runs must be at most %d", ErrInvalidConfig, MaxTasks)
	}
	return nil
}
