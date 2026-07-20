// Records an authenticated browsing session of the timesheet app.
// - Opens a visible Chromium with a persistent profile (login survives reruns)
// - Writes every request/response INCREMENTALLY to capture/requests-<ts>.jsonl
//   (no flush-on-exit dependency — data is on disk the moment it happens)
// - Also records a Playwright HAR as a secondary artifact
// - Snapshots cookies/localStorage to capture/storage-state.json every 15s
// - Shuts down automatically when all browser windows are closed
//
// Usage: node scripts/capture.mjs
// IMPORTANT: do NOT log out of the app before closing the browser — logging
// out may invalidate the session tokens we need for API replay.

import { chromium } from 'playwright';
import path from 'node:path';
import fs from 'node:fs';

const TARGET_URL = process.env.CAPTURE_URL ?? 'https://luby-timesheet.azurewebsites.net';
const outDir = path.resolve('capture');
fs.mkdirSync(outDir, { recursive: true });

const logPath = path.join(outDir, `requests-${Date.now()}.jsonl`);
const logLine = (obj) => fs.appendFileSync(logPath, JSON.stringify(obj) + '\n');

// Response bodies only for these resource types — keeps the log small
// and focused on API traffic (no JS/CSS/font binaries).
const BODY_TYPES = new Set(['xhr', 'fetch', 'document']);

const context = await chromium.launchPersistentContext(path.join(outDir, 'profile'), {
  headless: false,
  recordHar: {
    path: path.join(outDir, 'session.har'),
    content: 'embed',
  },
  viewport: { width: 1280, height: 900 },
});

context.on('request', (request) => {
  logLine({
    kind: 'request',
    ts: new Date().toISOString(),
    method: request.method(),
    url: request.url(),
    resourceType: request.resourceType(),
    headers: request.headers(),
    postData: request.postData() ?? null,
  });
});

context.on('response', (response) => {
  const request = response.request();
  const entry = {
    kind: 'response',
    ts: new Date().toISOString(),
    method: request.method(),
    url: response.url(),
    resourceType: request.resourceType(),
    status: response.status(),
    headers: response.headers(),
    body: null,
  };
  if (!BODY_TYPES.has(entry.resourceType)) {
    logLine(entry);
    return;
  }
  response
    .body()
    .then((buf) => {
      entry.body = buf.toString('utf8');
    })
    .catch(() => {})
    .finally(() => logLine(entry));
});

async function snapshotState() {
  try {
    await context.storageState({ path: path.join(outDir, 'storage-state.json') });
  } catch {
    // context may be mid-teardown; next tick will retry
  }
}

const snapshotTimer = setInterval(snapshotState, 15_000);

let closing = false;
async function shutdown(reason) {
  if (closing) return;
  closing = true;
  console.log(`Shutting down (${reason}).`);
  clearInterval(snapshotTimer);
  await snapshotState();
  try {
    await context.close(); // flushes the HAR
  } catch {}
  console.log(`Request log: ${logPath}`);
  process.exit(0);
}

process.on('SIGTERM', () => shutdown('SIGTERM'));
process.on('SIGINT', () => shutdown('SIGINT'));

// Quit when every window is closed (covers macOS window-close where the
// browser process itself stays alive).
const pageWatch = setInterval(() => {
  try {
    if (context.pages().length === 0) shutdown('all windows closed');
  } catch {
    shutdown('context gone');
  }
}, 1_000);

const page = context.pages()[0] ?? (await context.newPage());
await page.goto(TARGET_URL);

console.log('Browser is open. Log in, then click through the timesheet app.');
console.log('Do NOT log out when finished — just close the browser window.');
console.log(`Logging to ${logPath}`);

await new Promise((resolve) => context.on('close', resolve));
await shutdown('context closed');
