"use strict";
const STRATEGIES = [
    { id: "sequential", label: "Sequential", color: "#9aa89a" },
    { id: "fanout", label: "Fan-out", color: "#e37b3a" },
    { id: "semaphore", label: "Semaphore", color: "#3cbfa0" },
    { id: "worker_pool", label: "Worker pool", color: "#d6c15a" },
];
const state = {
    running: false,
    activeId: "",
    activeStrategy: "",
    configKey: "",
    progress: null,
    focus: null,
    results: {},
    message: "",
};
let activeSocket = null;
let stopQueue = false;
function must(id) {
    const node = document.getElementById(id);
    if (!node)
        throw new Error("missing #" + id);
    return node;
}
function strategyMeta(id) {
    const found = STRATEGIES.find((item) => item.id === id);
    if (!found)
        throw new Error("unknown strategy " + id);
    return found;
}
function numberValue(id) {
    return Number(must(id).value);
}
function configFor(strategy) {
    const workload = must("workload").value;
    const body = {
        strategy,
        workload,
        tasks: numberValue("tasks"),
        concurrency: numberValue("concurrency"),
        queue_size: numberValue("queue_size"),
        runs: numberValue("runs"),
    };
    if (workload === "sleep")
        body.task_ms = numberValue("task_ms");
    else
        body.iterations = numberValue("iterations");
    return body;
}
function configKey() {
    const body = configFor("sequential");
    const { strategy: _strategy, ...rest } = body;
    return JSON.stringify(rest);
}
function formatMS(value) {
    if (typeof value !== "number" || !Number.isFinite(value))
        return "—";
    return value.toFixed(2) + " ms";
}
function formatRate(value) {
    if (typeof value !== "number" || !Number.isFinite(value))
        return "—";
    return value.toFixed(1) + "/s";
}
function formatBytes(value) {
    if (typeof value !== "number" || !Number.isFinite(value))
        return "—";
    return (value / (1024 * 1024)).toFixed(2) + " MiB";
}
function median(values) {
    if (!values.length)
        return null;
    const sorted = values.slice().sort((a, b) => a - b);
    const mid = Math.floor(sorted.length / 2);
    if (sorted.length % 2)
        return sorted[mid];
    return (sorted[mid - 1] + sorted[mid]) / 2;
}
function setRunning(running) {
    state.running = running;
    must("compare").disabled = running;
    must("run-one").disabled = running;
    must("cancel").disabled = !running;
    must("controls").querySelectorAll("input, select").forEach((el) => {
        el.disabled = running;
    });
}
function showError(message) {
    const node = must("error");
    node.hidden = !message;
    node.textContent = message;
}
function renderProgress() {
    const progress = state.progress;
    const label = state.activeStrategy
        ? strategyMeta(state.activeStrategy).label
        : state.running
            ? "Starting"
            : "Finished";
    if (!progress) {
        must("status").textContent = state.running ? "Starting " + label : "Idle";
        must("bar").style.width = "0%";
        must("meta").textContent = state.message;
        return;
    }
    const total = progress.total || 0;
    const ratio = total ? Math.min(1, progress.completed / total) : 0;
    must("status").textContent = label + " · " + progress.completed + " / " + total;
    must("bar").style.width = (ratio * 100).toFixed(1) + "%";
    const parts = [
        progress.elapsed_ms.toFixed(0) + " ms",
        formatRate(progress.throughput_window),
        progress.goroutines + " goroutines",
        formatBytes(progress.heap_bytes),
    ];
    if (state.message)
        parts.push(state.message);
    must("meta").textContent = parts.join(" · ");
}
function focusedRun() {
    if (!state.focus)
        return null;
    const runs = state.results[state.focus.strategy]?.response?.runs;
    if (!runs?.length)
        return null;
    return runs[Math.min(state.focus.run, runs.length - 1)];
}
function renderCards() {
    const host = must("cards");
    host.replaceChildren();
    const focused = focusedRun();
    const stats = [
        ["E2E p50", focused?.e2e.p50_ms],
        ["E2E p95", focused?.e2e.p95_ms],
        ["E2E p99", focused?.e2e.p99_ms],
        ["Service p50", focused?.service.p50_ms],
        ["Service p95", focused?.service.p95_ms],
        ["Service p99", focused?.service.p99_ms],
    ];
    for (const [name, value] of stats) {
        const card = document.createElement("article");
        card.className = "card";
        const label = document.createElement("span");
        label.textContent = name;
        const strong = document.createElement("strong");
        strong.textContent = formatMS(value);
        card.append(label, strong);
        host.append(card);
    }
}
function renderTable() {
    const body = must("comparison").querySelector("tbody");
    if (!body)
        return;
    body.replaceChildren();
    for (const strategy of STRATEGIES) {
        const runs = state.results[strategy.id]?.response?.runs;
        if (!runs?.length)
            continue;
        const mid = median(runs.map((run) => run.throughput));
        runs.forEach((run, index) => {
            const row = document.createElement("tr");
            const selected = state.focus?.strategy === strategy.id && state.focus.run === index;
            if (selected)
                row.className = "selected";
            row.tabIndex = 0;
            const cells = [
                strategy.label,
                String(index + 1),
                formatRate(run.throughput),
                formatRate(mid),
                formatMS(run.e2e.p50_ms),
                formatMS(run.e2e.p99_ms),
                formatMS(run.service.p50_ms),
                formatMS(run.service.p99_ms),
                String(run.resources.goroutines_peak),
                formatBytes(run.resources.heap_peak),
            ];
            for (const text of cells) {
                const cell = document.createElement("td");
                cell.textContent = text;
                row.append(cell);
            }
            const select = () => {
                state.focus = { strategy: strategy.id, run: index };
                renderTable();
                renderCards();
                drawCharts();
            };
            row.addEventListener("click", select);
            row.addEventListener("keydown", (event) => {
                if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    select();
                }
            });
            body.append(row);
        });
    }
}
function finitePoints(points) {
    return points.filter((point) => Number.isFinite(point.x) && Number.isFinite(point.y));
}
function drawCharts() {
    const runIndex = state.focus?.run ?? 0;
    const latency = STRATEGIES.map((strategy) => {
        const runs = state.results[strategy.id]?.response?.runs;
        const run = runs?.length ? runs[Math.min(runIndex, runs.length - 1)] : undefined;
        return {
            label: strategy.label,
            color: strategy.color,
            data: finitePoints((run?.series ?? []).map((point) => ({ x: point.at_ms, y: point.e2e_ms }))),
        };
    });
    const live = (pick) => STRATEGIES.map((strategy) => ({
        label: strategy.label,
        color: strategy.color,
        data: finitePoints(pick(state.results[strategy.id]?.live ?? emptyLive())),
    }));
    window.LabCharts.draw(must("latency"), latency, { xLabel: "ms", empty: "Appears when a run finishes" });
    window.LabCharts.draw(must("throughput"), live((samples) => samples.throughput), { xLabel: "ms", empty: "Sampled during the run" });
    window.LabCharts.draw(must("goroutines"), live((samples) => samples.goroutines), { xLabel: "ms" });
    window.LabCharts.draw(must("heap"), live((samples) => samples.heap), { xLabel: "ms" });
}
function renderLegend() {
    const list = must("legend");
    list.replaceChildren();
    for (const strategy of STRATEGIES) {
        const item = document.createElement("li");
        const swatch = document.createElement("i");
        swatch.style.background = strategy.color;
        item.append(swatch, document.createTextNode(strategy.label));
        list.append(item);
    }
}
function renderAll() {
    renderProgress();
    renderCards();
    renderTable();
    drawCharts();
}
function emptyLive() {
    return { throughput: [], goroutines: [], heap: [] };
}
function trim(series) {
    if (series.length <= 360)
        return;
    const next = [];
    for (let i = 0; i < series.length; i += 2)
        next.push(series[i]);
    series.splice(0, series.length, ...next);
}
function pushLive(strategy, progress) {
    const entry = state.results[strategy];
    if (!entry)
        return;
    const x = progress.elapsed_ms;
    entry.live.throughput.push({ x, y: progress.throughput_window });
    entry.live.goroutines.push({ x, y: progress.goroutines });
    entry.live.heap.push({ x, y: progress.heap_bytes / (1024 * 1024) });
    trim(entry.live.throughput);
    trim(entry.live.goroutines);
    trim(entry.live.heap);
}
function watch(id, strategy) {
    return new Promise((resolve, reject) => {
        const source = new EventSource("/api/benchmarks/" + id + "/stream");
        activeSocket = source;
        let settled = false;
        const finish = (fn) => {
            if (settled)
                return;
            settled = true;
            source.close();
            if (activeSocket === source)
                activeSocket = null;
            fn();
        };
        source.addEventListener("progress", (event) => {
            const progress = JSON.parse(event.data);
            state.progress = progress;
            pushLive(strategy, progress);
            renderProgress();
            drawCharts();
        });
        source.addEventListener("done", (event) => {
            const payload = JSON.parse(event.data);
            finish(() => {
                fetch("/api/benchmarks/" + id)
                    .then((res) => res.json())
                    .then((full) => {
                    const entry = state.results[strategy];
                    if (entry)
                        entry.response = full;
                    const count = full.runs?.length ?? 0;
                    state.focus = { strategy, run: Math.max(0, count - 1) };
                    const env = full.environment;
                    if (env?.go_version) {
                        state.message = env.go_version + " · GOMAXPROCS " + env.gomaxprocs + " · " + env.num_cpu + " CPUs";
                    }
                    renderAll();
                    if (payload.status === "failed") {
                        reject(new Error(full.error || "benchmark failed"));
                        return;
                    }
                    if (payload.status === "cancelled")
                        stopQueue = true;
                    resolve(payload.status);
                })
                    .catch(reject);
            });
        });
        source.onerror = () => {
            finish(() => reject(new Error("live stream closed")));
        };
    });
}
async function runStrategy(strategy) {
    state.activeStrategy = strategy;
    state.progress = null;
    state.results[strategy] = { live: emptyLive(), response: null };
    renderAll();
    const res = await fetch("/api/benchmarks", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(configFor(strategy)),
    });
    const body = (await res.json().catch(() => ({})));
    if (!res.ok || !body.id)
        throw new Error(body.error || "HTTP " + res.status);
    state.activeId = body.id;
    return watch(body.id, strategy);
}
async function runQueue(strategies) {
    if (state.running)
        return;
    showError("");
    const key = configKey();
    if (strategies.length > 1 || key !== state.configKey) {
        state.results = {};
        state.focus = null;
        state.configKey = key;
    }
    stopQueue = false;
    setRunning(true);
    try {
        for (const strategy of strategies) {
            if (stopQueue)
                break;
            await runStrategy(strategy);
        }
    }
    catch (err) {
        showError(err instanceof Error ? err.message : "run failed");
    }
    finally {
        state.activeStrategy = "";
        state.activeId = "";
        setRunning(false);
        renderAll();
    }
}
function syncWorkloadFields() {
    const sleep = must("workload").value === "sleep";
    must("iterations-field").hidden = sleep;
    must("task-ms-field").hidden = !sleep;
}
function selectedStrategy() {
    const value = must("strategy").value;
    if (STRATEGIES.some((item) => item.id === value))
        return value;
    return "sequential";
}
must("workload").addEventListener("change", syncWorkloadFields);
must("compare").addEventListener("click", () => {
    void runQueue(STRATEGIES.map((item) => item.id));
});
must("run-one").addEventListener("click", () => {
    void runQueue([selectedStrategy()]);
});
must("cancel").addEventListener("click", () => {
    stopQueue = true;
    if (!state.activeId)
        return;
    void fetch("/api/benchmarks/" + state.activeId + "/cancel", { method: "POST" });
});
window.addEventListener("resize", drawCharts);
renderLegend();
syncWorkloadFields();
renderAll();
