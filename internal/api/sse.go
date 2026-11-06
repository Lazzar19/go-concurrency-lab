package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Lazzar19/go-concurrency-lab/internal/storage"
)

// Stream writes progress about every 100ms.
// Closing the stream only stops the events. Cancel is POST /cancel.
func (h *Handler) Stream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, done, status, err := h.store.Live(id)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "benchmark not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	if status != storage.StatusRunning {
		_ = h.writeProgress(w, id)
		_ = h.writeDone(w, id)
		return
	}
	if err := h.writeProgress(w, id); err != nil {
		return
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-done:
			_ = h.writeProgress(w, id)
			_ = h.writeDone(w, id)
			return
		case <-ticker.C:
			if err := h.writeProgress(w, id); err != nil {
				return
			}
		}
	}
}

func (h *Handler) writeProgress(w http.ResponseWriter, id string) error {
	view, err := h.store.View(id)
	if err != nil {
		return err
	}
	if view.Monitor == nil {
		return writeEvent(w, "progress", progressJSON{})
	}
	return writeEvent(w, "progress", progressFrom(view.Monitor.Snapshot()))
}

func (h *Handler) writeDone(w http.ResponseWriter, id string) error {
	view, err := h.store.View(id)
	if err != nil {
		return err
	}
	return writeEvent(w, "done", map[string]string{
		"id":     view.ID,
		"status": view.Status,
	})
}

func writeEvent(w http.ResponseWriter, name string, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, payload); err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}
