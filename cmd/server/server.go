package main

import (
	"context"
	"io"
	"net/http"

	"github.com/Lazzar19/go-concurrency-lab/internal/api"
	"github.com/Lazzar19/go-concurrency-lab/web"
)

type app struct {
	handler *api.Handler
	mux     *http.ServeMux
}

func newApp(ctx context.Context) *app {
	h := api.NewHandler()
	h.UseContext(ctx)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health)
	h.Register(mux)
	mux.Handle("GET /", http.FileServer(http.FS(web.Files)))
	return &app{handler: h, mux: mux}
}

func newMux() *http.ServeMux {
	return newApp(context.Background()).mux
}

func health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok\n")
}
