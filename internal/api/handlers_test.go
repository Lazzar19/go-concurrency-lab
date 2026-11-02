package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Lazzar19/go-concurrency-lab/internal/benchmark"
)

func TestStartRejectsInvalidConfig(t *testing.T) {
	_, ts := newTestServer(t)
	bodies := []string{
		`{`,
		`{"strategy":"sequential","workload":"cpu","tasks":0,"iterations":1,"runs":1}`,
		`{"strategy":"sequential","workload":"cpu","tasks":100001,"iterations":1,"runs":1}`,
		`{"strategy":"worker_pool","workload":"cpu","tasks":1,"iterations":1,"concurrency":0,"runs":1}`,
		`{"strategy":"nope","workload":"cpu","tasks":1,"iterations":1,"runs":1}`,
	}
	for _, body := range bodies {
		res, raw := postJSON(t, ts.URL+"/api/benchmarks", body)
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %s status = %d, want 400 (%s)", body, res.StatusCode, raw)
		}
	}
}

func TestBenchmarkLifecycle(t *testing.T) {
	_, ts := newTestServer(t)
	res, raw := postJSON(t, ts.URL+"/api/benchmarks", `{
		"strategy":"sequential",
		"workload":"cpu",
		"tasks":20,
		"iterations":30,
		"runs":1
	}`)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("start status = %d (%s)", res.StatusCode, raw)
	}
	var started struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &started); err != nil {
		t.Fatal(err)
	}
	if started.Status != "running" || started.ID == "" {
		t.Fatalf("start body = %+v", started)
	}

	view := waitStatus(t, ts.URL, started.ID, "completed")
	if view.Environment.GoVersion == "" || view.Environment.GOMAXPROCS < 1 || view.Environment.NumCPU < 1 {
		t.Fatalf("environment = %+v", view.Environment)
	}
	if len(view.Runs) != 1 || view.Runs[0].TasksCompleted != 20 {
		t.Fatalf("runs = %+v", view.Runs)
	}
	run := view.Runs[0]
	if run.E2E.P99MS < run.E2E.P50MS || run.Service.P99MS < run.Service.P50MS {
		t.Fatalf("percentiles e2e %+v service %+v", run.E2E, run.Service)
	}
	if len(run.Series) == 0 || len(run.Series) > maxSeriesPoints+1 {
		t.Fatalf("series len = %d", len(run.Series))
	}
	if view.Progress == nil || view.Progress.Completed != 20 || view.Progress.Total != 20 {
		t.Fatalf("progress = %+v", view.Progress)
	}
}

func TestSecondStartReturns409UntilCancel(t *testing.T) {
	_, ts := newTestServer(t)
	res, raw := postJSON(t, ts.URL+"/api/benchmarks", `{
		"strategy":"worker_pool",
		"workload":"sleep",
		"tasks":40,
		"concurrency":1,
		"task_ms":300,
		"queue_size":0,
		"runs":1
	}`)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("start = %d (%s)", res.StatusCode, raw)
	}
	var started struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &started); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancelBench(t, ts.URL, started.ID) })

	res, raw = postJSON(t, ts.URL+"/api/benchmarks", `{
		"strategy":"sequential",
		"workload":"cpu",
		"tasks":1,
		"iterations":1,
		"runs":1
	}`)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("second start = %d (%s)", res.StatusCode, raw)
	}
	var conflict struct {
		ActiveID string `json:"active_id"`
	}
	if err := json.Unmarshal(raw, &conflict); err != nil {
		t.Fatal(err)
	}
	if conflict.ActiveID != started.ID {
		t.Fatalf("active_id = %s, want %s", conflict.ActiveID, started.ID)
	}

	view := cancelBench(t, ts.URL, started.ID)
	if view.Status != "cancelled" {
		t.Fatalf("status = %s", view.Status)
	}
	if len(view.Runs) != 1 || view.Runs[0].TasksCompleted >= 40 {
		t.Fatalf("partial run = %+v", view.Runs)
	}
}

func TestStreamEmitsProgressThenDone(t *testing.T) {
	_, ts := newTestServer(t)
	res, raw := postJSON(t, ts.URL+"/api/benchmarks", `{
		"strategy":"semaphore",
		"workload":"sleep",
		"tasks":8,
		"concurrency":4,
		"task_ms":30,
		"runs":1
	}`)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("start = %d (%s)", res.StatusCode, raw)
	}
	var started struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &started); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/benchmarks/"+started.ID+"/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if ct := stream.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %s", ct)
	}
	body, err := io.ReadAll(stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "event: progress") || !strings.Contains(text, "event: done") {
		t.Fatalf("stream = %s", text)
	}
	if !strings.Contains(text, `"status":"completed"`) {
		t.Fatalf("done event = %s", text)
	}
}

func TestStreamDisconnectCancels(t *testing.T) {
	_, ts := newTestServer(t)
	res, raw := postJSON(t, ts.URL+"/api/benchmarks", `{
		"strategy":"worker_pool",
		"workload":"sleep",
		"tasks":30,
		"concurrency":1,
		"task_ms":400,
		"queue_size":0,
		"runs":1
	}`)
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("start = %d (%s)", res.StatusCode, raw)
	}
	var started struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &started); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancelBench(t, ts.URL, started.ID) })

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/benchmarks/"+started.ID+"/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		res *http.Response
		err error
	}
	errCh := make(chan result, 1)
	go func() {
		stream, err := http.DefaultClient.Do(req)
		errCh <- result{res: stream, err: err}
	}()
	var stream *http.Response
	select {
	case got := <-errCh:
		if got.err != nil {
			t.Fatal(got.err)
		}
		stream = got.res
	case <-time.After(2 * time.Second):
		t.Fatal("stream headers did not arrive")
	}
	cancel()
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, stream.Body)
		stream.Body.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not return after disconnect")
	}

	view := waitStatus(t, ts.URL, started.ID, "cancelled")
	if len(view.Runs) != 1 || view.Runs[0].TasksCompleted >= 30 {
		t.Fatalf("partial run = %+v", view.Runs)
	}
}

func TestUnknownBenchmark(t *testing.T) {
	_, ts := newTestServer(t)
	res, err := http.Get(ts.URL + "/api/benchmarks/missing")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("get = %d", res.StatusCode)
	}
	res, err = http.Post(ts.URL+"/api/benchmarks/missing/cancel", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("cancel = %d", res.StatusCode)
	}
}

func TestDownsampleKeepsEnds(t *testing.T) {
	tasks := make([]benchmark.TaskResult, 500)
	for i := range tasks {
		tasks[i] = benchmark.TaskResult{
			ID:      i,
			At:      time.Duration(i) * time.Millisecond,
			E2E:     time.Millisecond,
			Service: time.Millisecond,
		}
	}
	points := downsample(tasks)
	if len(points) == 0 || len(points) > maxSeriesPoints+1 {
		t.Fatalf("len = %d", len(points))
	}
	if points[0].AtMS != 0 || points[len(points)-1].AtMS != 499 {
		t.Fatalf("ends = %v .. %v", points[0].AtMS, points[len(points)-1].AtMS)
	}
}

func newTestServer(t *testing.T) (*Handler, *httptest.Server) {
	t.Helper()
	h := NewHandler()
	mux := http.NewServeMux()
	h.Register(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return h, ts
}

func postJSON(t *testing.T, url, body string) (*http.Response, []byte) {
	t.Helper()
	res, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res, raw
}

func cancelBench(t *testing.T, base, id string) benchmarkResponse {
	t.Helper()
	res, err := http.Post(base+"/api/benchmarks/"+id+"/cancel", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var view benchmarkResponse
	if err := json.NewDecoder(res.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	return view
}

func waitStatus(t *testing.T, base, id, want string) benchmarkResponse {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last benchmarkResponse
	for time.Now().Before(deadline) {
		res, err := http.Get(base + "/api/benchmarks/" + id)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewDecoder(res.Body).Decode(&last); err != nil {
			res.Body.Close()
			t.Fatal(err)
		}
		res.Body.Close()
		if last.Status == want {
			return last
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("status = %s, want %s", last.Status, want)
	return last
}
