// Records the Triagearr demo: drives the dashboard in a headless browser while
// poking the dev fakes' control endpoints, so the captured video tells the
// reap story end to end. A demo-only overlay (injected, not part of the app)
// adds a step counter and spotlights the element each beat is about.
//
// Storyboard (7 beats, each waits on real daemon state — not a fixed sleep — so
// the recording stays in sync no matter how fast the machine is):
//
//   1/7 Dashboard — a healthy volume, nothing to reap. (Off camera: the
//                   operator protects one archive and prioritizes one grab.)
//   2/7 Torrents  — the scored library: guards and operator overrides flagged.
//   3/7 Grab      — a fresh 4K release is added; it appears in the list.
//   4/7 Pressure  — back on the dashboard, the volume tips below threshold.
//   5/7 Actions   — the live run that fired on its own reaps, in order: the
//                   operator-boosted grab, the dead-tracker graveyards, and the
//                   ratio-paid private seed. Protected/rare/HnR seeds are spared.
//   6/7 Recover   — back on the dashboard, the freed space is reclaimed.
//   7/7 Library   — back to the list: the reaped torrents are gone for good.
//
// Run via scripts/record-demo.sh (it boots the armed demo stack first). Output
// is a run of deviceScaleFactor:2 screenshots plus an ffconcat manifest of their
// real durations; the wrapper assembles them into an animated WebP. We capture
// screenshots rather than recordVideo because Playwright's video records at the
// viewport's CSS resolution and ignores deviceScaleFactor (its docs tie video
// size to the viewport only), so its text comes out soft — whereas screenshots
// honor deviceScaleFactor, giving the same layout at 2x density and crisp text.
//
// Env:
//   UI_URL    dashboard base URL        (default http://127.0.0.1:5173)
//   API_URL   daemon API base           (default http://127.0.0.1:9494)
//   QBIT_URL  fake qBit control base    (default http://127.0.0.1:18090)
//   DISK_URL  fake disk control base    (default http://127.0.0.1:18091/disk/dev)
//   OUT_DIR   where frames + manifest are written (default ./.dev/demo)

import { chromium } from "playwright";
import { mkdir, writeFile } from "node:fs/promises";

const UI = process.env.UI_URL ?? "http://127.0.0.1:5173";
const API = process.env.API_URL ?? "http://127.0.0.1:9494";
const QBIT = process.env.QBIT_URL ?? "http://127.0.0.1:18090";
const DISK = process.env.DISK_URL ?? "http://127.0.0.1:18091/disk/dev";
const OUT_DIR = process.env.OUT_DIR ?? "./.dev/demo";

const TOTAL_STEPS = 7;

// The fresh grab: private + a live tracker + grabbed just now → the HnR-window
// veto (-10000) keeps it off the reap list even though it caused the pressure.
const GRAB_NAME = "Fresh.4K.Remux.S01.2160p.WEB-FAKE";
// Exactly 220 GiB so the UI (which formats in GiB) reads "220 GB", matching the
// step-3 caption. A 4K remux season pack. Sized so the post-fill volume needs
// five deletions to recover — the operator-boosted grab, the three graveyards,
// and the ratio-paid private seed — rather than stopping sooner.
const GRAB_SIZE = 220 * 1024 ** 3; // 236_223_201_280
// What the reap frees: boosted grab (30) + 3 graveyards (45+60+55) + the
// ratio-paid Severance seed (40) = 230 GB. Matched to the fixtures so beat 6
// recovers to green.
const FREED_BYTES = 230_000_000_000;

// qBit-only fixtures whose per-torrent overrides are DB-only columns the
// scenario YAML can't seed — record.mjs sets them via the API at runtime
// (handlers_torrents: PUT .../protected and .../candidate_boost).
const BOOST_HASH = "d000000000000000000000000000000000000007"; // Prioritize deletion
const PROTECT_HASH = "d000000000000000000000000000000000000008"; // Protect

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

// Set a per-torrent override flag (protect / candidate_boost) the way the UI
// toggle does. The handler rescores the single hash synchronously, so the badge
// and ranking are live by the time we hold on the list.
async function apiPut(path, body) {
  const res = await fetch(`${API}/api/v1${path}`, {
    method: "PUT",
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`PUT ${path}: HTTP ${res.status}`);
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

// ── Frame capture ────────────────────────────────────────────────────────────
// Each frame is a deviceScaleFactor:2 viewport screenshot (2560x1600, lossless)
// timestamped at grab time, so the wrapper can rebuild real-time pacing.
const frames = [];
let frameIdx = 0;

async function snap(page) {
  const file = `f_${String(frameIdx++).padStart(5, "0")}.png`;
  await page.screenshot({ path: `${OUT_DIR}/${file}` });
  frames.push({ file, t: Date.now() });
}

// Hold the current scene "on camera" for ms, grabbing frames the whole time.
// The screenshot latency itself paces the loop (no fixed sleep), so a CSS
// transition playing during the hold is captured across several frames. A
// static hold yields byte-identical frames the WebP encoder dedupes.
async function holdFrames(page, ms) {
  const end = Date.now() + ms;
  do {
    await snap(page);
  } while (Date.now() < end);
}

// Emit the ffconcat manifest with each frame's real on-screen duration (the gap
// to the next grab), so the wrapper's constant-fps resample plays back at the
// captured wall-clock speed. ffconcat ignores the last entry's duration unless
// the file is repeated, hence the trailing line.
async function writeManifest() {
  let m = "ffconcat version 1.0\n";
  for (let i = 0; i < frames.length; i++) {
    const dur = i < frames.length - 1 ? (frames[i + 1].t - frames[i].t) / 1000 : 0.6;
    m += `file '${frames[i].file}'\nduration ${dur.toFixed(3)}\n`;
  }
  m += `file '${frames[frames.length - 1].file}'\n`;
  await writeFile(`${OUT_DIR}/frames.txt`, m);
}

async function main() {
  await mkdir(OUT_DIR, { recursive: true });

  const browser = await chromium.launch({ channel: "chrome", headless: true });
  const context = await browser.newContext({
    viewport: { width: 1280, height: 800 },
    deviceScaleFactor: 2,
    colorScheme: "dark",
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
    // ── Beat 1/7: a healthy volume. Wait (via the API, no page yet) for the
    // daemon to ingest + score the seeded library, then apply the operator's two
    // manual overrides the way the UI toggles do: protect the irreplaceable
    // archive (excluded despite a top-tier score) and prioritize the bloated
    // grab for deletion (+2000 → reaped first). Setting them BEFORE the first
    // page load means beat 1's candidate list already reflects them — boosted on
    // top, protected dropped — instead of a stale pre-override snapshot. Both are
    // also honored by the pressure run that fires later.
    await until("baseline scored", async () => {
      const s = await api("/summary");
      return s.counts.torrents >= 8 && s.volume.free_percent >= 25;
    });
    await apiPut(`/torrents/${PROTECT_HASH}/protected`, { protected: true });
    await apiPut(`/torrents/${BOOST_HASH}/candidate_boost`, { candidate_boost: true });
    // Land on the dashboard now that the data is settled and the overrides hold.
    await gotoSettled(page, UI);
    await step(page, 1, "A healthy volume — nothing to reap");
    await spotlight(page, gaugeCard(page));
    await holdFrames(page, 3800);

    // ── Beat 2/7: the scored library. Move to the Torrents list.
    await clearSpot(page);
    await clickNav(page, "Torrents");
    await page.locator("table.tbl tbody tr").first().waitFor({ state: "visible", timeout: 15_000 });
    await holdFrames(page, 500);
    await step(page, 2, "Your library, scored — guards and overrides flagged");
    await spotlight(page, page.locator("table.tbl"), 6);
    await holdFrames(page, 3000);
    // Inject the grab and let the poller ingest it while the list is still
    // spotlit, so beat 3 can sort + highlight it with no focus-less wait.
    await injectFreshGrab();
    await until("grab in store", async () => (await api("/summary")).counts.torrents >= 9);

    // ── Beat 3/7: a fresh grab lands. Sort by size so the 220 GB release jumps
    // to the top (and the query refetches), and spotlight the new row.
    await clearSpot(page);
    await page.locator("th.sortable", { hasText: "Size" }).click();
    const grabRow = page.locator("tr.clickable", { hasText: "Fresh.4K.Remux" });
    await grabRow.first().waitFor({ state: "visible", timeout: 15_000 });
    await holdFrames(page, 400);
    await step(page, 3, "A fresh 4K grab lands — 220 GB");
    await spotlight(page, grabRow, 4);
    await holdFrames(page, 2800);
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
    await holdFrames(page, 700);
    await step(page, 4, "Disk pressure rises past the threshold");
    await spotlight(page, gaugeCard(page));
    await holdFrames(page, 3500);

    // ── Beat 5/7: the reap. The disk-pressure trigger fired a LIVE run on its
    // own (during beat 4); open Actions and surface the run's named deletions.
    // In score order the run reaps: the operator-BOOSTED grab first (+2000), the
    // three dead-tracker graveyards, then Severance.S02 — a private seed on a
    // LIVE tracker whose ratio + seed-time obligation is paid and whose HnR
    // window has long cleared. The protected archive and rare seed are left be.
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
    await holdFrames(page, 700);
    await step(page, 5, "Your pick first, then dead trackers and a paid-off seed");
    await spotlight(page, page.locator(".split-detail"), 6);
    await holdFrames(page, 4500);
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
    await holdFrames(page, 700);
    await step(page, 6, "Space reclaimed — back to healthy");
    await spotlight(page, gaugeCard(page));
    await holdFrames(page, 3500);

    // ── Beat 7/7: the cleaned library. Back to the Torrents list — the reaped
    // rows are gone (ForgetTorrent evicted them from the store), leaving only
    // the spared releases. Wait for a graveyard row AND the ratio-paid Severance
    // seed to detach, so we hold on the refreshed, pruned list (both reap
    // regimes proven gone) rather than a stale cached one.
    await clearSpot(page);
    await clickNav(page, "Torrents");
    await page.locator("table.tbl tbody tr").first().waitFor({ state: "visible", timeout: 15_000 });
    // Generous timeout: wait for the list's post-reap refetch + re-render to
    // catch up to the daemon's own eviction (we're waiting on the UI, not the
    // reap), so we hold on the pruned list rather than a stale cached one.
    await page
      .locator("tr.clickable", { hasText: "Defunct.Crime" })
      .waitFor({ state: "detached", timeout: 40_000 });
    await page
      .locator("tr.clickable", { hasText: "Severance.S02" })
      .waitFor({ state: "detached", timeout: 40_000 });
    await holdFrames(page, 500);
    await step(page, 7, "The dead weight is gone from your library");
    await spotlight(page, page.locator("table.tbl"), 6);
    await holdFrames(page, 4500);
    await clearSpot(page);
    await holdFrames(page, 600);

    // Emit the manifest now that every frame is on disk; the wrapper builds the
    // animated WebP from it.
    await writeManifest();
  } finally {
    await context.close();
    await browser.close();
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
