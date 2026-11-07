#!/usr/bin/env node
/**
 * Go Concurrency Lab Demo Recording Script
 * 
 * Prerequisites:
 *   - Go 1.27.1+ (from go.mod)
 *   - Node.js 22+ for Playwright
 *   - ffmpeg for GIF conversion
 * 
 * Installation:
 *   cd docs && npm ci && npx playwright install chromium
 * 
 * Usage:
 *   node record-demo.mjs
 * 
 * This script:
 *   1. Starts the Go server on port 8080
 *   2. Drives browser automation to interact with dashboard
 *   3. Captures screenshots during "Compare all" benchmark run
 *   4. Converts frames to optimized GIF with ffmpeg palette
 *   5. Validates GIF quality and extracts proof frames
 *   6. Cleans up server and child processes
 */

import { chromium } from 'playwright';
import { spawn } from 'child_process';
import { writeFileSync, mkdirSync, rmSync, existsSync, readdirSync, readFileSync, mkdtempSync, cpSync } from 'fs';
import { join, dirname } from 'path';
import { fileURLToPath } from 'url';
import { createServer } from 'net';
import { tmpdir } from 'os';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);
const WORKSPACE = join(__dirname, '..');
const PORT = 8080;
const BASE_URL = `http://127.0.0.1:${PORT}`;

let serverProcess = null;
let serverPid = null;
let workDir = null;
let activeBrowser = null;

// Demo configuration (tuned for ~10-12 seconds total, visible progress)
const DEMO_CONFIG = {
  workload: 'sleep',
  tasks: 400,
  concurrency: 8,
  queue_size: 32,
  runs: 1,
  task_ms: 60
};

function log(message) {
  console.log(`[${new Date().toISOString()}] ${message}`);
}

function exec(command, args = [], options = {}) {
  return new Promise((resolve, reject) => {
    const proc = spawn(command, args, { stdio: 'pipe', ...options });
    let stdout = '';
    let stderr = '';
    proc.stdout?.on('data', (data) => stdout += data);
    proc.stderr?.on('data', (data) => stderr += data);
    proc.on('close', (code) => {
      if (code === 0) resolve({ stdout, stderr });
      else reject(new Error(`${command} failed: ${code}\n${stderr}`));
    });
  });
}

async function waitForHealth(maxAttempts = 30) {
  log('Waiting for server health endpoint...');
  for (let i = 0; i < maxAttempts; i++) {
    try {
      const response = await fetch(`${BASE_URL}/health`);
      if (response.ok) {
        const text = await response.text();
        if (text.includes('ok')) {
          log('Server is healthy');
          return true;
        }
      }
    } catch (err) {
      // Server not ready yet
    }
    await new Promise(resolve => setTimeout(resolve, 1000));
  }
  throw new Error('Server health check timeout');
}

async function checkPortAvailable() {
  // Test real TCP binding using node:net exclusive listen
  return new Promise((resolve, reject) => {
    const server = createServer();
    server.once('error', (err) => {
      if (err.code === 'EADDRINUSE') {
        reject(new Error(`Port ${PORT} is already in use. Cannot start server.`));
      } else {
        reject(err);
      }
    });
    server.once('listening', () => {
      server.close(() => {
        log(`Port ${PORT} is available`);
        resolve(true);
      });
    });
    server.listen(PORT, '127.0.0.1');
  });
}

async function startServer() {
  log('Starting Go server...');
  
  // Preflight: test real TCP binding (bug fix #1)
  await checkPortAvailable();
  
  // Build server to unique temp binary in workDir
  const serverBinaryPath = join(workDir, 'server');
  log(`Building server to ${serverBinaryPath}...`);
  await exec('go', ['build', '-o', serverBinaryPath, './cmd/server'], { cwd: WORKSPACE });
  
  return new Promise((resolve, reject) => {
    // Spawn the built binary directly (exact PID control)
    serverProcess = spawn(serverBinaryPath, [], {
      cwd: WORKSPACE,
      env: { ...process.env, PORT: String(PORT), HOST: '127.0.0.1' },
      stdio: ['ignore', 'pipe', 'pipe'],
      detached: false
    });

    let errorOccurred = false;
    serverProcess.on('error', (err) => {
      errorOccurred = true;
      reject(err);
    });
    
    serverProcess.stdout.on('data', (data) => {
      const line = data.toString();
      if (line.includes('listening') || line.includes('started')) {
        log(`Server output: ${line.trim()}`);
      }
      // Check for bind errors
      if (line.includes('address already in use')) {
        errorOccurred = true;
        reject(new Error('Server failed to bind: address already in use'));
      }
    });

    serverProcess.stderr.on('data', (data) => {
      const stderr = data.toString().trim();
      if (stderr && !stderr.includes('exit status')) {
        log(`Server stderr: ${stderr}`);
        if (stderr.includes('address already in use')) {
          errorOccurred = true;
          reject(new Error('Server failed to bind: address already in use'));
        }
      }
    });

    serverPid = serverProcess.pid;
    log(`Server process started (PID: ${serverPid})`);
    
    // Give it a moment to initialize
    setTimeout(() => {
      if (!errorOccurred) resolve();
    }, 2000);
  });
}

async function stopServer() {
  log('Stopping server...');
  
  // Idempotent cleanup
  if (!serverProcess || !serverPid) {
    log('No server process to stop');
    return;
  }

  const pid = serverPid;
  
  // Try graceful shutdown first (kills only our owned process)
  try {
    serverProcess.kill('SIGTERM');
    log(`Sent SIGTERM to owned PID ${pid}`);
  } catch (err) {
    log(`Error sending SIGTERM: ${err.message}`);
  }
  
  // Wait for actual exit with tagged result (bug fix: avoid undefined race)
  const exitPromise = new Promise(resolve => {
    serverProcess.once('close', (code) => {
      log(`Owned server process ${pid} exited with code ${code}`);
      resolve('exited');
    });
  });
  
  // Bounded timeout
  const timeoutPromise = new Promise(resolve => setTimeout(() => resolve('timeout'), 5000));
  const result = await Promise.race([exitPromise, timeoutPromise]);
  
  if (result === 'timeout') {
    // Check if process is actually still alive before SIGKILL
    try {
      process.kill(pid, 0); // Signal 0 checks existence without killing
      log(`Owned process ${pid} did not exit, sending SIGKILL`);
      process.kill(pid, 'SIGKILL');
      // Await actual exit/reap
      await exitPromise;
    } catch (err) {
      if (err.code === 'ESRCH') {
        log(`Process ${pid} already exited during timeout window`);
      } else {
        log(`Error sending SIGKILL: ${err.message}`);
      }
    }
  }
  // If result === 'exited', process already stopped - do NOT send any kill

  // Clear references AFTER exit verification complete
  serverProcess = null;
  serverPid = null;

  // Wait for TCP socket to be fully released by the kernel
  await new Promise(resolve => setTimeout(resolve, 2000));
  log('Server stopped (only owned process terminated)');
}

async function verifyNoLeakedProcesses() {
  // Verify only our spawned server process is cleaned up
  // Do NOT kill or check other processes
  if (serverPid) {
    try {
      // Check if our specific owned PID still exists
      process.kill(serverPid, 0); // Signal 0 = check existence only
      log(`ERROR: Owned server process ${serverPid} is still running`);
      throw new Error(`Leaked owned server process: PID ${serverPid}`);
    } catch (err) {
      if (err.code === 'ESRCH') {
        // Process does not exist - correct
        log(`Verified owned server process ${serverPid} is stopped`);
        return true;
      }
      throw err;
    }
  }
  log('No owned server process to verify');
  return true;
}

async function captureFrames(page) {
  log('Starting frame capture...');
  
  // Create frames directory in workDir (bug fix #4)
  const framesDir = join(workDir, 'frames');
  mkdirSync(framesDir, { recursive: true });

  let frameCount = 0;
  const captureInterval = 83; // ~12fps
  let capturing = true;

  const captureLoop = async () => {
    while (capturing) {
      try {
        await page.screenshot({
          path: join(framesDir, `frame-${String(frameCount).padStart(5, '0')}.png`),
          fullPage: false
        });
        frameCount++;
      } catch (err) {
        log(`Screenshot error: ${err.message}`);
      }
      await new Promise(resolve => setTimeout(resolve, captureInterval));
    }
  };

  const capturePromise = captureLoop();

  return {
    stop: async () => {
      capturing = false;
      await capturePromise;
      log(`Captured ${frameCount} frames`);
      return frameCount;
    }
  };
}

async function generateGIF() {
  log('Generating GIF with ffmpeg palette...');
  
  const framesDir = join(workDir, 'frames');
  const gifPath = join(__dirname, 'demo.gif');
  const palettePath = join(workDir, 'palette.png');

  // Generate palette for better color quality
  // Intentional 2x timelapse: ~20s real capture → ~10s GIF playback
  log('Generating color palette...');
  await exec('ffmpeg', [
    '-y',
    '-framerate', '12', // Input: 12fps (explicit)
    '-pattern_type', 'glob',
    '-i', join(framesDir, 'frame-*.png'),
    '-vf', 'scale=1280:-1:flags=lanczos,palettegen=stats_mode=diff',
    palettePath
  ]);

  // Generate GIF with 2x timelapse compression via setpts filter
  // All segments preserved including final table segment
  log('Creating GIF with 2x timelapse (setpts=0.5*PTS)...');
  await exec('ffmpeg', [
    '-y',
    '-framerate', '12', // Input: 12fps
    '-pattern_type', 'glob',
    '-i', join(framesDir, 'frame-*.png'),
    '-i', palettePath,
    '-filter_complex', 'setpts=0.5*PTS,scale=1280:-1:flags=lanczos,fps=12[x];[x][1:v]paletteuse=dither=bayer:bayer_scale=5',
    gifPath
  ]);

  log(`GIF created: ${gifPath}`);
  return gifPath;
}

async function validateGIF(gifPath) {
  log('Validating GIF...');
  
  // Check if file exists and get size
  const { stdout: sizeOutput } = await exec('du', ['-h', gifPath]);
  const sizeMatch = sizeOutput.match(/^([\d.]+[KMG]?)\s/);
  const fileSize = sizeMatch ? sizeMatch[1] : 'unknown';
  log(`GIF size: ${fileSize}`);

  // Use ffprobe to validate and get metadata
  const { stdout: probeOutput } = await exec('ffprobe', [
    '-v', 'error',
    '-select_streams', 'v:0',
    '-show_entries', 'stream=width,height,duration,nb_frames',
    '-of', 'default=noprint_wrappers=1',
    gifPath
  ]);

  log('GIF metadata:');
  log(probeOutput);

  // Extract proof frames in workDir (bug fix #4)
  const framesDir = join(workDir, 'frames');
  const proofDir = join(workDir, 'proof-frames');
  mkdirSync(proofDir, { recursive: true });

  // Get total frame count
  const frames = readdirSync(framesDir).filter(f => f.startsWith('frame-')).sort();
  const totalFrames = frames.length;
  
  if (totalFrames === 0) {
    throw new Error('No frames captured!');
  }

  // Extract representative frames
  const indices = [
    0, // Start
    Math.floor(totalFrames * 0.3), // Early progress
    Math.floor(totalFrames * 0.7), // Late progress
    totalFrames - 1 // Final
  ];

  for (const [i, idx] of indices.entries()) {
    const frame = frames[idx];
    const labels = ['start', 'early', 'late', 'final'];
    await exec('cp', [
      join(framesDir, frame),
      join(proofDir, `proof-${labels[i]}.png`)
    ]);
  }

  log(`Proof frames extracted to ${proofDir} in work directory`);

  return {
    size: fileSize,
    frameCount: totalFrames,
    probeOutput,
    proofDir
  };
}

async function recordDemo() {
  log('=== Starting Demo Recording ===');
  
  let browser = null;
  let page = null;
  let capture = null;

  try {
    // Start server
    await startServer();
    await waitForHealth();

    // Launch browser
    log('Launching browser...');
    browser = await chromium.launch({
      headless: true,
      args: ['--no-sandbox', '--disable-setuid-sandbox']
    });
    activeBrowser = browser; // Track for interrupt cleanup
    
    const context = await browser.newContext({
      viewport: { width: 1280, height: 800 },
      deviceScaleFactor: 1
    });
    
    page = await context.newPage();

    // Navigate to dashboard
    log('Navigating to dashboard...');
    await page.goto(BASE_URL, { waitUntil: 'networkidle' });
    
    // Wait for page to be fully loaded
    await page.waitForSelector('#compare', { state: 'visible', timeout: 5000 });
    
    log('Dashboard loaded');

    // Set workload to sleep
    await page.selectOption('#workload', 'sleep');
    await page.waitForTimeout(200);

    // Fill in demo config
    await page.fill('#tasks', String(DEMO_CONFIG.tasks));
    await page.fill('#concurrency', String(DEMO_CONFIG.concurrency));
    await page.fill('#queue_size', String(DEMO_CONFIG.queue_size));
    await page.fill('#runs', String(DEMO_CONFIG.runs));
    await page.fill('#task_ms', String(DEMO_CONFIG.task_ms));

    log('Form configured');
    await page.waitForTimeout(500);

    // Start capturing frames
    capture = await captureFrames(page);

    // Click "Compare all" button
    log('Clicking "Compare all"...');
    await page.click('#compare');
    
    // Wait for benchmark to start
    await page.waitForTimeout(500);

    // Wait for completion by polling status
    log('Waiting for benchmark completion...');
    let completed = false;
    let attempts = 0;
    const maxAttempts = 60; // 60 seconds max

    while (!completed && attempts < maxAttempts) {
      const statusText = await page.textContent('#status');
      
      // Check if we've reached "Finished" state
      if (statusText && (statusText.includes('Finished') || statusText.includes('Idle'))) {
        // Wait a bit more for charts to fully render
        await page.waitForTimeout(1000);
        completed = true;
        break;
      }

      await page.waitForTimeout(1000);
      attempts++;
    }

    if (!completed) {
      throw new Error('Benchmark did not complete in expected time');
    }
    log('Benchmark completed');

    // Scroll to show complete comparison table with all 4 rows visible
    log('Scrolling to show complete comparison table...');
    await page.evaluate(() => {
      // Find the table and scroll to show all rows
      const table = document.querySelector('#comparison');
      if (table) {
        const tbody = table.querySelector('tbody');
        if (tbody) {
          const rows = tbody.querySelectorAll('tr');
          if (rows.length > 0) {
            // Scroll the last row into view at the bottom
            const lastRow = rows[rows.length - 1];
            lastRow.scrollIntoView({ behavior: 'smooth', block: 'end' });
          }
        }
      }
    });
    await page.waitForTimeout(2000);

    // Assert EXACTLY 4 rows and all in viewport (bug fix #3)
    const tableValidation = await page.evaluate(() => {
      const tbody = document.querySelector('#comparison tbody');
      if (!tbody) return { valid: false, reason: 'tbody not found', rowCount: 0 };
      
      const rows = tbody.querySelectorAll('tr');
      const rowCount = rows.length;
      
      // Must have EXACTLY 4 data rows
      if (rowCount !== 4) {
        return { valid: false, reason: `Expected exactly 4 rows, found ${rowCount}`, rowCount };
      }
      
      const viewportHeight = window.innerHeight;
      const results = [];
      
      for (let i = 0; i < rows.length; i++) {
        const rect = rows[i].getBoundingClientRect();
        const visible = rect.top >= 0 && rect.bottom <= viewportHeight;
        results.push({
          row: i + 1,
          top: rect.top,
          bottom: rect.bottom,
          visible
        });
      }
      
      const allVisible = results.every(r => r.visible);
      return { valid: allVisible, rowCount, rows: results, viewportHeight };
    });

    log(`Table validation: ${JSON.stringify(tableValidation)}`);
    
    // Zero or fewer than 4 rows cannot pass (bug fix #3)
    if (tableValidation.rowCount !== 4) {
      throw new Error(`Table validation failed: ${tableValidation.reason}`);
    }
    
    if (!tableValidation.valid) {
      log('Not all table rows visible, adjusting...');
      // Scroll the document to bottom
      await page.evaluate(() => {
        window.scrollTo(0, document.body.scrollHeight);
      });
      await page.waitForTimeout(1000);
      
      // Verify again after scroll - all 4 rows must be in viewport
      const recheck = await page.evaluate(() => {
        const tbody = document.querySelector('#comparison tbody');
        if (!tbody) return { valid: false, rowCount: 0 };
        const rows = tbody.querySelectorAll('tr');
        if (rows.length !== 4) return { valid: false, rowCount: rows.length };
        
        const viewportHeight = window.innerHeight;
        let allVisible = true;
        for (let i = 0; i < rows.length; i++) {
          const rect = rows[i].getBoundingClientRect();
          if (rect.top < 0 || rect.bottom > viewportHeight) {
            allVisible = false;
            break;
          }
        }
        return { valid: allVisible, rowCount: 4 };
      });
      
      if (recheck.rowCount !== 4 || !recheck.valid) {
        throw new Error('Table validation failed: Not all 4 rows in viewport after scroll adjustment');
      }
      log('All 4 table rows now visible in viewport');
    }

    // Hold final view for at least 2 seconds
    await page.waitForTimeout(2000);

    // Stop capturing
    const frameCount = await capture.stop();
    
    if (frameCount < 10) {
      throw new Error(`Too few frames captured: ${frameCount}`);
    }

    // Close browser
    await browser.close();
    browser = null;

    // Generate GIF
    const gifPath = await generateGIF();

    // Validate GIF
    const validation = await validateGIF(gifPath);

    log('=== Demo Recording Complete ===');
    log(`Frames: ${validation.frameCount}`);
    log(`Size: ${validation.size}`);

    return validation;

  } catch (error) {
    log(`ERROR: ${error.message}`);
    throw error;
  } finally {
    // Cleanup
    if (capture) {
      try {
        await capture.stop();
      } catch (err) {
        // Ignore
      }
    }
    
    if (browser) {
      try {
        await browser.close();
        activeBrowser = null;
      } catch (err) {
        log(`Browser close error: ${err.message}`);
      }
    }
    
    await stopServer();
  }
}

async function runTests() {
  log('Running Go tests...');
  try {
    const { stdout, stderr } = await exec('go', ['test', './...'], { cwd: WORKSPACE });
    log('Tests passed');
    if (stdout) log(stdout.trim());
  } catch (err) {
    log(`Tests failed: ${err.message}`);
    throw err;
  }
}

async function checkGitDiff() {
  log('Checking git diff...');
  try {
    const { stdout } = await exec('git', ['diff', '--stat'], { cwd: WORKSPACE });
    if (stdout.trim()) {
      log('Git diff detected:');
      log(stdout);
    } else {
      log('No git diff in tracked files');
    }
  } catch (err) {
    log(`Git diff check failed: ${err.message}`);
  }
}

async function cleanupAll() {
  // Idempotent cleanup for server, browser, and workdir
  log('Running cleanup...');
  
  // Close browser
  if (activeBrowser) {
    try {
      await activeBrowser.close();
      activeBrowser = null;
      log('Browser closed');
    } catch (err) {
      log(`Browser close error: ${err.message}`);
    }
  }
  
  // Stop server
  await stopServer();
  
  // Clean workdir
  if (workDir && existsSync(workDir)) {
    try {
      log(`Cleaning up work directory: ${workDir}`);
      rmSync(workDir, { recursive: true, force: true });
      log('Work directory cleaned');
      workDir = null;
    } catch (err) {
      log(`Failed to clean work directory: ${err.message}`);
    }
  }
}

async function runSelfTests() {
  log('=== Running Self-Tests ===\n');
  
  const originalProcessKill = process.kill;
  
  try {
    // Test 1: Graceful child gets SIGTERM only
    log('Test 1: Graceful child (exits on SIGTERM)...');
    const gracefulChild = spawn('sleep', ['1']);
    const gracefulPid = gracefulChild.pid;
    serverProcess = gracefulChild;
    serverPid = gracefulPid;
    
    let gracefulSigTermSent = false;
    let gracefulSigKillSent = false;
    
    const originalChildKill = gracefulChild.kill.bind(gracefulChild);
    gracefulChild.kill = function(signal) {
      if (signal === 'SIGTERM' || signal === undefined) gracefulSigTermSent = true;
      return originalChildKill(signal);
    };
    
    process.kill = (pid, signal) => {
      if (pid === gracefulPid && (signal === 'SIGKILL' || signal === 9)) {
        gracefulSigKillSent = true;
      }
      return originalProcessKill(pid, signal);
    };
    
    await stopServer();
    process.kill = originalProcessKill;
    
    if (gracefulSigTermSent && !gracefulSigKillSent) {
      log('✓ Test 1 passed: Graceful child received SIGTERM only');
    } else {
      throw new Error(`Test 1 failed: SIGTERM=${gracefulSigTermSent}, SIGKILL=${gracefulSigKillSent}`);
    }
    
    // Test 2: Resistant child gets SIGTERM then SIGKILL
    log('Test 2: Resistant child (ignores SIGTERM)...');
    
    // Spawn resistant child and wait for READY signal AFTER trap installed
    const resistantChild = spawn('bash', ['-c', 'trap "" TERM; echo READY; sleep 100']);
    const resistantPid = resistantChild.pid;
    
    // Wait for READY (trap installed)
    await new Promise((resolve, reject) => {
      const timeout = setTimeout(() => reject(new Error('Resistant child READY timeout')), 5000);
      resistantChild.stdout.on('data', (data) => {
        if (data.toString().includes('READY')) {
          clearTimeout(timeout);
          resolve();
        }
      });
    });
    
    serverProcess = resistantChild;
    serverPid = resistantPid;
    
    let resistantSigTermSent = false;
    let resistantSigKillSent = false;
    
    const originalResistantKill = resistantChild.kill.bind(resistantChild);
    resistantChild.kill = function(signal) {
      if (signal === 'SIGTERM' || signal === undefined) resistantSigTermSent = true;
      return originalResistantKill(signal);
    };
    
    process.kill = (pid, signal) => {
      if (pid === resistantPid && (signal === 'SIGKILL' || signal === 9)) {
        resistantSigKillSent = true;
      }
      return originalProcessKill(pid, signal);
    };
    
    // Temporarily reduce timeout for this test
    const originalStopServer = stopServer;
    stopServer = async function() {
      if (!serverProcess || !serverPid) {
        log('No server process to stop');
        return;
      }
      const pid = serverPid;
      try {
        serverProcess.kill('SIGTERM');
      } catch (err) {}
      const exitPromise = new Promise(resolve => {
        serverProcess.once('close', () => resolve('exited'));
      });
      const timeoutPromise = new Promise(resolve => setTimeout(() => resolve('timeout'), 1000));
      const result = await Promise.race([exitPromise, timeoutPromise]);
      if (result === 'timeout') {
        try {
          process.kill(pid, 0);
          process.kill(pid, 'SIGKILL');
          await exitPromise;
        } catch (err) {}
      }
      serverProcess = null;
      serverPid = null;
    };
    
    await stopServer();
    stopServer = originalStopServer;
    process.kill = originalProcessKill;
    
    if (resistantSigTermSent && resistantSigKillSent) {
      log('✓ Test 2 passed: Resistant child received SIGTERM then SIGKILL');
    } else {
      throw new Error(`Test 2 failed: SIGTERM=${resistantSigTermSent}, SIGKILL=${resistantSigKillSent}`);
    }
    
    // Test 3: No signals after exit
    log('Test 3: No signals sent after process already exited...');
    const shortChild = spawn('bash', ['-c', 'exit 0']);
    const shortPid = shortChild.pid;
    serverProcess = shortChild;
    serverPid = shortPid;
    
    // Wait for it to exit naturally
    await new Promise(resolve => shortChild.once('close', resolve));
    
    let signalsAfterExit = 0;
    process.kill = (pid, signal) => {
      if (pid === shortPid && (signal === 'SIGKILL' || signal === 9)) {
        signalsAfterExit++;
      }
      return originalProcessKill(pid, signal);
    };
    
    await stopServer();
    process.kill = originalProcessKill;
    
    if (signalsAfterExit === 0) {
      log('✓ Test 3 passed: No SIGKILL sent to already-exited process');
    } else {
      throw new Error(`Test 3 failed: ${signalsAfterExit} signals sent after exit`);
    }
    
    log('\n✓ All self-tests passed\n');
    
  } catch (error) {
    process.kill = originalProcessKill;
    log('\n✗ Self-tests FAILED\n');
    throw error;
  }
}

async function main() {
  // Check for --self-test flag
  if (process.argv.includes('--self-test')) {
    try {
      await runSelfTests();
      process.exit(0);
    } catch (error) {
      log(error.stack);
      process.exit(1);
    }
  }
  
  const startTime = Date.now();
  
  // Create unique workdir for all temp files
  workDir = mkdtempSync(join(tmpdir(), 'concurrency-lab-demo-'));
  log(`Work directory: ${workDir}`);
  const workDirPath = workDir; // Capture for assertion
  
  try {
    // First run
    log('\n========================================');
    log('RUN 1');
    log('========================================\n');
    const result1 = await recordDemo();
    await verifyNoLeakedProcesses();

    // Wait a bit between runs
    await new Promise(resolve => setTimeout(resolve, 2000));

    // Second run (validation)
    log('\n========================================');
    log('RUN 2 (Validation)');
    log('========================================\n');
    const result2 = await recordDemo();
    await verifyNoLeakedProcesses();

    // Run tests
    await runTests();

    // Check git diff
    await checkGitDiff();

    // Optional: copy proof frames to artifacts (outside workdir for audit)
    const artifactsDir = '/opt/cursor/artifacts';
    if (existsSync(artifactsDir)) {
      const proofDir = join(workDir, 'proof-frames');
      if (existsSync(proofDir)) {
        const proofFiles = readdirSync(proofDir);
        for (const file of proofFiles) {
          cpSync(join(proofDir, file), join(artifactsDir, file));
        }
        log(`Proof frames copied to ${artifactsDir}`);
      }
    }

    const duration = ((Date.now() - startTime) / 1000).toFixed(1);
    
    log('\n========================================');
    log('SUCCESS');
    log('========================================');
    log(`Total time: ${duration}s`);
    log(`Run 1: ${result1.frameCount} frames, ${result1.size}`);
    log(`Run 2: ${result2.frameCount} frames, ${result2.size}`);
    log('GIF: docs/demo.gif');
    log(`Proof frames: ${artifactsDir}/proof-*.png`);
    
  } catch (error) {
    log('\n========================================');
    log('FAILURE');
    log('========================================');
    log(error.stack);
    process.exitCode = 1;
  } finally {
    // Clean up on both success and failure (bug fix: finally must run)
    await cleanupAll();
    
    // Final assertion: workdir MUST be absent after cleanup (bug fix: assert after subprocess exit)
    log('\nAsserting workdir cleanup...');
    if (workDirPath && existsSync(workDirPath)) {
      log(`✗ ASSERTION FAILED: Work directory ${workDirPath} still exists after cleanup`);
      process.exitCode = 1;
    } else {
      log(`✓ Work directory absent after cleanup`);
    }
  }
}

// Handle interrupts - await idempotent cleanup then exit
process.on('SIGINT', async () => {
  log('Interrupted, cleaning up...');
  await cleanupAll();
  await verifyNoLeakedProcesses();
  process.exitCode = 130; // Standard SIGINT exit code
  process.exit();
});

process.on('SIGTERM', async () => {
  log('Terminated, cleaning up...');
  await cleanupAll();
  await verifyNoLeakedProcesses();
  process.exitCode = 143; // Standard SIGTERM exit code
  process.exit();
});

main();
