package benchmark

import (
	"context"
	"crypto/sha256"
	"runtime"
	"time"
)

type workFunc func(ctx context.Context)

func (c Config) newWork() workFunc {
	switch c.Workload {
	case WorkloadSleep:
		d := time.Duration(c.TaskMS) * time.Millisecond
		return func(ctx context.Context) {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
			case <-timer.C:
			}
		}
	default:
		iterations := c.Iterations
		if iterations < 1 {
			iterations = 1
		}
		nbytes := c.Bytes
		if nbytes < 1 {
			nbytes = 32
		}
		return func(context.Context) {
			cpuWork(iterations, nbytes)
		}
	}
}

// cpuWork hashes a fixed buffer so the compiler cannot eliminate the loop.
// iterations and nbytes are the inputs; elapsed time is the output.
func cpuWork(iterations, nbytes int) {
	buf := make([]byte, nbytes)
	sum := sha256.Sum256(buf)
	for i := 1; i < iterations; i++ {
		sum = sha256.Sum256(sum[:])
	}
	runtime.KeepAlive(&sum)
}
