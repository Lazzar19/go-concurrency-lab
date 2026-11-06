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
	"github.com/Lazzar19/go-concurrency-lab/internal/storage"
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

func TestStartRejectsExtraJSONAndMissingType(t *testing.T) {
	_, ts := newTestServer(t)
	valid := `{"strategy":"sequential","workload":"cpu","tasks":1,"iterations":1,"runs":1}`
	cases := []struct {
		body  string
		ctype string
	}{
		{body: valid + valid, ctype: "application/json"},
		{body: valid, ctype: ""},
		{body: valid, ctype: "text/plain"},
	}
	for _, tc := range cases {
		req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/benchmarks", strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		if tc.ctype != "" {
			req.Header.Set("Content-Type", tc.ctype)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("type %q status = %d, want 400 (%s)", tc.ctype, res.StatusCode, raw)
		}
	}

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/benchmarks", strings.NewReader(valid))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("charset status = %d, want 202 (%s)", res.StatusCode, raw)
	}
	var started struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &started); err != nil {
		t.Fatal(err)
	}
	cancelBench(t, ts.URL, started.ID)
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

func TestStreamDisconnectLeavesRunRunning(t *testing.T) {
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

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		res, err = http.Get(ts.URL + "/api/benchmarks/" + started.ID)
		if err != nil {
			t.Fatal(err)
		}
		var view benchmarkResponse
		if err := json.NewDecoder(res.Body).Decode(&view); err != nil {
			res.Body.Close()
			t.Fatal(err)
		}
		res.Body.Close()
		if view.Status != "running" {
			t.Fatalf("status = %s, want running after the stream closed", view.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}

	view := cancelBench(t, ts.URL, started.ID)
	if view.Status != "cancelled" {
		t.Fatalf("cancel status = %s", view.Status)
	}
	view = waitStatus(t, ts.URL, started.ID, "cancelled")
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

func TestExportFullJSONAndSummaryCSV(t *testing.T) {
	_, ts := newTestServer(t)
	res, raw := postJSON(t, ts.URL+"/api/benchmarks", `{
		"strategy":"sequential",
		"workload":"cpu",
		"tasks":6,
		"iterations":20,
		"runs":2
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
	waitStatus(t, ts.URL, started.ID, "completed")

	body := getBody(t, ts.URL+"/api/benchmarks/"+started.ID+"/export.json")
	var doc struct {
		Runs []struct {
			Samples []struct {
				ID        int     `json:"id"`
				E2EMS     float64 `json:"e2e_ms"`
				ServiceMS float64 `json:"service_ms"`
			} `json:"samples"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Runs) != 2 {
		t.Fatalf("json runs = %d", len(doc.Runs))
	}
	for _, run := range doc.Runs {
		if len(run.Samples) != 6 {
			t.Fatalf("samples = %d, want 6", len(run.Samples))
		}
		for _, sample := range run.Samples {
			if sample.E2EMS < sample.ServiceMS {
				t.Fatalf("sample %d e2e %v < service %v", sample.ID, sample.E2EMS, sample.ServiceMS)
			}
		}
	}

	csvBody := string(getBody(t, ts.URL+"/api/benchmarks/"+started.ID+"/export.csv"))
	lines := strings.Split(strings.TrimSpace(csvBody), "\n")
	if len(lines) != 3 {
		t.Fatalf("csv lines = %d, want header + 2 runs\n%s", len(lines), csvBody)
	}
	if !strings.Contains(lines[0], "e2e_p99_ms") || strings.Contains(csvBody, "e2e_ms") {
		t.Fatalf("csv should be a summary, got %s", lines[0])
	}
}

func TestExportRejectsRunningBenchmark(t *testing.T) {
	_, ts := newTestServer(t)
	res, raw := postJSON(t, ts.URL+"/api/benchmarks", `{
		"strategy":"worker_pool",
		"workload":"sleep",
		"tasks":30,
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

	export, err := http.Get(ts.URL + "/api/benchmarks/" + started.ID + "/export.json")
	if err != nil {
		t.Fatal(err)
	}
	defer export.Body.Close()
	if export.StatusCode != http.StatusConflict {
		t.Fatalf("export status = %d, want 409", export.StatusCode)
	}
}

func TestCancelFinishedRunStaysCompleted(t *testing.T) {
	_, ts := newTestServer(t)
	id := startTiny(t, ts.URL)
	waitStatus(t, ts.URL, id, "completed")
	view := cancelBench(t, ts.URL, id)
	if view.Status != "completed" {
		t.Fatalf("cancel status = %s, want completed", view.Status)
	}
	view = waitStatus(t, ts.URL, id, "completed")
	if view.Status != "completed" {
		t.Fatalf("status = %s", view.Status)
	}
}

func TestExportAllowsCancelledAndFailed(t *testing.T) {
	_, ts := newTestServer(t)
	res, raw := postJSON(t, ts.URL+"/api/benchmarks", `{
		"strategy":"worker_pool",
		"workload":"sleep",
		"tasks":20,
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
	cancelBench(t, ts.URL, started.ID)
	waitStatus(t, ts.URL, started.ID, "cancelled")
	assertExport(t, ts.URL, started.ID, "cancelled")

	h, failedSrv := newTestServer(t)
	done := make(chan struct{})
	close(done)
	rec := &storage.Record{
		ID:     "failed-job",
		Status: storage.StatusRunning,
		Config: benchmark.Config{
			Strategy:   benchmark.StrategySequential,
			Workload:   benchmark.WorkloadCPU,
			Tasks:      1,
			Iterations: 1,
			Runs:       1,
		},
		Done:   done,
		Cancel: func() {},
	}
	if err := h.store.Start(rec); err != nil {
		t.Fatal(err)
	}
	h.store.Finish(rec.ID, benchmark.Result{}, storage.StatusFailed, "boom")
	assertExport(t, failedSrv.URL, rec.ID, "failed")
}

func TestStreamOfFinishedBenchmarkDoesNotChangeStatus(t *testing.T) {
	_, ts := newTestServer(t)
	id := startTiny(t, ts.URL)
	waitStatus(t, ts.URL, id, "completed")

	res, err := http.Get(ts.URL + "/api/benchmarks/" + id + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if res.StatusCode != http.StatusOK || !strings.Contains(text, "event: progress") || !strings.Contains(text, "event: done") {
		t.Fatalf("stream = %d %s", res.StatusCode, text)
	}
	if !strings.Contains(text, `"status":"completed"`) {
		t.Fatalf("done event = %s", text)
	}
	if view := waitStatus(t, ts.URL, id, "completed"); view.Status != "completed" {
		t.Fatalf("status = %s", view.Status)
	}
}

func startTiny(t *testing.T, base string) string {
	t.Helper()
	res, raw := postJSON(t, base+"/api/benchmarks", `{
		"strategy":"sequential",
		"workload":"cpu",
		"tasks":4,
		"iterations":20,
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
	return started.ID
}

func assertExport(t *testing.T, base, id, status string) {
	t.Helper()
	jsonRes := mustGet(t, base+"/api/benchmarks/"+id+"/export.json")
	defer jsonRes.Body.Close()
	if jsonRes.StatusCode != http.StatusOK {
		t.Fatalf("json status = %d", jsonRes.StatusCode)
	}
	if ct := jsonRes.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("json content-type = %q", ct)
	}
	if disp := jsonRes.Header.Get("Content-Disposition"); disp != `attachment; filename="benchmark-`+id+`.json"` {
		t.Fatalf("json disposition = %q", disp)
	}
	var doc struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(jsonRes.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc.Status != status {
		t.Fatalf("json status field = %s, want %s", doc.Status, status)
	}

	csvRes := mustGet(t, base+"/api/benchmarks/"+id+"/export.csv")
	defer csvRes.Body.Close()
	if csvRes.StatusCode != http.StatusOK {
		t.Fatalf("csv status = %d", csvRes.StatusCode)
	}
	if ct := csvRes.Header.Get("Content-Type"); ct != "text/csv; charset=utf-8" {
		t.Fatalf("csv content-type = %q", ct)
	}
	if disp := csvRes.Header.Get("Content-Disposition"); disp != `attachment; filename="benchmark-`+id+`.csv"` {
		t.Fatalf("csv disposition = %q", disp)
	}
}

func mustGet(t *testing.T, url string) *http.Response {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	return res
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

func getBody(t *testing.T, url string) []byte {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", url, res.StatusCode)
	}
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return raw
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
