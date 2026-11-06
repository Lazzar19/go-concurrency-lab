package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

func TestServeReturnsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := newApp(ctx)
	bound := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- serve(ctx, "127.0.0.1:0", app.mux, app.handler.Wait, func(addr string) {
			bound <- addr
		})
	}()
	var addr string
	select {
	case addr = <-bound:
	case err := <-errCh:
		t.Fatalf("serve ended before listen: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("listener did not open")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		res, err := http.Get("http://" + addr + "/health")
		if err == nil {
			body, readErr := io.ReadAll(res.Body)
			res.Body.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if res.StatusCode != http.StatusOK || string(body) != "ok\n" {
				t.Fatalf("health = %d %q", res.StatusCode, body)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("health: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return after cancel")
	}
}

func TestListenAddr(t *testing.T) {
	if got := listenAddr("", ""); got != "127.0.0.1:8080" {
		t.Fatalf("default addr = %s", got)
	}
	if got := listenAddr("", "9090"); got != "127.0.0.1:9090" {
		t.Fatalf("custom port = %s", got)
	}
	if got := listenAddr("0.0.0.0", "8080"); got != "0.0.0.0:8080" {
		t.Fatalf("all interfaces = %s", got)
	}
	if got := listenAddr("::1", ""); got != "[::1]:8080" {
		t.Fatalf("ipv6 = %s", got)
	}
}
