# Demo Recording

This directory contains the automated demo recording for the Go Concurrency Lab dashboard.

## Prerequisites

- **Go 1.27.1+** (from `go.mod`)
- **Node.js 22+** for Playwright
- **ffmpeg** for GIF conversion
- **Unix-like environment** (Linux, macOS, WSL on Windows)

## Installation

```bash
cd docs
npm ci
npx playwright install chromium
```

## Usage

Run the complete recording workflow:

```bash
cd docs
node record-demo.mjs
```

This script:
1. Verifies port 8080 is available (rejects if occupied)
2. Starts Go server on port 8080 (tracks exact PID)
3. Waits for `/health` endpoint
4. Launches headless Chrome browser (1280x800)
5. Fills form with tuned config and clicks "Compare all"
6. Captures screenshots at ~12fps during benchmark
7. Scrolls to verify all 4 table rows are visible
8. If any row is cut off, scrolls to document bottom
9. Throws error if table incomplete after adjustment
10. Stops capture after 2+ second final hold
11. Generates optimized GIF with ffmpeg palette (2x time compression)
12. Validates GIF and extracts proof frames to `/tmp` only
13. Stops server using exact PID (no broad pkill)
14. Verifies no leaked processes (asserts, not warns)
15. Runs entire process TWICE for validation
16. Runs Go tests and checks git diff

## Output

- **`demo.gif`** - Optimized GIF (8-15 seconds target, ~10-12s typical, 1280x800, under 1MB)
- All temporary files (frames, palette, proof frames, server binary) are stored in a unique work directory
- Work directory is automatically cleaned up on both success and failure
- Optional: proof frames may be copied to `/opt/cursor/artifacts/` for audit

No persistent temporary files are created in the repository or fixed /tmp paths.

## Demo Configuration

The recording uses a **tuned sleep-based workload** for visible progress:

- **Workload:** sleep (not CPU-bound)
- **Tasks:** 400
- **Concurrency:** 8
- **Queue size:** 32 (worker_pool only)
- **Runs:** 1 per strategy
- **Task duration:** 60ms per task

This configuration was chosen because:
- CPU workload completes too fast (<1 second)
- Sleep workload provides deterministic ~10-12 second demo
- Shows all 4 strategies: sequential, fanout, semaphore, worker_pool

**Not using preset buttons** - config is hardcoded in script for reproducibility.

## Time Compression

The GIF uses **2x time compression** (timelapse):
- Capture: ~20 seconds of real time
- GIF: ~10 seconds playback (8-15s enforced)
- Method: ffmpeg `setpts=0.5*PTS` filter with 12fps input/output
- All segments preserved (no truncation)
- Final table segment: 2+ seconds held in capture
- Validation: actual final decoded GIF frame shows all 4 table rows

## Safety Features

- **Port check:** Real TCP bind test (node:net) - rejects if port 8080 already occupied
- **Unique temp binary:** Server built to unique path in work directory
- **Exact PID tracking:** Only terminates the owned spawned server process
- **No broad pkill/pgrep:** Will not interfere with other processes
- **Exit handling:** Waits for actual process exit (not just signal sent)
- **Assertion on leaks:** Throws error if server process not cleaned up
- **Table verification:** Asserts EXACTLY 4 rows and all in viewport - zero/fewer rows fail
- **Timeout handling:** Throws error (not warning) if benchmark incomplete
- **Work directory cleanup:** All temporary files cleaned in finally block (success or failure)

## Regeneration

To regenerate the demo:

```bash
cd docs
node record-demo.mjs
```

The script is idempotent and includes cleanup on both success and failure paths.
Only the exact spawned server process is managed - no interference with other services.

## Requirements

- Unix-like environment (Linux, macOS, or WSL on Windows)
- Port 8080 must be available (tested via TCP bind before start)
- Temporary directory space for frames (~25MB in unique work directory)
- Sufficient permissions to spawn processes

## Troubleshooting

**Port 8080 occupied:**
```bash
# Find what's using the port
lsof -i :8080

# If it's your old test server, kill it specifically by PID
kill <PID>
```

**Browser not found:**
```bash
npx playwright install chromium
```

**ffmpeg not found:**
```bash
# Ubuntu/Debian
sudo apt-get install ffmpeg

# macOS
brew install ffmpeg
```

**Script fails during capture:**
- Check server logs in script output
- Verify dashboard loads at http://127.0.0.1:8080
- Check /tmp has space for frames

## Cleanup

The script automatically cleans up (in finally block):
- Unique work directory with all temporary files (frames, palette, proof frames, server binary)
- Spawned server process (exact owned PID only via SIGTERM then SIGKILL if needed)
- Cleanup is idempotent and runs on both success and failure

On interruption (Ctrl+C), cleanup handlers ensure:
- Server process is terminated (owned PID only)
- No processes are leaked (assertion enforced)
- No broad pkill commands are used
