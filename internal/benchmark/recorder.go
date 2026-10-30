package benchmark

import (
	"sync"
	"time"
)

type observation struct {
	id     int
	submit time.Time
	start  time.Time
	end    time.Time
}

// recorder collects task observations. Fan-out writes from many goroutines,
// so every add is mutex-protected.
type recorder struct {
	mu    sync.Mutex
	items []observation
}

func (r *recorder) add(item observation) {
	r.mu.Lock()
	r.items = append(r.items, item)
	r.mu.Unlock()
}

func (r *recorder) snapshot() []observation {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]observation, len(r.items))
	copy(out, r.items)
	return out
}
