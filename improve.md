# Plan za poboljšanje `README.md`

Ovaj dokument navodi sve predložene izmene za unapređenje dokumentacije projekta `go-concurrency-lab`.

## Cilj

README treba da bude više od kratkog uputstva za pokretanje. Treba da objasni:

- šta projekat demonstrira;
- koje concurrency strategije poredi;
- koje workload-e podržava;
- kako tumačiti rezultate;
- kako reproducirati poređenja;
- kako koristiti HTTP API i export;
- kako razvijati i testirati projekat.

## 1. Naslov i uvodni opis

Zadržati naslov:

```md
# Go Concurrency Lab
```

Dodati jasniji uvod:

```md
Interactive dashboard for comparing Go concurrency strategies under
CPU-bound and sleep-based workloads.

The project compares sequential execution, unbounded fan-out,
semaphore-limited fan-out, and a fixed worker pool while reporting
throughput, latency percentiles, goroutine usage, and heap usage.
```

Ako postoji screenshot ili GIF dashboarda, postaviti ga odmah ispod uvoda.

Uvod treba kratko da odgovori:

- šta projekat radi;
- kome je namenjen;
- koje strategije poredi;
- koje metrike prikazuje;
- da li je namenjen učenju i eksperimentisanju.

## 2. Sekcija `What it demonstrates`

Dodati opis glavnih strategija:

- `sequential` — jedan task se izvršava nakon drugog;
- `fanout` — kreira se goroutine za svaki task;
- `semaphore` — fan-out uz ograničen broj aktivnih taskova;
- `worker_pool` — fiksan broj worker-a obrađuje queue.

Dodati tabelu:

| Strategy | Description | Main trade-off |
| --- | --- | --- |
| `sequential` | One task at a time | Simplest baseline, no concurrency |
| `fanout` | One goroutine per task | Maximum fan-out, more goroutine overhead |
| `semaphore` | Fan-out with a concurrency limit | Bounded active work |
| `worker_pool` | Fixed workers consuming a queue | Predictable worker count and queueing |

Navesti i workload-e:

- `cpu` — CPU-bound sintetički rad;
- `sleep` — čekanje koje oslobađa processor.

## 3. Sekcija `Quick start`

Navesti zahtevanu Go verziju i osnovno pokretanje:

```bash
go run ./cmd/server
```

Objasniti:

- default adresu `127.0.0.1:8080`;
- da `HOST` menja bind adresu;
- da `PORT` menja port;
- da `HOST=0.0.0.0` izlaže servis na svim interfejsima.

Primer promene porta:

```bash
PORT=9090 go run ./cmd/server
```

Primer eksplicitnog bind-a:

```bash
HOST=0.0.0.0 PORT=9090 go run ./cmd/server
```

Windows PowerShell primer:

```powershell
$env:HOST = "127.0.0.1"
$env:PORT = "9090"
go run ./cmd/server
```

Napomenuti da CPU rezultati zavise od `GOMAXPROCS`, čiji je default broj logičkih CPU-a koje Go vidi.

## 4. Sekcija `Run with Docker`

Zadržati osnovnu komandu:

```bash
docker compose up --build
```

Dokumentovati:

- dashboard na `http://127.0.0.1:8080`;
- da Compose objavljuje port samo na localhost;
- da container koristi `HOST=0.0.0.0`;
- da image koristi non-root korisnika;
- da `/health` vraća `ok`;
- da Docker CPU limit i throttling mogu uticati na rezultate.

Unix primer za `GOMAXPROCS`:

```bash
GOMAXPROCS=2 docker compose up --build
```

Windows PowerShell primer:

```powershell
$env:GOMAXPROCS = "2"
docker compose up --build
```

Objasniti da `GOMAXPROCS` treba držati istim kada se porede rezultati na različitim mašinama ili u različitim container limitima.

## 5. Sekcija `How to read the results`

Objasniti sledeće metrike:

- **Throughput** — broj završenih taskova u sekundi;
- **End-to-end latency** — vreme od submit-a do završetka taska;
- **Service latency** — vreme provedeno u samom workload-u;
- **P50** — sredina distribucije i tipičniji rezultat;
- **P95/P99** — sporiji, repni deo distribucije;
- **Goroutines peak** — maksimalan broj goroutine-a tokom run-a;
- **Heap peak** — maksimalna prijavljena heap memorija.

Dodati objašnjenje:

```text
End-to-end latency = queue/wait time + service time
Service latency = time spent executing the workload
```

Napomenuti da veći `P95` ili `P99` može ukazivati na queueing ili neujednačeno raspoređivanje taskova, čak i kada je throughput dobar.

## 6. Dokumentovati workload-e

### CPU workload

Objasniti da:

- koristi hashing kao sintetički CPU rad;
- trajanje zavisi od `Iterations`, `Bytes`, CPU-a i `GOMAXPROCS`;
- služi za poređenje CPU scheduling ponašanja;
- nije zamena za realan poslovni workload.

### Sleep workload

Objasniti da:

- svaki task čeka `TaskMS`;
- čekanje oslobađa processor;
- pogodniji je za posmatranje workload-a koji nije CPU-bound;
- veća konkurentnost može povećati throughput dok taskovi čekaju.

Naglasiti da CPU i sleep rezultate ne treba tumačiti na isti način.

## 7. Ažurirati dokumentaciju limita

README treba da navede sve limite iz `internal/benchmark/config.go`, a ne samo `Tasks` i `Concurrency`.

| Parameter | Limit |
| --- | ---: |
| `Tasks` | `1–100,000` |
| `Concurrency` | `0–10,000`, odnosno `1–10,000` za `semaphore` i `worker_pool` |
| `Runs` | `1–20` |
| `QueueSize` | `0–100,000` |
| `Iterations` | najviše `100,000,000` |
| `Bytes` | najviše `16 MiB` |
| `TaskMS` | `1–60,000 ms` |
| `Tasks * Runs` | najviše `100,000` |

Objasniti da:

- `QueueSize` ima praktično značenje samo za `worker_pool`;
- `Concurrency` nije potreban na isti način za sve strategije;
- `Iterations` i `Bytes` se koriste za `cpu`;
- `TaskMS` se koristi za `sleep`;
- `Tasks * Runs` ograničava veličinu progress monitora i broj sačuvanih uzoraka.

## 8. Poboljšati sekciju `Presets`

Zadržati postojeću tabelu preset-a, ali dodati objašnjenje kolona:

- `Tasks` — broj taskova po run-u;
- `Concurrency` — broj aktivnih taskova ili worker-a, u zavisnosti od strategije;
- `Queue` — veličina worker-pool queue-a;
- `Runs` — broj ponavljanja;
- `Iterations` — količina CPU rada po tasku.

Dodati napomenu:

```md
Presets are starting points for exploration, not canonical benchmark settings.
For meaningful comparisons, keep the same preset and environment.
```

Navesti da `Queue` ima efekta samo kod `worker_pool`.

## 9. Dodati sekciju `Methodology and limitations`

Obavezno navesti:

- u jednom trenutku radi samo jedan benchmark;
- strategije se izvršavaju odvojeno;
- strategije ne dele CPU tokom istog benchmark-a;
- CPU rezultati zavise od `GOMAXPROCS`;
- background procesi mogu promeniti rezultate;
- Docker CPU limit i throttling mogu uticati na merenja;
- tri repetitions pokazuju varijaciju, ali nisu formalna statistička analiza;
- percentili se računaju po pojedinačnom run-u;
- throughput se sažima median vrednošću;
- workload je sintetički i ne predstavlja realnu aplikaciju.

Predloženi tekst:

```md
These measurements are intended for learning and controlled comparison.
They are not a replacement for a formal benchmark suite. Run comparisons
on the same machine with the same Go version, `GOMAXPROCS`, workload
configuration, and container limits.
```

Dodati i preporuku da se pri poređenju drže istim:

- Go version;
- `GOMAXPROCS`;
- broj taskova;
- broj iteracija;
- broj ponavljanja;
- Docker CPU limit;
- operativni sistem ili container okruženje.

## 10. Dodati sekciju `HTTP API`

Dokumentovati endpoint-e:

```text
GET  /health
POST /api/benchmarks
GET  /api/benchmarks/{id}
POST /api/benchmarks/{id}/cancel
GET  /api/benchmarks/{id}/stream
GET  /api/benchmarks/{id}/export.json
GET  /api/benchmarks/{id}/export.csv
```

Za svaki endpoint navesti osnovnu namenu.

Dokumentovati status kodove:

- `202 Accepted` — benchmark je prihvaćen i pokrenut;
- `200 OK` — status, rezultat ili export je uspešno vraćen;
- `400 Bad Request` — nevalidan JSON, `Content-Type` ili konfiguracija;
- `404 Not Found` — benchmark ID ne postoji;
- `409 Conflict` — drugi benchmark već radi ili export još nije moguć.

Dodati primer request-a:

```bash
curl -X POST http://127.0.0.1:8080/api/benchmarks \
  -H "Content-Type: application/json" \
  -d '{
    "strategy": "worker_pool",
    "workload": "sleep",
    "tasks": 100,
    "concurrency": 4,
    "queue_size": 16,
    "task_ms": 10,
    "runs": 3
  }'
```

Objasniti da prekid SSE konekcije samo prekida praćenje napretka. Benchmark se eksplicitno otkazuje preko:

```text
POST /api/benchmarks/{id}/cancel
```

## 11. Proširiti sekciju `Export`

Objasniti:

- JSON sadrži sve task sample-ove;
- CSV sadrži summary row po repetition-u;
- export nije dostupan dok benchmark traje;
- JSON je pogodan za detaljnu analizu;
- CSV je pogodan za Excel, pandas ili statističku obradu.

Dodati URL primere:

```text
http://127.0.0.1:8080/api/benchmarks/{id}/export.json
http://127.0.0.1:8080/api/benchmarks/{id}/export.csv
```

## 12. Dodati sekciju `Project structure`

Predloženi sadržaj:

```text
cmd/server/          HTTP server entrypoint and lifecycle
internal/api/        HTTP handlers, SSE, validation and exports
internal/benchmark/  workloads, strategies, metrics and runner
internal/storage/    in-memory benchmark records
web/src/             TypeScript dashboard source
web/build/           Compiled frontend embedded in the binary
```

Dodati napomenu da se `web/build` regeneriše kada se promeni frontend source.

## 13. Srediti sekciju `Development and testing`

Go testovi:

```bash
go test -race ./...
```

Frontend build:

```bash
cd web
npm ci
npm run build
```

Windows PowerShell:

```powershell
Set-Location web
npm ci
npm run build
```

Objasniti da CI:

- pokreće Go race tests na push i pull request;
- instalira frontend dependencies;
- gradi TypeScript;
- proverava da nema razlike između source-a i `web/build`.

## 14. Dodati sekciju `Troubleshooting`

### Promena porta

```bash
PORT=9090 go run ./cmd/server
```

```powershell
$env:PORT = "9090"
go run ./cmd/server
```

### Provera health endpoint-a

```bash
curl http://127.0.0.1:8080/health
```

Očekivani rezultat:

```text
ok
```

### Docker servis nije healthy

Proveriti:

```text
http://127.0.0.1:8080/health
```

### Frontend build nije ažuriran

```bash
cd web
npm ci
npm run build
git diff -- web/build
```

## 15. Dodati sekciju `License`

Ako licenca postoji:

```md
## License

This project is licensed under the MIT License. See [LICENSE](LICENSE).
```

README ne treba da navodi licencu koja nije stvarno prisutna u repozitorijumu. Ako `LICENSE` fajl ne postoji, prvo izabrati licencu i dodati fajl.

## Predloženi redosled rada

1. Napraviti novu strukturu README-ja.
2. Prebaciti postojeće Go i Docker komande u sekciju `Quick start`.
3. Dodati opis strategija i workload-a.
4. Dodati sekciju o metrikama.
5. Dodati metodologiju i ograničenja.
6. Uskladiti sve limite sa `internal/benchmark/config.go`.
7. Dodati API i export dokumentaciju.
8. Dodati Windows PowerShell primere.
9. Dodati strukturu projekta i razvojne komande.
10. Dodati troubleshooting.
11. Dodati screenshot ili GIF, ako postoji.
12. Dodati ili dokumentovati licencu.
13. Proveriti da svi linkovi, endpoint-i, environment varijable i vrednosti odgovaraju kodu.
14. Pokrenuti Markdown preview i uraditi završni proofread.

## Završna checklist-a

- [ ] README ne tvrdi da se ponašanje razlikuje od implementacije.
- [ ] `HOST`, `PORT` i `GOMAXPROCS` primeri su tačni.
- [ ] Svi limiti odgovaraju `internal/benchmark/config.go`.
- [ ] Opis SSE ponašanja odgovara `internal/api/sse.go`.
- [ ] Opis graceful shutdown-a odgovara `cmd/server/main.go`.
- [ ] Svi API endpoint-i postoje u `internal/api/handlers.go`.
- [ ] Export opis odgovara `internal/api/export.go`.
- [ ] Komande rade iz root direktorijuma projekta.
- [ ] Linux/macOS i Windows primeri su odvojeni i jasni.
- [ ] README razlikuje edukativni eksperiment od formalnog benchmark-a.
- [ ] License sekcija odgovara stvarnom `LICENSE` fajlu.
- [ ] Uvod je kratak, a detalji su organizovani u kasnijim sekcijama.
