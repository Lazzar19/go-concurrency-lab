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

func TestListenAddr(t *testing.T) {
	if got := listenAddr(""); got != "0.0.0.0:8080" {
		t.Fatalf("default addr = %s", got)
	}
	if got := listenAddr("9090"); got != "0.0.0.0:9090" {
		t.Fatalf("custom addr = %s", got)
	}
}
