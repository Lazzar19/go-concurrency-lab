// Package storage keeps benchmark jobs in memory for the life of the process.
package storage

import (
	"errors"
	"sync"

	"github.com/Lazzar19/go-concurrency-lab/internal/benchmark"
)

const (
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"

	maxRetained = 32
)

// ErrBusy is returned when a benchmark is already running.
var ErrBusy = errors.New("a benchmark is already running")

// ErrNotFound is returned when the id is unknown.
var ErrNotFound = errors.New("benchmark not found")

// BusyError is ErrBusy plus the id of the benchmark that holds the process.
type BusyError struct {
	ActiveID string
}

func (e *BusyError) Error() string { return ErrBusy.Error() }

func (e *BusyError) Unwrap() error { return ErrBusy }

// Record is one benchmark request.
type Record struct {
	ID      string
	Status  string
	Config  benchmark.Config
	Result  benchmark.Result
	Err     string
	Monitor *benchmark.Monitor
	Cancel  func()
	Done    chan struct{}
}

// View is a consistent copy of a record for HTTP responses.
type View struct {
	ID      string
	Status  string
	Config  benchmark.Config
	Result  benchmark.Result
	Err     string
	Monitor *benchmark.Monitor
}

// Memory stores jobs and allows one of them to run.
type Memory struct {
	mu     sync.Mutex
	jobs   map[string]*Record
	order  []string
	active *Record
}

// New returns an empty store.
func New() *Memory {
	return &Memory{jobs: make(map[string]*Record)}
}

// Start inserts rec if nothing is running.
func (m *Memory) Start(rec *Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != nil {
		return &BusyError{ActiveID: m.active.ID}
	}
	m.jobs[rec.ID] = rec
	m.order = append(m.order, rec.ID)
	m.active = rec
	m.evict()
	return nil
}

// Finish stores the outcome and clears the active slot when id still holds it.
func (m *Memory) Finish(id string, result benchmark.Result, status, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.jobs[id]
	if !ok || rec.Status != StatusRunning {
		return
	}
	rec.Result = result
	rec.Status = status
	rec.Err = errMsg
	if m.active != nil && m.active.ID == id {
		m.active = nil
	}
}

// Cancel requests cancellation when the job is still running.
// The returned channel is closed after Finish.
func (m *Memory) Cancel(id string) (<-chan struct{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.jobs[id]
	if !ok {
		return nil, ErrNotFound
	}
	if rec.Status == StatusRunning && rec.Cancel != nil {
		rec.Cancel()
	}
	return rec.Done, nil
}

// View returns a copy of the record fields.
func (m *Memory) View(id string) (View, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.jobs[id]
	if !ok {
		return View{}, ErrNotFound
	}
	return View{
		ID:      rec.ID,
		Status:  rec.Status,
		Config:  rec.Config,
		Result:  rec.Result,
		Err:     rec.Err,
		Monitor: rec.Monitor,
	}, nil
}

// Live returns the pieces a stream needs without holding the store lock.
func (m *Memory) Live(id string) (*benchmark.Monitor, <-chan struct{}, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.jobs[id]
	if !ok {
		return nil, nil, "", ErrNotFound
	}
	return rec.Monitor, rec.Done, rec.Status, nil
}

// evict drops the oldest finished jobs until maxRetained remains.
// A running job stays, and eviction continues past it. The caller holds m.mu.
func (m *Memory) evict() {
	seen := 0
	for len(m.jobs) > maxRetained && seen < len(m.order) {
		id := m.order[0]
		m.order = m.order[1:]
		seen++
		rec := m.jobs[id]
		if rec == nil {
			continue
		}
		if rec.Status == StatusRunning {
			m.order = append(m.order, id)
			continue
		}
		delete(m.jobs, id)
		seen = 0
	}
}
