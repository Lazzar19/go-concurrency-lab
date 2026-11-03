# Go concurrency lab

A dashboard for one synthetic workload under four scheduling strategies: a sequential baseline, unbounded fan-out, semaphore-limited fan-out, and a fixed worker pool. The server runs one benchmark at a time, so the strategies do not share the CPU.

## Run with Go

Requires Go 1.27.

```bash
go run ./cmd/server
```

Open [http://localhost:8080](http://localhost:8080). The process listens on `0.0.0.0`. `PORT` changes the port.

```bash
PORT=9090 go run ./cmd/server
```

CPU results follow `GOMAXPROCS`. The default is the number of logical CPUs Go can see.

## Run with Docker

```bash
docker compose up --build
```

The page is at [http://127.0.0.1:8080](http://127.0.0.1:8080). Compose publishes that address on localhost.

Set `GOMAXPROCS` when you want two machines, or two container limits, to run the same CPU comparison:

```bash
GOMAXPROCS=2 docker compose up --build
```

An empty value leaves Go's default, which is the CPU count visible inside the container. `/health` returns `ok` once the process is listening.

## Test

```bash
go test -race ./...
```

The page source is TypeScript in `web/src`. The server embeds the compiled files in `web/build`.

```bash
cd web
npm ci
npm run build
```

CI runs the race tests on every push and pull request, then rebuilds the client and fails if `web/build` does not match the source.

## Presets

The sidebar presets fill the form. They stay inside the server caps: 100,000 tasks and 10,000 concurrency.

| Preset | Tasks | Concurrency | Queue | Runs | Iterations |
| --- | ---: | ---: | ---: | ---: | ---: |
| Light | 200 | 4 | 0 | 1 | 2,000 |
| Medium | 2,000 | 8 | 32 | 3 | 8,000 |
| Heavy | 20,000 | 16 | 128 | 3 | 20,000 |

Three repetitions show noise. They are not a statistical proof. Percentiles stay on each run. Only throughput is summarized with a median.

## Export

After a run finishes, select its row.

- JSON contains every task sample: completion time, end-to-end latency, and service time.
- CSV contains one summary row per repetition.

A run that is still in progress cannot be exported.
