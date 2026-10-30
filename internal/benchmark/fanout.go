package benchmark

import (
	"context"
	"sync"
	"time"
)

// runFanout starts one goroutine per task. Results are collected through the
// shared recorder mutex. There is no concurrency cap.
func runFanout(ctx context.Context, cfg Config, work workFunc, rec *recorder) error {
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
