package benchmark

import (
	"context"
	"sync"
	"time"
)

// runSemaphore is fan-out with a channel semaphore of size cfg.Concurrency.
// Time blocked on the semaphore is included in end-to-end latency.
func runSemaphore(ctx context.Context, cfg Config, work workFunc, rec *recorder) error {
	sem := make(chan struct{}, cfg.Concurrency)
	var wg sync.WaitGroup
	for id := 0; id < cfg.Tasks; id++ {
		if err := ctx.Err(); err != nil {
			wg.Wait()
			return err
		}
		wg.Add(1)
		submit := time.Now()
		go func(id int, submit time.Time) {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
			}
			defer func() { <-sem }()

			if ctx.Err() != nil {
				return
			}
			start := time.Now()
			work(ctx)
			rec.add(observation{
				id:     id,
				submit: submit,
				start:  start,
				end:    time.Now(),
			})
		}(id, submit)
	}
	wg.Wait()
	return ctx.Err()
}
