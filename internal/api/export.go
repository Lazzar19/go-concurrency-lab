package api

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Lazzar19/go-concurrency-lab/internal/benchmark"
	"github.com/Lazzar19/go-concurrency-lab/internal/storage"
)

// ExportJSON downloads every task sample for a finished benchmark.
func (h *Handler) ExportJSON(w http.ResponseWriter, r *http.Request) {
	view, ok := h.finishedView(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="benchmark-%s.json"`, view.ID))
	writeJSON(w, http.StatusOK, documentFrom(view))
}

// ExportCSV downloads one summary row per repetition, without task samples.
func (h *Handler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	view, ok := h.finishedView(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="benchmark-%s.csv"`, view.ID))
	cw := csv.NewWriter(w)
	if err := cw.Write(csvHeader); err != nil {
		return
	}
	for i, run := range view.Result.Runs {
		if err := cw.Write(csvRow(view, i, run)); err != nil {
			return
		}
	}
	cw.Flush()
}

func (h *Handler) finishedView(w http.ResponseWriter, r *http.Request) (storage.View, bool) {
	view, err := h.store.View(r.PathValue("id"))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "benchmark not found")
		return storage.View{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return storage.View{}, false
	}
	if view.Status == storage.StatusRunning {
		writeError(w, http.StatusConflict, "benchmark is still running")
		return storage.View{}, false
	}
	return view, true
}

type exportDocument struct {
	ID          string                `json:"id"`
	Status      string                `json:"status"`
	Error       string                `json:"error,omitempty"`
	Environment benchmark.Environment `json:"environment"`
	Config      benchmark.Config      `json:"config"`
	Runs        []exportRun           `json:"runs"`
}

type exportRun struct {
	TasksCompleted int                 `json:"tasks_completed"`
	DurationMS     float64             `json:"duration_ms"`
	Throughput     float64             `json:"throughput"`
	E2E            latencyJSON         `json:"e2e"`
	Service        latencyJSON         `json:"service"`
	Resources      benchmark.Resources `json:"resources"`
	Samples        []exportSample      `json:"samples"`
}

type exportSample struct {
	ID        int     `json:"id"`
	AtMS      float64 `json:"at_ms"`
	E2EMS     float64 `json:"e2e_ms"`
	ServiceMS float64 `json:"service_ms"`
}

func documentFrom(view storage.View) exportDocument {
	doc := exportDocument{
		ID:          view.ID,
		Status:      view.Status,
		Error:       view.Err,
		Environment: view.Result.Environment,
		Config:      view.Result.Config,
		Runs:        make([]exportRun, 0, len(view.Result.Runs)),
	}
	for _, run := range view.Result.Runs {
		samples := make([]exportSample, len(run.Tasks))
		for i, task := range run.Tasks {
			samples[i] = exportSample{
				ID:        task.ID,
				AtMS:      durationMS(task.At),
				E2EMS:     durationMS(task.E2E),
				ServiceMS: durationMS(task.Service),
			}
		}
		doc.Runs = append(doc.Runs, exportRun{
			TasksCompleted: run.TasksCompleted,
			DurationMS:     durationMS(run.Duration),
			Throughput:     run.Throughput,
			E2E:            latencyFrom(run.E2E),
			Service:        latencyFrom(run.Service),
			Resources:      run.Resources,
			Samples:        samples,
		})
	}
	return doc
}

var csvHeader = []string{
	"id",
	"status",
	"strategy",
	"workload",
	"tasks",
	"concurrency",
	"queue_size",
	"run",
	"tasks_completed",
	"duration_ms",
	"throughput",
	"e2e_p50_ms",
	"e2e_p99_ms",
	"service_p50_ms",
	"service_p99_ms",
	"goroutines_peak",
	"heap_peak",
}

func csvRow(view storage.View, index int, run benchmark.RunResult) []string {
	cfg := view.Result.Config
	return []string{
		view.ID,
		view.Status,
		string(cfg.Strategy),
		string(cfg.Workload),
		strconv.Itoa(cfg.Tasks),
		strconv.Itoa(cfg.Concurrency),
		strconv.Itoa(cfg.QueueSize),
		strconv.Itoa(index + 1),
		strconv.Itoa(run.TasksCompleted),
		strconv.FormatFloat(durationMS(run.Duration), 'f', 3, 64),
		strconv.FormatFloat(run.Throughput, 'f', 3, 64),
		strconv.FormatFloat(durationMS(run.E2E.P50), 'f', 3, 64),
		strconv.FormatFloat(durationMS(run.E2E.P99), 'f', 3, 64),
		strconv.FormatFloat(durationMS(run.Service.P50), 'f', 3, 64),
		strconv.FormatFloat(durationMS(run.Service.P99), 'f', 3, 64),
		strconv.Itoa(run.Resources.GoroutinesPeak),
		strconv.FormatUint(run.Resources.HeapPeak, 10),
	}
}
