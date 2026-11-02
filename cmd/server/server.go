package main

import (
	"io"
	"net/http"

	"github.com/Lazzar19/go-concurrency-lab/internal/api"
	"github.com/Lazzar19/go-concurrency-lab/web"
)

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health)
	api.NewHandler().Register(mux)
	mux.Handle("GET /", http.FileServer(http.FS(web.Files)))
	return mux
}

func health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok\n")
}
