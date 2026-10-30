package benchmark

import (
	"context"
	"time"
)

func runSequential(ctx context.Context, cfg Config, work workFunc, rec *recorder) error {
	for id := 0; id < cfg.Tasks; id++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		submit := time.Now()
		start := time.Now()
		work(ctx)
		rec.add(observation{
			id:     id,
			submit: submit,
			start:  start,
			end:    time.Now(),
		})
	}
	return nil
}
