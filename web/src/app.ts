type StrategyId = "sequential" | "fanout" | "semaphore" | "worker_pool";
type Workload = "cpu" | "sleep";

interface Strategy {
  id: StrategyId;
  label: string;
  color: string;
}

interface LatencyJSON {
  min_ms: number;
  max_ms: number;
  p50_ms: number;
  p90_ms: number;
  p95_ms: number;
  p99_ms: number;
}

interface SeriesPoint {
  at_ms: number;
  e2e_ms: number;
  service_ms: number;
}

interface ResourcesJSON {
  goroutines_peak: number;
  heap_peak: number;
}

interface RunJSON {
  tasks_completed: number;
  duration_ms: number;
  throughput: number;
  e2e: LatencyJSON;
  service: LatencyJSON;
  resources: ResourcesJSON;
  series: SeriesPoint[];
}

interface ProgressJSON {
  completed: number;
  total: number;
  elapsed_ms: number;
  throughput_window: number;
  goroutines: number;
  heap_bytes: number;
  e2e_p50_ms: number;
  e2e_p95_ms: number;
}

interface EnvironmentJSON {
  go_version?: string;
  gomaxprocs?: number;
  num_cpu?: number;
}

interface BenchmarkJSON {
  id: string;
  status: string;
  error?: string;
  environment?: EnvironmentJSON;
  runs?: RunJSON[];
}

interface LiveSamples {
  throughput: ChartPoint[];
  goroutines: ChartPoint[];
  heap: ChartPoint[];
}

interface Entry {
  live: LiveSamples;
  response: BenchmarkJSON | null;
}

interface Focus {
  strategy: StrategyId;
  run: number;
}

interface BenchConfig {
  strategy: StrategyId;
  workload: Workload;
  tasks: number;
  concurrency: number;
  queue_size: number;
  runs: number;
  iterations?: number;
  task_ms?: number;
}

const PRESETS: Record<string, { tasks: number; concurrency: number; queue_size: number; runs: number; iterations: number }> = {
  light: { tasks: 200, concurrency: 4, queue_size: 0, runs: 1, iterations: 2000 },
  medium: { tasks: 2000, concurrency: 8, queue_size: 32, runs: 3, iterations: 8000 },
  heavy: { tasks: 20000, concurrency: 16, queue_size: 128, runs: 3, iterations: 20000 },
};

const STRATEGIES: Strategy[] = [
  { id: "sequential", label: "Sequential", color: "#9aa89a" },
  { id: "fanout", label: "Fan-out", color: "#e37b3a" },
  { id: "semaphore", label: "Semaphore", color: "#3cbfa0" },
  { id: "worker_pool", label: "Worker pool", color: "#d6c15a" },
];

const state: {
  running: boolean;
  activeId: string;
  activeStrategy: StrategyId | "";
  configKey: string;
  progress: ProgressJSON | null;
  focus: Focus | null;
  results: Partial<Record<StrategyId, Entry>>;
  message: string;
  outcome: string;
} = {
  running: false,
  activeId: "",
  activeStrategy: "",
  configKey: "",
  progress: null,
  focus: null,
  results: {},
  message: "",
  outcome: "",
};

let activeSocket: EventSource | null = null;
let stopQueue = false;

function must<T extends HTMLElement>(id: string): T {
  const node = document.getElementById(id);
  if (!node) throw new Error("missing #" + id);
  return node as T;
}

function strategyMeta(id: StrategyId): Strategy {
  const found = STRATEGIES.find((item) => item.id === id);
  if (!found) throw new Error("unknown strategy " + id);
  return found;
}

function numberValue(id: string): number {
  return Number(must<HTMLInputElement>(id).value);
}

function configFor(strategy: StrategyId): BenchConfig {
  const workload = must<HTMLSelectElement>("workload").value as Workload;
  const body: BenchConfig = {
    strategy,
    workload,
    tasks: numberValue("tasks"),
    concurrency: numberValue("concurrency"),
    queue_size: numberValue("queue_size"),
    runs: numberValue("runs"),
  };
  if (workload === "sleep") body.task_ms = numberValue("task_ms");
  else body.iterations = numberValue("iterations");
  return body;
}

function configKey(): string {
  const body = configFor("sequential");
  const { strategy: _strategy, ...rest } = body;
  return JSON.stringify(rest);
}

function formatMS(value: number | null | undefined): string {
  if (typeof value !== "number" || !Number.isFinite(value)) return "—";
  return value.toFixed(2) + " ms";
}

function formatRate(value: number | null | undefined): string {
  if (typeof value !== "number" || !Number.isFinite(value)) return "—";
  return value.toFixed(1) + "/s";
}

function formatBytes(value: number | null | undefined): string {
  if (typeof value !== "number" || !Number.isFinite(value)) return "—";
  return (value / (1024 * 1024)).toFixed(2) + " MiB";
}

function median(values: number[]): number | null {
  if (!values.length) return null;
  const sorted = values.slice().sort((a, b) => a - b);
  const mid = Math.floor(sorted.length / 2);
  if (sorted.length % 2) return sorted[mid];
  return (sorted[mid - 1] + sorted[mid]) / 2;
}

function setRunning(running: boolean): void {
  state.running = running;
  must<HTMLButtonElement>("compare").disabled = running;
  must<HTMLButtonElement>("run-one").disabled = running;
  must<HTMLButtonElement>("cancel").disabled = !running;
  must<HTMLElement>("presets").querySelectorAll("button").forEach((button) => {
    (button as HTMLButtonElement).disabled = running;
  });
  must<HTMLFormElement>("controls").querySelectorAll("input, select").forEach((el) => {
    (el as HTMLInputElement).disabled = running;
  });
}

function showError(message: string): void {
  const node = must<HTMLParagraphElement>("error");
  node.hidden = !message;
  node.textContent = message;
}

function outcomeLabel(): string {
  if (state.outcome === "cancelled") return "Cancelled";
  if (state.outcome === "failed") return "Failed";
  if (state.progress) return "Finished";
  return "Idle";
}

function renderProgress(): void {
  const progress = state.progress;
  const label = state.activeStrategy
    ? strategyMeta(state.activeStrategy).label
    : state.running
      ? "Starting"
      : outcomeLabel();
  if (!progress) {
    must<HTMLElement>("status").textContent = state.running ? "Starting " + label : outcomeLabel();
    must<HTMLElement>("bar").style.width = "0%";
    must<HTMLElement>("meta").textContent = state.message;
    return;
  }
  const total = progress.total || 0;
  const ratio = total ? Math.min(1, progress.completed / total) : 0;
  must<HTMLElement>("status").textContent = label + " · " + progress.completed + " / " + total;
  must<HTMLElement>("bar").style.width = (ratio * 100).toFixed(1) + "%";
  const parts = [
    progress.elapsed_ms.toFixed(0) + " ms",
    formatRate(progress.throughput_window),
    progress.goroutines + " goroutines",
    formatBytes(progress.heap_bytes),
  ];
  if (state.running) {
    parts.push("p50 " + formatMS(progress.e2e_p50_ms));
    parts.push("p95 " + formatMS(progress.e2e_p95_ms));
  }
  if (state.message) parts.push(state.message);
  must<HTMLElement>("meta").textContent = parts.join(" · ");
}

function focusedRun(): RunJSON | null {
  if (!state.focus) return null;
  const runs = state.results[state.focus.strategy]?.response?.runs;
  if (!runs?.length) return null;
  return runs[Math.min(state.focus.run, runs.length - 1)];
}

function renderCards(): void {
  const host = must<HTMLElement>("cards");
  host.replaceChildren();
  const focused = focusedRun();
  const stats: Array<[string, number | undefined]> = [
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

function renderTable(): void {
  const body = must<HTMLTableElement>("comparison").querySelector("tbody");
  if (!body) return;
  body.replaceChildren();
  for (const strategy of STRATEGIES) {
    const runs = state.results[strategy.id]?.response?.runs;
    if (!runs?.length) continue;
    const mid = median(runs.map((run) => run.throughput));
    runs.forEach((run, index) => {
      const row = document.createElement("tr");
      const selected = state.focus?.strategy === strategy.id && state.focus.run === index;
      if (selected) row.className = "selected";
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
      const select = (): void => {
        state.focus = { strategy: strategy.id, run: index };
        renderTable();
        renderCards();
        drawCharts();
        updateExportButtons();
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

function finitePoints(points: ChartPoint[]): ChartPoint[] {
  return points.filter((point) => Number.isFinite(point.x) && Number.isFinite(point.y));
}

function drawCharts(): void {
  const runIndex = state.focus?.run ?? 0;
  const latency: ChartSeries[] = STRATEGIES.map((strategy) => {
    const runs = state.results[strategy.id]?.response?.runs;
    const run = runs?.length ? runs[Math.min(runIndex, runs.length - 1)] : undefined;
    return {
      label: strategy.label,
      color: strategy.color,
      data: finitePoints((run?.series ?? []).map((point) => ({ x: point.at_ms, y: point.e2e_ms }))),
    };
  });
  const live = (pick: (samples: LiveSamples) => ChartPoint[]): ChartSeries[] =>
    STRATEGIES.map((strategy) => ({
      label: strategy.label,
      color: strategy.color,
      data: finitePoints(pick(state.results[strategy.id]?.live ?? emptyLive())),
    }));

  const runNumber = (state.focus?.run ?? 0) + 1;
  must<HTMLElement>("latency-caption").textContent = state.focus
    ? "End-to-end latency · run " + runNumber
    : "End-to-end latency";
  window.LabCharts.draw(must("latency"), latency, { xLabel: "ms", empty: "Appears when a run finishes" });
  window.LabCharts.draw(must("throughput"), live((samples) => samples.throughput), { xLabel: "ms", empty: "Sampled during the run" });
  window.LabCharts.draw(must("goroutines"), live((samples) => samples.goroutines), { xLabel: "ms" });
  window.LabCharts.draw(must("heap"), live((samples) => samples.heap), { xLabel: "ms" });
}

function renderLegend(): void {
  const list = must<HTMLElement>("legend");
  list.replaceChildren();
  for (const strategy of STRATEGIES) {
    const item = document.createElement("li");
    const swatch = document.createElement("i");
    swatch.style.background = strategy.color;
    item.append(swatch, document.createTextNode(strategy.label));
    list.append(item);
  }
}

function focusedID(): string {
  if (!state.focus) return "";
  return state.results[state.focus.strategy]?.response?.id ?? "";
}

function updateExportButtons(): void {
  const available = !state.running && focusedID() !== "";
  must<HTMLButtonElement>("export-json").disabled = !available;
  must<HTMLButtonElement>("export-csv").disabled = !available;
}

function renderAll(): void {
  renderProgress();
  renderCards();
  renderTable();
  drawCharts();
  updateExportButtons();
}

function applyPreset(name: string): void {
  const preset = PRESETS[name];
  if (!preset || state.running) return;
  must<HTMLSelectElement>("workload").value = "cpu";
  must<HTMLInputElement>("tasks").value = String(preset.tasks);
  must<HTMLInputElement>("concurrency").value = String(preset.concurrency);
  must<HTMLInputElement>("queue_size").value = String(preset.queue_size);
  must<HTMLInputElement>("runs").value = String(preset.runs);
  must<HTMLInputElement>("iterations").value = String(preset.iterations);
  syncWorkloadFields();
}

function downloadExport(kind: "json" | "csv"): void {
  const id = focusedID();
  if (!id) return;
  const link = document.createElement("a");
  link.href = "/api/benchmarks/" + id + "/export." + kind;
  link.download = "benchmark-" + id + "." + kind;
  link.click();
}

function emptyLive(): LiveSamples {
  return { throughput: [], goroutines: [], heap: [] };
}

function trim(series: ChartPoint[]): void {
  if (series.length <= 360) return;
  const next: ChartPoint[] = [];
  for (let i = 0; i < series.length; i += 2) next.push(series[i]);
  series.splice(0, series.length, ...next);
}

function pushLive(strategy: StrategyId, progress: ProgressJSON): void {
  const entry = state.results[strategy];
  if (!entry) return;
  const x = progress.elapsed_ms;
  entry.live.throughput.push({ x, y: progress.throughput_window });
  entry.live.goroutines.push({ x, y: progress.goroutines });
  entry.live.heap.push({ x, y: progress.heap_bytes / (1024 * 1024) });
  trim(entry.live.throughput);
  trim(entry.live.goroutines);
  trim(entry.live.heap);
}

function watch(id: string, strategy: StrategyId): Promise<string> {
  return new Promise((resolve, reject) => {
    const source = new EventSource("/api/benchmarks/" + id + "/stream");
    activeSocket = source;
    let settled = false;
    const finish = (fn: () => void): void => {
      if (settled) return;
      settled = true;
      source.close();
      if (activeSocket === source) activeSocket = null;
      fn();
    };
    source.addEventListener("progress", (event) => {
      const progress = JSON.parse((event as MessageEvent<string>).data) as ProgressJSON;
      state.progress = progress;
      pushLive(strategy, progress);
      renderProgress();
      drawCharts();
    });
    source.addEventListener("done", (event) => {
      const payload = JSON.parse((event as MessageEvent<string>).data) as { status: string };
      finish(() => {
        fetch("/api/benchmarks/" + id)
          .then((res) => res.json() as Promise<BenchmarkJSON>)
          .then((full) => {
            const entry = state.results[strategy];
            if (entry) entry.response = full;
            const count = full.runs?.length ?? 0;
            state.focus = { strategy, run: Math.max(0, count - 1) };
            const env = full.environment;
            if (env?.go_version) {
              state.message = env.go_version + " · GOMAXPROCS " + env.gomaxprocs + " · " + env.num_cpu + " CPUs";
            }
            renderAll();
            state.outcome = payload.status;
            if (payload.status === "failed") {
              reject(new Error(full.error || "benchmark failed"));
              return;
            }
            if (payload.status === "cancelled") stopQueue = true;
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

async function runStrategy(strategy: StrategyId): Promise<string> {
  state.activeStrategy = strategy;
  state.progress = null;
  state.results[strategy] = { live: emptyLive(), response: null };
  renderAll();
  const res = await fetch("/api/benchmarks", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(configFor(strategy)),
  });
  const body = (await res.json().catch(() => ({}))) as { id?: string; error?: string };
  if (!res.ok || !body.id) throw new Error(body.error || "HTTP " + res.status);
  state.activeId = body.id;
  if (stopQueue) {
    void fetch("/api/benchmarks/" + body.id + "/cancel", { method: "POST" });
  }
  return watch(body.id, strategy);
}

async function runQueue(strategies: StrategyId[]): Promise<void> {
  if (state.running) return;
  showError("");
  const key = configKey();
  if (strategies.length > 1 || key !== state.configKey) {
    state.results = {};
    state.focus = null;
    state.configKey = key;
  }
  stopQueue = false;
  state.outcome = "";
  setRunning(true);
  try {
    for (const strategy of strategies) {
      if (stopQueue) break;
      await runStrategy(strategy);
    }
  } catch (err) {
    showError(err instanceof Error ? err.message : "run failed");
  } finally {
    state.activeStrategy = "";
    state.activeId = "";
    setRunning(false);
    renderAll();
  }
}

function syncWorkloadFields(): void {
  const sleep = must<HTMLSelectElement>("workload").value === "sleep";
  must<HTMLElement>("iterations-field").hidden = sleep;
  must<HTMLElement>("task-ms-field").hidden = !sleep;
  must<HTMLElement>("sleep-note").hidden = !sleep;
}

function selectedStrategy(): StrategyId {
  const value = must<HTMLSelectElement>("strategy").value;
  if (STRATEGIES.some((item) => item.id === value)) return value as StrategyId;
  return "sequential";
}

must<HTMLFormElement>("controls").addEventListener("submit", (event) => {
  event.preventDefault();
});
must<HTMLSelectElement>("workload").addEventListener("change", syncWorkloadFields);
must<HTMLButtonElement>("compare").addEventListener("click", () => {
  void runQueue(STRATEGIES.map((item) => item.id));
});
must<HTMLButtonElement>("run-one").addEventListener("click", () => {
  void runQueue([selectedStrategy()]);
});
must<HTMLButtonElement>("cancel").addEventListener("click", () => {
  stopQueue = true;
  if (!state.activeId) return;
  void fetch("/api/benchmarks/" + state.activeId + "/cancel", { method: "POST" });
});
must<HTMLElement>("presets").querySelectorAll("button").forEach((button) => {
  button.addEventListener("click", () => applyPreset((button as HTMLButtonElement).dataset.preset ?? ""));
});
must<HTMLButtonElement>("export-json").addEventListener("click", () => downloadExport("json"));
must<HTMLButtonElement>("export-csv").addEventListener("click", () => downloadExport("csv"));
window.addEventListener("resize", drawCharts);

renderLegend();
syncWorkloadFields();
renderAll();
