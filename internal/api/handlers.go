// Package api exposes the benchmark engine over HTTP.
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/Lazzar19/go-concurrency-lab/internal/benchmark"
	"github.com/Lazzar19/go-concurrency-lab/internal/storage"
)

const maxSeriesPoints = 200

// Handler serves benchmark routes. At most one benchmark runs at a time.
type Handler struct {
	store *storage.Memory
}

// NewHandler returns a handler with an empty in-memory store.
func NewHandler() *Handler {
	return &Handler{store: storage.New()}
}

// Register attaches the benchmark routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/benchmarks", h.Start)
	mux.HandleFunc("GET /api/benchmarks/{id}", h.Get)
	mux.HandleFunc("POST /api/benchmarks/{id}/cancel", h.Cancel)
	mux.HandleFunc("GET /api/benchmarks/{id}/stream", h.Stream)
	mux.HandleFunc("GET /api/benchmarks/{id}/export.json", h.ExportJSON)
	mux.HandleFunc("GET /api/benchmarks/{id}/export.csv", h.ExportCSV)
}

// Start validates the body and launches one benchmark.
func (h *Handler) Start(w http.ResponseWriter, r *http.Request) {
	var cfg benchmark.Config
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := cfg.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	rec := &storage.Record{
		ID:      newID(),
		Status:  storage.StatusRunning,
		Config:  cfg,
		Monitor: benchmark.NewMonitor(cfg.Tasks * cfg.Runs),
		Cancel:  cancel,
		Done:    make(chan struct{}),
	}
	if err := h.store.Start(rec); err != nil {
		cancel()
		var busy *storage.BusyError
		if errors.As(err, &busy) {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error":     err.Error(),
				"active_id": busy.ActiveID,
			})
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	id := rec.ID
	status := rec.Status
	go h.run(ctx, rec)
	writeJSON(w, http.StatusAccepted, map[string]string{
		"id":     id,
		"status": status,
	})
}

func (h *Handler) run(ctx context.Context, rec *storage.Record) {
	defer close(rec.Done)
	result, err := benchmark.RunObserved(ctx, rec.Config, rec.Monitor)
	status, msg := statusFromErr(err)
	h.store.Finish(rec.ID, result, status, msg)
}

// Get returns status, summaries, and a downsampled series.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	view, err := h.store.View(r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "benchmark not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, present(view))
}

// Cancel stops a running benchmark and waits until it has finished.
func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	done, err := h.store.Cancel(id)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "benchmark not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	select {
	case <-done:
	case <-r.Context().Done():
		return
	}
	view, err := h.store.View(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "benchmark not found")
		return
	}
	writeJSON(w, http.StatusOK, present(view))
}

func statusFromErr(err error) (string, string) {
	if err == nil {
		return storage.StatusCompleted, ""
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return storage.StatusCancelled, err.Error()
	}
	return storage.StatusFailed, err.Error()
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("150405.000000")))
	}
	return hex.EncodeToString(b[:])
}

type benchmarkResponse struct {
	ID          string                `json:"id"`
	Status      string                `json:"status"`
	Error       string                `json:"error,omitempty"`
	Environment benchmark.Environment `json:"environment"`
	Config      benchmark.Config      `json:"config"`
	Progress    *progressJSON         `json:"progress,omitempty"`
	Runs        []runJSON             `json:"runs"`
}

type progressJSON struct {
	Completed        int     `json:"completed"`
	Total            int     `json:"total"`
	ElapsedMS        float64 `json:"elapsed_ms"`
	ThroughputWindow float64 `json:"throughput_window"`
	Goroutines       int     `json:"goroutines"`
	HeapBytes        uint64  `json:"heap_bytes"`
	E2EP50MS         float64 `json:"e2e_p50_ms"`
	E2EP95MS         float64 `json:"e2e_p95_ms"`
}

type runJSON struct {
	TasksCompleted int                 `json:"tasks_completed"`
	DurationMS     float64             `json:"duration_ms"`
	Throughput     float64             `json:"throughput"`
	E2E            latencyJSON         `json:"e2e"`
	Service        latencyJSON         `json:"service"`
	Resources      benchmark.Resources `json:"resources"`
	Series         []seriesPoint       `json:"series"`
}

type latencyJSON struct {
	MinMS float64 `json:"min_ms"`
	MaxMS float64 `json:"max_ms"`
	P50MS float64 `json:"p50_ms"`
	P90MS float64 `json:"p90_ms"`
	P95MS float64 `json:"p95_ms"`
	P99MS float64 `json:"p99_ms"`
}

type seriesPoint struct {
	AtMS      float64 `json:"at_ms"`
	E2EMS     float64 `json:"e2e_ms"`
	ServiceMS float64 `json:"service_ms"`
}

func present(view storage.View) benchmarkResponse {
	env := view.Result.Environment
	if env.GoVersion == "" {
		env = benchmark.CurrentEnvironment()
	}
	resp := benchmarkResponse{
		ID:          view.ID,
		Status:      view.Status,
		Error:       view.Err,
		Environment: env,
		Config:      view.Config,
		Runs:        make([]runJSON, 0, len(view.Result.Runs)),
	}
	if view.Monitor != nil {
		snap := progressFrom(view.Monitor.Snapshot())
		resp.Progress = &snap
	}
	for _, run := range view.Result.Runs {
		resp.Runs = append(resp.Runs, runJSON{
			TasksCompleted: run.TasksCompleted,
			DurationMS:     durationMS(run.Duration),
			Throughput:     run.Throughput,
			E2E:            latencyFrom(run.E2E),
			Service:        latencyFrom(run.Service),
			Resources:      run.Resources,
			Series:         downsample(run.Tasks),
		})
	}
	return resp
}

func progressFrom(p benchmark.Progress) progressJSON {
	return progressJSON{
		Completed:        p.Completed,
		Total:            p.Total,
		ElapsedMS:        durationMS(p.Elapsed),
		ThroughputWindow: p.ThroughputWindow,
		Goroutines:       p.Goroutines,
		HeapBytes:        p.HeapBytes,
		E2EP50MS:         durationMS(p.E2EP50),
		E2EP95MS:         durationMS(p.E2EP95),
	}
}

func latencyFrom(stats benchmark.LatencyStats) latencyJSON {
	return latencyJSON{
		MinMS: durationMS(stats.Min),
		MaxMS: durationMS(stats.Max),
		P50MS: durationMS(stats.P50),
		P90MS: durationMS(stats.P90),
		P95MS: durationMS(stats.P95),
		P99MS: durationMS(stats.P99),
	}
}

func downsample(tasks []benchmark.TaskResult) []seriesPoint {
	if len(tasks) == 0 {
		return []seriesPoint{}
	}
	sorted := append([]benchmark.TaskResult(nil), tasks...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].At < sorted[j].At })

	stride := 1
	if len(sorted) > maxSeriesPoints {
		stride = (len(sorted) + maxSeriesPoints - 1) / maxSeriesPoints
	}
	points := make([]seriesPoint, 0, len(sorted)/stride+1)
	lastIdx := 0
	for i := 0; i < len(sorted); i += stride {
		points = append(points, pointFrom(sorted[i]))
		lastIdx = i
	}
	if lastIdx != len(sorted)-1 {
		points = append(points, pointFrom(sorted[len(sorted)-1]))
	}
	return points
}

func pointFrom(task benchmark.TaskResult) seriesPoint {
	return seriesPoint{
		AtMS:      durationMS(task.At),
		E2EMS:     durationMS(task.E2E),
		ServiceMS: durationMS(task.Service),
	}
}

func durationMS(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
