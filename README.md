# Go concurrency lab

A small dashboard that runs one synthetic workload under four scheduling strategies and shows latency, throughput, and resource use.

The strategies are a sequential baseline, unbounded fan-out, semaphore-limited fan-out, and a fixed worker pool. The page runs them one at a time in a single process, so they do not compete for the CPU.

## Run

Requires Go 1.27.

```bash
go run ./cmd/server
```

Open [http://localhost:8080](http://localhost:8080). The server listens on `0.0.0.0`. Set `PORT` to change the port.

```bash
PORT=9090 go run ./cmd/server
```

## Presets

The sidebar presets fill the form. They stay inside the server caps (100,000 tasks, 10,000 concurrency).

| Preset | Tasks | Concurrency | Queue | Runs | Iterations |
| --- | ---: | ---: | ---: | ---: | ---: |
| Light | 200 | 4 | 0 | 1 | 2,000 |
| Medium | 2,000 | 8 | 32 | 3 | 8,000 |
| Heavy | 20,000 | 16 | 128 | 3 | 20,000 |

Three repetitions show noise. They are not a statistical proof. Percentiles stay on each run. Only throughput is summarized with a median.

## Export

After a run finishes, select its row and use the export buttons.

- JSON contains every task sample: completion time, end-to-end latency, and service time.
- CSV contains one summary row per repetition and no raw samples.

A run that is still in progress cannot be exported.

## Change the page

The client is TypeScript in `web/src`. The server embeds the compiled files in `web/build`.

```bash
cd web
npm install
npm run build
```
