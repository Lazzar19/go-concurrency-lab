package benchmark

import "fmt"

const (
	// MaxTasks is the upper bound enforced before a run starts.
	MaxTasks = 100_000
	// MaxConcurrency is the upper bound for worker count and semaphore size.
	MaxConcurrency = 10_000
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
	if c.Runs < 1 {
		return fmt.Errorf("%w: runs must be positive", ErrInvalidConfig)
	}
	if c.QueueSize < 0 {
		return fmt.Errorf("%w: queue_size must be >= 0", ErrInvalidConfig)
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
