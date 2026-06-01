// Records the Triagearr demo: drives the dashboard in a headless browser while
// poking the dev fakes' control endpoints, so the captured video tells the
// reap story end to end. A demo-only overlay (injected, not part of the app)
// adds a step counter and spotlights the element each beat is about.
//
// Storyboard (6 beats, each waits on real daemon state — not a fixed sleep — so
// the recording stays in sync no matter how fast the machine is):
//
//   1/6 Dashboard — a healthy volume, nothing to reap.
//   2/6 Torrents  — the scored library.
//   3/6 Grab      — a fresh 4K release is added; it appears in the list.
//   4/6 Pressure  — back on the dashboard, the volume tips below threshold.
//   5/7 Actions   — the live run that fired on its own; the graveyard reaped.
//   6/7 Recover   — back on the dashboard, the freed space is reclaimed.
//   7/7 Library   — back to the list: the reaped graveyard is gone for good.
//
// Run via scripts/record-demo.sh (it boots the armed demo stack first). Output
// is a .webm the wrapper converts to an animated WebP.
//
// Env:
//   UI_URL    dashboard base URL        (default http://127.0.0.1:5173)
//   API_URL   daemon API base           (default http://127.0.0.1:9494)
//   QBIT_URL  fake qBit control base    (default http://127.0.0.1:18090)
//   DISK_URL  fake disk control base    (default http://127.0.0.1:18091/disk/dev)
//   OUT_DIR   where the .webm is written (default ./.dev/demo)

import { chromium } from "playwright";
import { mkdir } from "node:fs/promises";

const UI = process.env.UI_URL ?? "http://127.0.0.1:5173";
const API = process.env.API_URL ?? "http://127.0.0.1:9494";
const QBIT = process.env.QBIT_URL ?? "http://127.0.0.1:18090";
const DISK = process.env.DISK_URL ?? "http://127.0.0.1:18091/disk/dev";
const OUT_DIR = process.env.OUT_DIR ?? "./.dev/demo";

const TOTAL_STEPS = 7;

// The fresh grab: private + a live tracker + grabbed just now → the HnR-window
// veto (-10000) keeps it off the reap list even though it caused the pressure.
const GRAB_NAME = "Fresh.4K.Remux.S01.2160p.WEB-FAKE";
// Exactly 165 GiB so the UI (which formats in GiB) reads "165 GB", matching the
// step-3 caption. A 4K remux season pack.
const GRAB_SIZE = 165 * 1024 ** 3; // 177_167_503_360
const FREED_BYTES = 160_000_000_000; // what the graveyard reap actually frees

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// First load only. A full navigation briefly paints the light default before
// React applies the theme attribute, so wait that out before anything is held
// on camera. In-app moves use clickNav (client-side, no white flash).
async function gotoSettled(page, url) {
  await page.goto(url, { waitUntil: "networkidle" });
  await page.waitForSelector('html[data-theme="dark"]', { timeout: 10_000 });
}

// Navigate within the SPA by clicking a sidebar link — a client-side route
// swap, so there's no inter-document white flash to capture.
async function clickNav(page, label) {
  await page.locator(".sidebar-nav .nav-item", { hasText: label }).click();
}

async function api(path) {
  const res = await fetch(`${API}/api/v1${path}`);
  if (!res.ok) throw new Error(`GET ${path}: HTTP ${res.status}`);
  return res.json();
}

// Poll a predicate against the API until it holds (or we give up). Keeps the
// recording in lockstep with the daemon's poll cycle.
async function until(label, fn, { timeoutMs = 40_000, everyMs = 500 } = {}) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    try {
      if (await fn()) return;
    } catch {
      /* daemon may be mid-restart; keep trying */
    }
    if (Date.now() > deadline) throw new Error(`timeout waiting for: ${label}`);
    await sleep(everyMs);
  }
}

async function injectFreshGrab() {
  const now = Math.floor(Date.now() / 1000);
  const body = {
    hash: "f000000000000000000000000000000000000001",
    name: GRAB_NAME,
    category: "tv-sonarr",
    save_path: "/tmp/triagearr-dev/torrents/tv",
    size: GRAB_SIZE,
    added_on: now,
    completion_on: now,
    ratio: 0.05,
    uploaded: 8_250_000_000,
    num_complete: 8,
    num_incomplete: 3,
    state: "uploading",
    last_activity: now,
    private: true,
    tags: "new",
    files: [{ name: "Fresh.4K.S01E01.mkv", size: GRAB_SIZE, progress: 1.0 }],
    trackers: [{ url: "https://tracker.alive.example.org/announce", status: 2 }],
  };
  const res = await fetch(`${QBIT}/control/torrents`, {
    method: "POST",
    body: JSON.stringify(body),
  });
  if (res.status !== 201) throw new Error(`inject grab: HTTP ${res.status}`);
}

async function diskDelta(kind, bytes) {
  const res = await fetch(`${DISK}/${kind}`, {
    method: "POST",
    body: JSON.stringify({ bytes }),
  });
  if (!res.ok) throw new Error(`disk ${kind}: HTTP ${res.status}`);
}

// ── Demo overlay ───────────────────────────────────────────────────────────
// A step badge + a "spotlight" ring, injected into document.body so they ride
// over the app and survive client-side route swaps. The ring uses a huge
// spread box-shadow to dim everything outside the highlighted box.

const OVERLAY_SCRIPT = `
(() => {
  if (window.__demo) return;
  const style = document.createElement("style");
  style.textContent = \`
    #demo-ring {
      position: fixed; z-index: 2147483600; pointer-events: none;
      border: 2px solid #5aa2ff; border-radius: 10px;
      box-shadow: 0 0 0 9999px rgba(6,8,12,0), 0 0 22px 2px rgba(90,162,255,0);
      opacity: 0;
      transition: top .45s cubic-bezier(.4,0,.2,1), left .45s cubic-bezier(.4,0,.2,1),
                  width .45s cubic-bezier(.4,0,.2,1), height .45s cubic-bezier(.4,0,.2,1),
                  opacity .35s ease, box-shadow .35s ease;
    }
    /* Static glow (no infinite animation): the long holds stay byte-identical
       frame to frame, so the WebP encoder dedupes them and the file stays small. */
    #demo-ring.on {
      opacity: 1;
      box-shadow: 0 0 0 9999px rgba(6,8,12,.5), 0 0 26px 4px rgba(90,162,255,.6);
    }
    #demo-badge {
      position: fixed; z-index: 2147483601; left: 50%; bottom: 34px;
      transform: translateX(-50%) translateY(8px); opacity: 0;
      display: inline-flex; align-items: center; gap: 14px;
      padding: 13px 24px; border-radius: 999px;
      background: rgba(13,17,23,.92); border: 1px solid rgba(255,255,255,.14);
      box-shadow: 0 10px 32px rgba(0,0,0,.55);
      font: 600 17px/1.2 ui-sans-serif, system-ui, sans-serif; color: #e7edf5;
      transition: opacity .4s ease, transform .4s cubic-bezier(.4,0,.2,1);
    }
    #demo-badge.on { opacity: 1; transform: translateX(-50%) translateY(0); }
    #demo-badge .demo-count {
      font: 700 15px/1 'Geist Mono', ui-monospace, monospace; letter-spacing: .04em;
      color: #5aa2ff; background: rgba(90,162,255,.13);
      padding: 6px 11px; border-radius: 999px; flex: none;
    }
  \`;
  document.head.appendChild(style);
  const ring = document.createElement("div"); ring.id = "demo-ring";
  const badge = document.createElement("div"); badge.id = "demo-badge";
  badge.innerHTML = '<span class="demo-count"></span><span class="demo-label"></span>';
  document.body.append(ring, badge);
  window.__demo = {
    step(n, total, label) {
      badge.querySelector(".demo-count").textContent = n + " / " + total;
      badge.querySelector(".demo-label").textContent = label;
      badge.classList.add("on");
    },
    spot(x, y, w, h, pad) {
      // Clamp the ring to stay this many px inside the viewport so its border is
      // never clipped on an element that touches a screen edge (gauge card,
      // full-width table). Edge elements get a ring that hugs the edge instead
      // of bleeding off-screen.
      const m = 8;
      const left = Math.max(m, x - pad);
      const top = Math.max(m, y - pad);
      const right = Math.min(window.innerWidth - m, x + w + pad);
      const bottom = Math.min(window.innerHeight - m, y + h + pad);
      ring.style.left = left + "px";
      ring.style.top = top + "px";
      ring.style.width = Math.max(0, right - left) + "px";
      ring.style.height = Math.max(0, bottom - top) + "px";
      ring.classList.add("on");
    },
    clear() { ring.classList.remove("on"); },
  };
})();
`;

async function ensureChrome(page) {
  await page.evaluate(OVERLAY_SCRIPT);
}

async function step(page, n, label) {
  await ensureChrome(page);
  await page.evaluate(
    ({ n, total, label }) => window.__demo.step(n, total, label),
    { n, total: TOTAL_STEPS, label },
  );
}

// Spotlight a located element. Measures its box, then positions the ring with
// some padding. No-op (clears) if the element isn't on screen.
async function spotlight(page, locator, pad = 10) {
  await ensureChrome(page);
  const el = locator.first();
  await el.scrollIntoViewIfNeeded().catch(() => {});
  const box = await el.boundingBox();
  if (!box) {
    await page.evaluate(() => window.__demo.clear());
    return;
  }
  await page.evaluate(
    ({ x, y, w, h, pad }) => window.__demo.spot(x, y, w, h, pad),
    { x: box.x, y: box.y, w: box.width, h: box.height, pad },
  );
}

async function clearSpot(page) {
  await ensureChrome(page);
  await page.evaluate(() => window.__demo.clear());
}

// The disk-pressure gauge card, keyed off its title so it survives layout
// changes. card-title is card-specific, so it never matches the sidebar.
function gaugeCard(page) {
  return page
    .locator(".card")
    .filter({ has: page.locator(".card-title", { hasText: "Disk pressure" }) });
}

async function main() {
  await mkdir(OUT_DIR, { recursive: true });

  const browser = await chromium.launch({ channel: "chrome", headless: true });
  const context = await browser.newContext({
    viewport: { width: 1280, height: 800 },
    deviceScaleFactor: 2,
    colorScheme: "dark",
    recordVideo: { dir: OUT_DIR, size: { width: 1280, height: 800 } },
  });
  // Pin dark theme before any app script runs, so every full navigation loads
  // consistently dark instead of flashing the light default (the app reads
  // this key in src/lib/theme.ts).
  await context.addInitScript(() => {
    try {
      localStorage.setItem("theme", "dark");
    } catch {
      /* storage unavailable — fall back to system scheme */
    }
  });

  const page = await context.newPage();

  try {
    // ── Beat 1/7: a healthy volume. Land on the dashboard, settle on healthy
    // data, spotlight the gauge.
    await gotoSettled(page, UI);
    await until("baseline scored", async () => {
      const s = await api("/summary");
      return s.counts.torrents >= 5 && s.volume.free_percent >= 25;
    });
    await step(page, 1, "A healthy volume — nothing to reap");
    await spotlight(page, gaugeCard(page));
    await sleep(3800);

    // ── Beat 2/7: the scored library. Move to the Torrents list.
    await clearSpot(page);
    await clickNav(page, "Torrents");
    await page.locator("table.tbl tbody tr").first().waitFor({ state: "visible", timeout: 15_000 });
    await sleep(500);
    await step(page, 2, "Your library, scored for deletion");
    await spotlight(page, page.locator("table.tbl"), 6);
    await sleep(3000);
    // Inject the grab and let the poller ingest it while the list is still
    // spotlit, so beat 3 can sort + highlight it with no focus-less wait.
    await injectFreshGrab();
    await until("grab in store", async () => (await api("/summary")).counts.torrents >= 6);

    // ── Beat 3/7: a fresh grab lands. Sort by size so the 165 GB release jumps
    // to the top (and the query refetches), and spotlight the new row.
    await clearSpot(page);
    await page.locator("th.sortable", { hasText: "Size" }).click();
    const grabRow = page.locator("tr.clickable", { hasText: "Fresh.4K.Remux" });
    await grabRow.first().waitFor({ state: "visible", timeout: 15_000 });
    await sleep(400);
    await step(page, 3, "A fresh 4K grab lands — 165 GB");
    await spotlight(page, grabRow, 4);
    await sleep(2800);
    // Fill the volume and wait for the daemon to go red while the grab is still
    // spotlit, so beat 4 cuts straight to the red gauge — no focus-less wait.
    await diskDelta("fill", GRAB_SIZE);
    await until("gauge red", async () => {
      const v = (await api("/summary")).volume;
      return v.free_percent < v.target_free_percent;
    });

    // ── Beat 4/7: pressure rises. Back on the dashboard the gauge is red (the
    // fill + daemon re-sample already happened under beat 3's spotlight).
    await clearSpot(page);
    await clickNav(page, "Dashboard");
    await page.getByRole("button", { name: "Refresh" }).click();
    await sleep(700);
    await step(page, 4, "Disk pressure rises past the threshold");
    await spotlight(page, gaugeCard(page));
    await sleep(3500);

    // ── Beat 5/7: the reap. The disk-pressure trigger fired a LIVE run on its
    // own (during beat 4); open Actions and surface the run's named deletions.
    await until(
      "live pressure run",
      async () => {
        const { runs } = await api("/runs");
        return runs.some((r) => r.mode === "live" && r.status === "completed");
      },
      { timeoutMs: 45_000 },
    );
    await clearSpot(page);
    await clickNav(page, "Actions");
    const runRow = page.locator(".runrow").first();
    await runRow.waitFor({ state: "visible", timeout: 15_000 });
    await runRow.click(); // open the run → its per-torrent deletions, by name
    await sleep(700);
    await step(page, 5, "Triagearr reaps the dead-tracker graveyard");
    await spotlight(page, page.locator(".split-detail"), 6);
    await sleep(4500);
    // Hand the reaped space back and wait for the daemon to re-sample it while
    // the run detail is still spotlit, so beat 6 cuts to the recovered gauge.
    await diskDelta("free", FREED_BYTES);
    await until("gauge recovered", async () => {
      const v = (await api("/summary")).volume;
      return v.free_percent >= v.target_free_percent;
    });

    // ── Beat 6/7: recovery. Reload the dashboard so the gauge shows green.
    await clearSpot(page);
    await clickNav(page, "Dashboard");
    await page.getByRole("button", { name: "Refresh" }).click();
    await sleep(700);
    await step(page, 6, "Space reclaimed — back to healthy");
    await spotlight(page, gaugeCard(page));
    await sleep(3500);

    // ── Beat 7/7: the cleaned library. Back to the Torrents list — the reaped
    // graveyard is gone (ForgetTorrent evicted it from the store), leaving only
    // the spared releases. Wait for a graveyard row to detach so we hold on the
    // refreshed, pruned list rather than a stale cached one.
    await clearSpot(page);
    await clickNav(page, "Torrents");
    await page.locator("table.tbl tbody tr").first().waitFor({ state: "visible", timeout: 15_000 });
    await page
      .locator("tr.clickable", { hasText: "Defunct.Crime" })
      .waitFor({ state: "detached", timeout: 20_000 });
    await sleep(500);
    await step(page, 7, "The dead weight is gone from your library");
    await spotlight(page, page.locator("table.tbl"), 6);
    await sleep(4500);
    await clearSpot(page);
    await sleep(600);
  } finally {
    // Closing the context flushes the video to disk.
    await context.close();
    await browser.close();
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
