package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	newMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ok\n" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestBenchmarkRouteIsWired(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/benchmarks", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	newMux().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestDashboardAssets(t *testing.T) {
	mux := newMux()
	pages := []struct {
		path   string
		needle string
	}{
		{path: "/", needle: "not a statistical proof"},
		{path: "/styles.css", needle: "legend"},
		{path: "/build/charts.js", needle: "LabCharts"},
		{path: "/build/app.js", needle: "EventSource"},
	}
	for _, page := range pages {
		req := httptest.NewRequest(http.MethodGet, page.path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), page.needle) {
			t.Fatalf("%s status=%d body=%q", page.path, rec.Code, rec.Body.String())
		}
	}
}

func TestListenAddr(t *testing.T) {
	if got := listenAddr(""); got != "0.0.0.0:8080" {
		t.Fatalf("default addr = %s", got)
	}
	if got := listenAddr("9090"); got != "0.0.0.0:9090" {
		t.Fatalf("custom addr = %s", got)
	}
}
