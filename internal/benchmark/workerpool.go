package benchmark

import (
	"context"
	"sync"
	"time"
)

type job struct {
	id     int
	submit time.Time
}

// runWorkerPool keeps cfg.Concurrency workers alive.
// QueueSize 0 is an unbuffered channel. Submit time is taken before the send,
// so end-to-end latency includes time blocked on a full or unbuffered queue.
func runWorkerPool(ctx context.Context, cfg Config, work workFunc, rec *recorder) error {
	jobs := make(chan job, cfg.QueueSize)
	var wg sync.WaitGroup
	wg.Add(cfg.Concurrency)
	for range cfg.Concurrency {
		go func() {
			defer wg.Done()
			for item := range jobs {
				if ctx.Err() != nil {
					continue
				}
				start := time.Now()
				work(ctx)
				rec.add(observation{
					id:     item.id,
					submit: item.submit,
					start:  start,
					end:    time.Now(),
				})
			}
		}()
	}

	var sendErr error
send:
	for id := 0; id < cfg.Tasks; id++ {
		item := job{id: id, submit: time.Now()}
		select {
		case <-ctx.Done():
			sendErr = ctx.Err()
			break send
		case jobs <- item:
		}
	}
	close(jobs)
	wg.Wait()
	if sendErr != nil {
		return sendErr
	}
	return ctx.Err()
}
