package storage

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/Lazzar19/go-concurrency-lab/internal/benchmark"
)

func TestRetainsFinishedCapAndKeepsTheRunningJob(t *testing.T) {
	m := New()
	var oldest string
	for i := 0; i < maxRetained; i++ {
		id := fmt.Sprintf("done-%02d", i)
		if i == 0 {
			oldest = id
		}
		startFinished(t, m, id)
	}
	active := newRecord("active")
	if err := m.Start(active); err != nil {
		t.Fatal(err)
	}
	if got := jobCount(m); got != maxRetained {
		t.Fatalf("jobs = %d, want %d", got, maxRetained)
	}
	if _, err := m.View("active"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.View(oldest); !errors.Is(err, ErrNotFound) {
		t.Fatalf("oldest err = %v, want ErrNotFound", err)
	}
	if m.active == nil || m.active.ID != "active" {
		t.Fatalf("active = %+v", m.active)
	}
}

func TestEvictSkipsOldestRunningAndDropsLaterFinished(t *testing.T) {
	m := New()
	m.mu.Lock()
	running := newRecord("active")
	m.jobs[running.ID] = running
	m.order = append(m.order, running.ID)
	m.active = running
	for i := 0; i < maxRetained; i++ {
		id := fmt.Sprintf("done-%02d", i)
		rec := newRecord(id)
		rec.Status = StatusCompleted
		m.jobs[id] = rec
		m.order = append(m.order, id)
	}
	m.evict()
	m.mu.Unlock()

	if got := jobCount(m); got != maxRetained {
		t.Fatalf("jobs = %d, want %d", got, maxRetained)
	}
	view, err := m.View("active")
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != StatusRunning {
		t.Fatalf("status = %s", view.Status)
	}
	if _, err := m.View("done-00"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("oldest finished err = %v", err)
	}
}

func TestConcurrentStartFinishAndView(t *testing.T) {
	m := New()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				id := fmt.Sprintf("g%d-%d", g, i)
				rec := newRecord(id)
				if err := m.Start(rec); err != nil {
					_, _ = m.View(id)
					continue
				}
				m.Finish(id, benchmark.Result{}, StatusCompleted, "")
				close(rec.Done)
				_, _ = m.View(id)
				_, _ = m.Cancel(id)
			}
		}(g)
	}
	wg.Wait()
	if jobCount(m) > maxRetained {
		t.Fatalf("jobs = %d, over cap %d", jobCount(m), maxRetained)
	}
}

func startFinished(t *testing.T, m *Memory, id string) {
	t.Helper()
	rec := newRecord(id)
	if err := m.Start(rec); err != nil {
		t.Fatal(err)
	}
	m.Finish(id, benchmark.Result{}, StatusCompleted, "")
	close(rec.Done)
}

func newRecord(id string) *Record {
	return &Record{
		ID:     id,
		Status: StatusRunning,
		Done:   make(chan struct{}),
		Cancel: func() {},
	}
}

func jobCount(m *Memory) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.jobs)
}
