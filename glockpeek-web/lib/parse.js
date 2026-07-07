// Parsing for the three glocker log files. Mirrors internal/reports/reports.go
// so the web app sees exactly the same data glockpeek does.
import { readFile } from "node:fs/promises";

// Default log paths match internal/reports/reports.go.
export const DEFAULT_PATHS = {
  reports: process.env.GLOCKER_REPORTS_LOG || "/var/log/glocker-reports.log",
  unblocks: process.env.GLOCKER_UNBLOCKS_LOG || "/var/log/glocker-unblocks.log",
  lifecycle: process.env.GLOCKER_LIFECYCLE_LOG || "/var/log/glocker-lifecycle.log",
};

// Periods shorter than this are treated as upgrades, not real exposure.
// Matches minUnmanagedDuration in cmd/glockpeek/main.go.
const MIN_UNMANAGED_MS = 2 * 60 * 1000;

async function readLines(path) {
  try {
    const text = await readFile(path, "utf8");
    return text.split("\n").map((l) => l.trim()).filter(Boolean);
  } catch (err) {
    if (err.code === "ENOENT" || err.code === "EACCES") return null;
    throw err;
  }
}

// [2025-11-17 15:35:46] | url-keyword:porn | https://... | domain
const REPORT_RE =
  /^\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\] \| (url-keyword|content-keyword):([^ |]+) \| ([^ |]+)(?: \| (.+))?$/;

// Reports use a naive local timestamp; interpret it in the server's local zone.
function parseLocalTimestamp(s) {
  const t = Date.parse(s.replace(" ", "T"));
  return Number.isNaN(t) ? null : t;
}

export async function parseReports(path = DEFAULT_PATHS.reports) {
  const lines = await readLines(path);
  if (lines === null) return { available: false, entries: [] };
  const entries = [];
  for (const line of lines) {
    const m = REPORT_RE.exec(line);
    if (!m) continue;
    const ts = parseLocalTimestamp(m[1]);
    if (ts === null) continue;
    entries.push({
      ts,
      type: m[2], // url-keyword | content-keyword
      keyword: m[3],
      url: m[4],
      domain: m[5] || hostFromUrl(m[4]),
    });
  }
  entries.sort((a, b) => a.ts - b.ts);
  return { available: true, entries };
}

// Older URL-keyword rows have no explicit domain column; derive it from the URL.
function hostFromUrl(url) {
  try {
    return new URL(url).hostname;
  } catch {
    return "";
  }
}

export async function parseUnblocks(path = DEFAULT_PATHS.unblocks) {
  const lines = await readLines(path);
  if (lines === null) return { available: false, entries: [] };
  const entries = [];
  for (const line of lines) {
    let o;
    try {
      o = JSON.parse(line);
    } catch {
      continue;
    }
    const ts = Date.parse(o.unblock_time);
    if (Number.isNaN(ts)) continue;
    const restore = Date.parse(o.restore_time);
    entries.push({
      ts,
      restoreTs: Number.isNaN(restore) ? null : restore,
      reason: o.reason || "",
      domain: o.domain || "",
    });
  }
  entries.sort((a, b) => a.ts - b.ts);
  return { available: true, entries };
}

export async function parseLifecycle(path = DEFAULT_PATHS.lifecycle) {
  const lines = await readLines(path);
  if (lines === null) return { available: false, entries: [] };
  const entries = [];
  for (const line of lines) {
    let o;
    try {
      o = JSON.parse(line);
    } catch {
      continue;
    }
    const ts = Date.parse(o.timestamp);
    if (Number.isNaN(ts)) continue;
    entries.push({
      ts,
      type: o.type, // install | uninstall
      reason: o.reason || "",
      note: o.note || "",
    });
  }
  entries.sort((a, b) => a.ts - b.ts);
  return { available: true, entries };
}

// Pair each uninstall with the next install to find spans when glocker was not
// enforcing. An open span (uninstall with no following install) ends at `now`.
// Mirrors getUnmanagedPeriods() in cmd/glockpeek/main.go.
export function unmanagedPeriods(lifecycle, now = Date.now()) {
  const periods = [];
  let openUninstall = null;
  for (const e of lifecycle) {
    if (e.type === "uninstall") {
      openUninstall = e;
    } else if (e.type === "install" && openUninstall) {
      if (e.ts - openUninstall.ts >= MIN_UNMANAGED_MS) {
        periods.push({
          start: openUninstall.ts,
          end: e.ts,
          open: false,
          reason: openUninstall.reason,
          note: openUninstall.note,
        });
      }
      openUninstall = null;
    }
  }
  if (openUninstall) {
    periods.push({
      start: openUninstall.ts,
      end: now,
      open: true,
      reason: openUninstall.reason,
      note: openUninstall.note,
    });
  }
  return periods;
}

// Read and parse everything in one shot.
export async function loadAll(paths = DEFAULT_PATHS, now = Date.now()) {
  const [reports, unblocks, lifecycle] = await Promise.all([
    parseReports(paths.reports),
    parseUnblocks(paths.unblocks),
    parseLifecycle(paths.lifecycle),
  ]);
  return {
    now,
    sources: {
      reports: reports.available,
      unblocks: unblocks.available,
      lifecycle: lifecycle.available,
    },
    violations: reports.entries,
    unblocks: unblocks.entries,
    lifecycle: lifecycle.entries,
    unmanaged: unmanagedPeriods(lifecycle.entries, now),
  };
}
