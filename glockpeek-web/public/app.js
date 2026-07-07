// glockpeek-web frontend. Pulls the full parsed history from /api/data, then
// does all aggregation/filtering in the browser (the data set is tiny). The
// vulnerability heatmap is the centrepiece: it surfaces the hour/weekday slots
// where you actually slip — and hatches the slots where glocker was off, so a
// blank cell is never mistaken for a safe one.

const WEEKDAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const DAY = 86400000;
const HOUR = 3600000;

const RANGES = [
  { id: "1d", label: "1d", days: 1 },
  { id: "1w", label: "1w", days: 7 },
  { id: "30d", label: "30d", days: 30 },
  { id: "90d", label: "90d", days: 90 },
  { id: "1y", label: "1y", days: 365 },
  { id: "all", label: "all", days: null },
];

// offset counts fixed windows back from the most recent: 0 = latest window
// ending now, -1 = the window immediately before it, etc.
const state = { data: null, range: "30d", offset: 0, view: "overview", charts: {}, rules: [], tagColors: {}, usageWindow: null };

const VIEW_TITLES = {
  overview: "Overview",
  history: "History",
  patterns: "Patterns",
  usage: "Usage",
  sources: "Sources",
  bypasses: "Bypasses",
};

init();

async function init() {
  buildRangeButtons();
  try {
    const res = await fetch("/api/data");
    if (!res.ok) throw new Error(`server returned ${res.status}`);
    state.data = await res.json();
  } catch (err) {
    showError(`Could not load logs: ${err.message}`);
    return;
  }
  // Usage categorization config: rules + tag colours (non-fatal if missing).
  try {
    const rres = await fetch("/api/rules");
    if (rres.ok) {
      const cfg = await rres.json();
      state.rules = cfg.rules || [];
      state.tagColors = cfg.colors || {};
    }
  } catch { /* keep empty rules/colours */ }

  document.getElementById("loading").hidden = true;
  document.getElementById("dash").hidden = false;

  // Delegated hover: the calendar is re-rendered on every range change, but the
  // container element is stable so one listener suffices.
  document.getElementById("calendar").addEventListener("mouseover", (e) => {
    const cell = e.target.closest(".cal-day[data-ts]");
    if (cell) showDay(Number(cell.dataset.ts));
  });

  // Left-nav view switching drives the URL hash; the hashchange handler below
  // (and the initial hash) is what actually applies the view — so deep links
  // and browser back/forward all work.
  document.getElementById("nav").addEventListener("click", (e) => {
    const btn = e.target.closest("button[data-view]");
    if (btn) routeTo(btn.dataset.view);
  });
  window.addEventListener("hashchange", () => setView(viewFromHash()));

  setupRules();
  renderFooter();
  render();
  setView(viewFromHash()); // honour a deep-linked view like #usage on load
}

// The view named by the URL hash (e.g. "#usage"), or "overview" if absent/unknown.
function viewFromHash() {
  const v = decodeURIComponent(location.hash.replace(/^#\/?/, ""));
  return VIEW_TITLES[v] ? v : "overview";
}

// Navigate to a view by updating the hash; the hashchange handler applies it.
// If the hash already matches (e.g. re-clicking the current tab), apply directly
// since no hashchange event would fire.
function routeTo(view) {
  if (viewFromHash() === view) setView(view);
  else location.hash = view;
}

function setView(view) {
  state.view = view;
  document.querySelectorAll(".view").forEach((v) => v.classList.toggle("active", v.dataset.view === view));
  document.querySelectorAll("#nav button").forEach((b) => b.classList.toggle("active", b.dataset.view === view));
  document.getElementById("view-title").textContent = VIEW_TITLES[view] || view;
  // Charts built while their view was display:none have zero size; fix on reveal.
  requestAnimationFrame(() => {
    for (const c of Object.values(state.charts)) {
      try { c.resize(); } catch { /* chart may be mid-rebuild */ }
    }
  });
}

function showError(msg) {
  document.getElementById("loading").hidden = true;
  const el = document.getElementById("error");
  el.hidden = false;
  el.innerHTML = `<div>⚠ ${msg}</div><div style="color:var(--ink-faint)">Is glockpeek-web running on a host with the glocker logs?</div>`;
}

// ── Local-time helpers ──────────────────────────────────
// JS getDay() is 0=Sun..6=Sat; we want Mon=0..Sun=6 to match glockpeek.
const weekdayIndex = (d) => (d.getDay() + 6) % 7;
const dayKey = (ts) => {
  const d = new Date(ts);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
};
const startOfDay = (ts) => {
  const d = new Date(ts);
  d.setHours(0, 0, 0, 0);
  return d.getTime();
};

// ── Range / window control ──────────────────────────────
function buildRangeButtons() {
  const wrap = document.getElementById("range-buttons");
  for (const r of RANGES) {
    const b = document.createElement("button");
    b.textContent = r.label;
    b.dataset.id = r.id;
    if (r.id === state.range) b.classList.add("active");
    b.addEventListener("click", () => {
      state.range = r.id;
      state.offset = 0; // reset to the most recent window when resizing
      wrap.querySelectorAll("button").forEach((x) => x.classList.toggle("active", x.dataset.id === r.id));
      render();
    });
    wrap.appendChild(b);
  }

  // Step one window into the past / future.
  document.getElementById("range-prev").addEventListener("click", () => shiftWindow(-1));
  document.getElementById("range-next").addEventListener("click", () => shiftWindow(1));
}

function shiftWindow(dir) {
  const r = RANGES.find((x) => x.id === state.range);
  if (!r.days) return; // "all" is not shiftable
  const next = Math.min(0, state.offset + dir);
  if (next === state.offset) return;
  // Don't page past the earliest data we have.
  if (dir < 0 && windowBounds().start <= startOfDay(earliestTs())) return;
  state.offset = next;
  render();
}

function earliestTs() {
  let min = Infinity;
  for (const a of [state.data.violations, state.data.unblocks, state.data.lifecycle, state.data.usage || []]) {
    if (a.length) min = Math.min(min, a[0].ts); // arrays are sorted ascending
  }
  return Number.isFinite(min) ? min : state.data.now;
}

function windowBounds() {
  const now = state.data.now;
  const r = RANGES.find((x) => x.id === state.range);
  if (!r.days) {
    // "all" — from the earliest event we have.
    const start = startOfDay(earliestTs());
    return { start, end: now };
  }
  // Anchor to day boundaries and page back by whole windows.
  const endDayStart = startOfDay(now) + state.offset * r.days * DAY;
  const start = endDayStart - (r.days - 1) * DAY;
  const end = state.offset === 0 ? now : endDayStart + DAY - 1;
  return { start, end };
}

// Update the pager label and enabled/disabled state of the step buttons.
function renderRangeControl(b) {
  const r = RANGES.find((x) => x.id === state.range);
  const nav = document.getElementById("range-nav");
  const label = document.getElementById("range-label");
  const prev = document.getElementById("range-prev");
  const next = document.getElementById("range-next");

  if (!r.days) {
    nav.hidden = true;
    return;
  }
  nav.hidden = false;
  const fmt = (ts) => new Date(ts).toLocaleDateString(undefined, { day: "numeric", month: "short" });
  const fmtY = (ts) => new Date(ts).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
  label.textContent = `${fmt(b.start)} – ${fmtY(b.end)}`;

  next.disabled = state.offset === 0;
  prev.disabled = b.start <= startOfDay(earliestTs());
}

const within = (ts, b) => ts >= b.start && ts <= b.end;

// ── Master render ───────────────────────────────────────
function render() {
  const b = windowBounds();
  renderRangeControl(b);
  const violations = state.data.violations.filter((v) => within(v.ts, b));
  const unblocks = state.data.unblocks.filter((u) => within(u.ts, b));
  const unmanaged = state.data.unmanaged
    .map((p) => ({ ...p, start: Math.max(p.start, b.start), end: Math.min(p.end, b.end) }))
    .filter((p) => p.end > p.start);

  // Per-day index shared by the calendar, timeline, and day inspector.
  const dayIndex = new Map();
  for (const v of violations) {
    const k = dayKey(v.ts);
    if (!dayIndex.has(k)) dayIndex.set(k, []);
    dayIndex.get(k).push(v);
  }
  state.dayIndex = dayIndex;

  const heat = buildHeatmap(violations, b);
  renderScore(violations, unmanaged, b);
  renderReadout(violations, unblocks, unmanaged, heat, b);
  renderHeatmap(heat);
  renderWeekdayChart(violations);
  renderHourChart(violations);
  renderCalendar(violations, b);
  renderTimeline(violations, b);
  resetDayDetail();
  renderRanklist("top-keywords", tally(violations, "keyword"), "var(--danger)");
  renderRanklist("top-domains", tally(violations, "domain"), "var(--signal)");
  renderRanklist("unblock-reasons", tally(unblocks, "reason"), "var(--safe)");
  renderUnmanaged(unmanaged);
  renderUsage(b);
}

// ── Aggregation ─────────────────────────────────────────
function tally(rows, field) {
  const m = new Map();
  for (const r of rows) {
    const k = r[field] || "—";
    m.set(k, (m.get(k) || 0) + 1);
  }
  return [...m.entries()].map(([name, count]) => ({ name, count })).sort((a, b) => b.count - a.count);
}

// Build the weekday × hour grid of violation counts, and the parallel grid of
// "unmanaged fraction" so we can hatch slots that were mostly glocker-off.
function buildHeatmap(violations, b) {
  const counts = Array.from({ length: 7 }, () => new Array(24).fill(0));
  for (const v of violations) {
    const d = new Date(v.ts);
    counts[weekdayIndex(d)][d.getHours()]++;
  }

  // Walk the window hour-by-hour to measure how often each slot was unmanaged.
  const slotTotal = Array.from({ length: 7 }, () => new Array(24).fill(0));
  const slotUnmanaged = Array.from({ length: 7 }, () => new Array(24).fill(0));
  const periods = state.data.unmanaged;
  const end = Math.min(b.end, state.data.now);
  for (let t = startOfDay(b.start); t < end; t += HOUR) {
    const d = new Date(t);
    const wd = weekdayIndex(d);
    const hr = d.getHours();
    slotTotal[wd][hr]++;
    if (periods.some((p) => t < (p.open ? state.data.now : p.end) && t + HOUR > p.start)) {
      slotUnmanaged[wd][hr]++;
    }
  }

  let max = 0, peak = null;
  for (let wd = 0; wd < 7; wd++) {
    for (let hr = 0; hr < 24; hr++) {
      if (counts[wd][hr] > max) {
        max = counts[wd][hr];
        peak = { wd, hr, count: counts[wd][hr] };
      }
    }
  }
  const unmanagedFrac = slotTotal.map((row, wd) =>
    row.map((tot, hr) => (tot ? slotUnmanaged[wd][hr] / tot : 0))
  );
  return { counts, max, peak, unmanagedFrac };
}

// ── Color ramp for the heatmap (ember → danger) ─────────
const RAMP = [
  [12, 17, 25],    // base / none
  [90, 28, 36],
  [150, 30, 40],
  [200, 40, 45],
  [255, 90, 60],   // hottest
];
function heatColor(ratio) {
  if (ratio <= 0) return `rgb(${RAMP[0].join(",")})`;
  const eased = Math.pow(ratio, 0.6);
  const seg = Math.min(RAMP.length - 2, Math.floor(eased * (RAMP.length - 1)));
  const f = eased * (RAMP.length - 1) - seg;
  const a = RAMP[seg + 1], c = RAMP[seg]; // start ramp at index 1 for any hit
  const mix = (i) => Math.round(c[i] + (a[i] - c[i]) * f);
  // ensure any non-zero count is visibly warm, not base
  const lo = RAMP[1];
  return `rgb(${Math.max(mix(0), lo[0] * 0.6) | 0},${mix(1)},${mix(2)})`;
}

// ── Composite health score ──────────────────────────────
// score = 100 − penalties, where each penalty is a RATE (not a raw count) so
// the number is comparable as you switch windows. Weights are the max points
// each failure mode can cost; tune them here.
const HEALTH_WEIGHTS = {
  exposure: 80, // points lost if glocker were uninstalled the entire window
  violation: 30, // points lost per (violation/day)…
  violationCap: 45, // …capped here so one wild day can't zero the score alone
  deliberate: 50, // points lost if every day were a deliberate day (>2 hits)
};

function computeHealth(violations, unmanaged, b) {
  const windowMs = Math.max(1, Math.min(b.end, state.data.now) - b.start);
  const days = Math.max(1, Math.round(windowMs / DAY));

  const unmanagedMs = unmanaged.reduce((s, p) => s + (p.end - p.start), 0);
  const exposureFrac = Math.min(1, unmanagedMs / windowMs);

  const byDay = new Map();
  for (const v of violations) byDay.set(dayKey(v.ts), (byDay.get(dayKey(v.ts)) || 0) + 1);
  const deliberateDays = [...byDay.values()].filter((c) => c > 2).length;

  const vRate = violations.length / days;
  const dFrac = deliberateDays / days;

  const penalties = {
    exposure: exposureFrac * HEALTH_WEIGHTS.exposure,
    violations: Math.min(vRate * HEALTH_WEIGHTS.violation, HEALTH_WEIGHTS.violationCap),
    deliberate: dFrac * HEALTH_WEIGHTS.deliberate,
  };
  const score = Math.max(0, Math.round(100 - penalties.exposure - penalties.violations - penalties.deliberate));

  return { score, days, exposureFrac, vRate, deliberateDays, penalties, band: bandFor(score) };
}

function bandFor(s) {
  if (s >= 90) return { label: "Excellent", color: "#34d399" };
  if (s >= 75) return { label: "Good", color: "var(--safe)" };
  if (s >= 60) return { label: "Fair", color: "var(--signal)" };
  if (s >= 40) return { label: "At risk", color: "#ff8a3c" };
  return { label: "Critical", color: "var(--danger)" };
}

function verdict(h) {
  if (h.score >= 90) return "Fully covered with almost no slips. Keep it up.";
  const drivers = [
    ["exposure", "unmanaged time — glocker was uninstalled"],
    ["violations", "frequent violations while guarded"],
    ["deliberate", "days with repeated, deliberate attempts"],
  ].sort((a, b) => h.penalties[b[0]] - h.penalties[a[0]]);
  return `Health is ${h.band.label.toLowerCase()}. Biggest drag: ${drivers[0][1]}.`;
}

function renderScore(violations, unmanaged, b) {
  const h = computeHealth(violations, unmanaged, b);
  const R = 52, C = 2 * Math.PI * R;
  const offset = C * (1 - h.score / 100);
  const color = h.band.color;

  const pens = [
    {
      label: "Unmanaged", val: h.penalties.exposure, color: "var(--exposed)",
      help: `Glocker was uninstalled ${(h.exposureFrac * 100).toFixed(1)}% of this window. Exposure is weighted heaviest — up to ${HEALTH_WEIGHTS.exposure} points.`,
    },
    {
      label: "Violations", val: h.penalties.violations, color: "var(--danger)",
      help: `${h.vRate.toFixed(2)} violations/day. ${HEALTH_WEIGHTS.violation} points per violation/day, capped at ${HEALTH_WEIGHTS.violationCap}.`,
    },
    {
      label: "Deliberate days", val: h.penalties.deliberate, color: "var(--danger-deep)",
      help: `${h.deliberateDays} of ${h.days} days had >2 hits. Up to ${HEALTH_WEIGHTS.deliberate} points if every day were deliberate.`,
    },
  ];

  const breakdown = pens
    .map(
      (p) => `<div class="pen" title="${esc(p.help)}">
        <span class="plabel">${p.label}</span>
        <span class="pval">&minus;${p.val.toFixed(0)}</span>
        <span class="ptrack"><span class="pfill" style="width:${Math.min(100, p.val).toFixed(1)}%;background:${p.color}"></span></span>
      </div>`
    )
    .join("");

  document.getElementById("score").innerHTML = `
    <div class="score-gauge" title="${h.score} out of 100">
      <svg viewBox="0 0 120 120" aria-hidden="true">
        <circle class="gauge-track" cx="60" cy="60" r="${R}"></circle>
        <circle class="gauge-arc" cx="60" cy="60" r="${R}"
          style="stroke:${color};stroke-dasharray:${C.toFixed(1)};stroke-dashoffset:${offset.toFixed(1)}"></circle>
      </svg>
      <div class="gauge-center">
        <span class="gauge-num" style="color:${color}">${h.score}</span>
        <span class="gauge-max">/ 100</span>
      </div>
    </div>
    <div class="score-detail">
      <div class="score-band" style="color:${color}">${h.band.label}</div>
      <p class="score-verdict">${esc(verdict(h))}</p>
      <div class="score-breakdown">${breakdown}</div>
    </div>`;
}

// ── Renderers ───────────────────────────────────────────
function renderReadout(violations, unblocks, unmanaged, heat, b) {
  const days = Math.max(1, Math.round((b.end - b.start) / DAY));
  const byDay = new Map();
  for (const v of violations) byDay.set(dayKey(v.ts), (byDay.get(dayKey(v.ts)) || 0) + 1);
  const deliberateDays = [...byDay.values()].filter((c) => c > 2).length; // glockpeek's "egregious" threshold
  const cleanDays = days - byDay.size;

  const unmanagedMs = unmanaged.reduce((s, p) => s + (p.end - p.start), 0);
  const peakLabel = heat.peak
    ? `${WEEKDAYS[heat.peak.wd]} ${String(heat.peak.hr).padStart(2, "0")}:00`
    : "—";

  const stats = [
    {
      k: "Violations", v: violations.length, sub: `${(violations.length / days).toFixed(1)}/day`,
      help: "Total blocked-keyword hits (URL + page content) reported by the browser extension in the selected window.",
    },
    {
      k: "Deliberate days", v: deliberateDays, sub: `>2 hits · of ${days}d`, cls: deliberateDays ? "alarm" : "",
      help: "Days with more than 2 violations. glockpeek treats >2 hits in a day as a deliberate attempt rather than an accidental one.",
    },
    {
      k: "Clean days", v: Math.max(0, cleanDays), sub: `${Math.round((Math.max(0, cleanDays) / days) * 100)}% of window`,
      help: "Days in the window with zero recorded violations.",
    },
    {
      k: "Peak exposure", v: peakLabel, sub: heat.peak ? `${heat.peak.count} hits` : "no data", cls: "peak", big: false,
      help: "The weekday + hour slot with the most violations — the time you are statistically most vulnerable to failure.",
    },
    {
      k: "Unmanaged", v: fmtDur(unmanagedMs), sub: `${unmanaged.length} span(s)`, cls: unmanagedMs ? "alarm" : "",
      help: "Total time glocker was uninstalled (not enforcing) in the window. This counts as exposure — no blocking was active.",
    },
    {
      k: "Unblocks", v: unblocks.length, sub: "deliberate grants",
      help: "Number of temporary unblock grants you requested (e.g. glocker -unblock) in the window.",
    },
  ];

  document.getElementById("readout").innerHTML = stats
    .map(
      (s) => `<div class="stat ${s.cls || ""}" title="${esc(s.help)}">
        <div class="k">${s.k}</div>
        <div class="v" ${s.big === false ? 'style="font-size:20px"' : ""}>${s.v}</div>
        <div class="sub">${s.sub}</div>
      </div>`
    )
    .join("");
}

function renderHeatmap(heat) {
  const el = document.getElementById("heatmap");
  const cells = [`<div class="hm-corner"></div>`];
  for (let hr = 0; hr < 24; hr++) {
    cells.push(`<div class="hm-hour">${hr % 3 === 0 ? String(hr).padStart(2, "0") : ""}</div>`);
  }
  for (let wd = 0; wd < 7; wd++) {
    cells.push(`<div class="hm-day">${WEEKDAYS[wd]}</div>`);
    for (let hr = 0; hr < 24; hr++) {
      const count = heat.counts[wd][hr];
      const ratio = heat.max ? count / heat.max : 0;
      const isPeak = heat.peak && heat.peak.wd === wd && heat.peak.hr === hr && count > 0;
      const unmanaged = heat.unmanagedFrac[wd][hr] >= 0.5;
      const cls = `hm-cell${isPeak ? " peak" : ""}${unmanaged ? " unmanaged" : ""}`;
      const bg = count > 0 ? `background:${heatColor(ratio)}` : "";
      const title = `${WEEKDAYS[wd]} ${String(hr).padStart(2, "0")}:00 · ${count} violation${count === 1 ? "" : "s"}${unmanaged ? " · mostly unmanaged" : ""}`;
      cells.push(`<div class="${cls}" style="${bg}" title="${title}"></div>`);
    }
  }
  el.innerHTML = cells.join("");

  document.getElementById("heatmap-legend").innerHTML = `
    <span class="legend-scale">less
      ${RAMP.map((_, i) => `<i style="background:${heatColor(i / (RAMP.length - 1))}"></i>`).join("")}
      more</span>
    <span class="legend-key"><span class="swatch" style="box-shadow:0 0 0 1.5px var(--signal)"></span> peak slot</span>
    <span class="legend-key"><span class="swatch hatch"></span> mostly unmanaged (glocker off)</span>`;
}

function renderWeekdayChart(violations) {
  const data = new Array(7).fill(0);
  for (const v of violations) data[weekdayIndex(new Date(v.ts))]++;
  barChart("chart-weekday", WEEKDAYS, data, "var(--danger)");
}

function renderHourChart(violations) {
  const data = new Array(24).fill(0);
  for (const v of violations) data[new Date(v.ts).getHours()]++;
  const labels = Array.from({ length: 24 }, (_, h) => (h % 2 === 0 ? String(h).padStart(2, "0") : ""));
  barChart("chart-hour", labels, data, "var(--signal)");
}

function renderTimeline(violations, b) {
  const days = Math.round((b.end - b.start) / DAY) + 1;
  const labels = [], data = [], dayTs = [];
  const counts = new Map();
  for (const v of violations) counts.set(dayKey(v.ts), (counts.get(dayKey(v.ts)) || 0) + 1);
  for (let i = 0; i < days; i++) {
    const ts = b.start + i * DAY;
    const d = new Date(ts);
    labels.push(d.getDate() === 1 || i === 0 ? d.toLocaleDateString(undefined, { month: "short", day: "numeric" }) : "");
    data.push(counts.get(dayKey(ts)) || 0);
    dayTs.push(ts);
  }
  // Vertical gridline density adapts to the window: every day for short
  // windows, weekly once daily would crowd, monthly for long spans.
  const granularity = days <= 45 ? "day" : days <= 180 ? "week" : "month";
  const gridAt = (i) => {
    const ts = dayTs[i];
    if (ts == null) return false;
    if (granularity === "day") return true;
    const d = new Date(ts);
    return granularity === "week" ? weekdayIndex(d) === 0 : d.getDate() === 1;
  };

  // Hovering a point fills the day inspector with that day's breakdown.
  lineChart("chart-timeline", labels, data, {
    onPoint: (index) => {
      if (index != null && dayTs[index] != null) showDay(dayTs[index]);
    },
    gridAt,
  });
}

// Milliseconds of a given day [t, t+DAY) that glocker was unmanaged, clipped to
// each span (and to "now" for the still-open span / for today's partial day).
function dayUnmanagedMs(t) {
  let ms = 0;
  for (const p of state.data.unmanaged) {
    const pEnd = p.open ? state.data.now : p.end;
    const s = Math.max(p.start, t);
    const e = Math.min(pEnd, t + DAY);
    if (e > s) ms += e - s;
  }
  return ms;
}

const HOUR_MS = 3600000;

function renderCalendar(violations, b) {
  const counts = new Map();
  for (const v of violations) counts.set(dayKey(v.ts), (counts.get(dayKey(v.ts)) || 0) + 1);
  const max = Math.max(1, ...counts.values());
  const periods = state.data.unmanaged;
  const today = startOfDay(state.data.now);
  const winStart = startOfDay(b.start);
  const winEnd = Math.min(startOfDay(b.end), today);

  // One card per calendar month spanned by the window.
  const months = [];
  let m = new Date(winStart);
  m = new Date(m.getFullYear(), m.getMonth(), 1);
  const lastMonth = new Date(winEnd);
  while (m <= lastMonth) {
    months.push(renderMonth(m, counts, max, winStart, winEnd, today));
    m = new Date(m.getFullYear(), m.getMonth() + 1, 1);
  }

  const legend = `<div class="cal-legend">
    <span class="legend-scale">none
      ${RAMP.map((_, i) => `<i style="background:${heatColor(i / (RAMP.length - 1))}"></i>`).join("")}
      more</span>
    <span class="legend-key"><span class="swatch hatch"></span> unmanaged (proportion of day, &gt;1h)</span>
    <span class="legend-key"><span class="swatch" style="box-shadow:0 0 0 1.5px var(--signal)"></span> today</span>
  </div>`;

  document.getElementById("calendar").innerHTML = months.join("") + legend;
}

function renderMonth(monthStart, counts, max, winStart, winEnd, today) {
  const year = monthStart.getFullYear();
  const month = monthStart.getMonth();
  const daysInMonth = new Date(year, month + 1, 0).getDate();
  const lead = weekdayIndex(monthStart); // Mon=0 blanks before day 1

  const wdHeader = WEEKDAYS.map((d) => `<div class="cal-wd">${d[0]}</div>`).join("");

  const cells = [];
  for (let i = 0; i < lead; i++) cells.push(`<div class="cal-day blank"></div>`);

  let monthTotal = 0;
  for (let day = 1; day <= daysInMonth; day++) {
    const t = new Date(year, month, day).getTime();
    const count = counts.get(dayKey(t)) || 0;
    const inWindow = t >= winStart && t <= winEnd;
    if (inWindow) monthTotal += count;

    // Hatch only the fraction of the day that was unmanaged, and only when it
    // exceeds an hour (below that it's usually just an upgrade blip).
    const unmMs = inWindow ? dayUnmanagedMs(t) : 0;
    const hatched = unmMs > HOUR_MS;
    const frac = Math.min(1, unmMs / DAY);

    const cls = [
      "cal-day",
      inWindow ? "" : "out",
      count > 0 && inWindow ? "has" : "",
      t === today ? "today" : "",
    ].filter(Boolean).join(" ");
    const bg = inWindow && count > 0 ? `background:${heatColor(count / max)}` : "";
    const label = new Date(t).toLocaleDateString(undefined, { weekday: "long", month: "long", day: "numeric", year: "numeric" });
    const title = inWindow
      ? `${label} · ${count} violation${count === 1 ? "" : "s"}${unmMs > 0 ? ` · ${fmtDur(unmMs)} unmanaged` : ""}`
      : `${label} · outside selected window`;
    const data = inWindow ? ` data-ts="${t}"` : "";
    const hatch = hatched ? `<span class="cal-hatch" style="height:${Math.round(frac * 100)}%"></span>` : "";
    cells.push(`<div class="${cls}" style="${bg}" title="${title}"${data}>${hatch}<span class="cal-daynum">${day}</span></div>`);
  }

  const heading = monthStart.toLocaleDateString(undefined, { month: "long", year: "numeric" });
  return `<div class="cal-month">
    <h3>${heading} <span class="mtotal">${monthTotal} hit${monthTotal === 1 ? "" : "s"}</span></h3>
    <div class="cal-weekdays">${wdHeader}</div>
    <div class="cal-days">${cells.join("")}</div>
  </div>`;
}

// ── Day inspector (shared by calendar + timeline hover) ──
function resetDayDetail() {
  document.getElementById("day-detail").innerHTML =
    `<div class="dd-empty">Hover a day in the calendar or a point on the timeline for a breakdown of that day's violations.</div>`;
}

function showDay(dayTs) {
  const k = dayKey(dayTs);
  const hits = (state.dayIndex.get(k) || []).slice().sort((a, b) => a.ts - b.ts);
  const dateLabel = new Date(dayTs).toLocaleDateString(undefined, {
    weekday: "long", month: "long", day: "numeric", year: "numeric",
  });

  const t = startOfDay(dayTs);
  const elapsed = Math.max(HOUR_MS, Math.min(t + DAY, state.data.now) - t); // day so far
  const unmMs = dayUnmanagedMs(t);
  const managedMs = Math.max(0, elapsed - unmMs);
  const unmPct = Math.round((unmMs / elapsed) * 100);
  const fullyUnmanaged = managedMs < HOUR_MS && unmMs > 0;
  const deliberate = hits.length > 2;

  const badges = [];
  if (hits.length) badges.push(`<span class="dd-count">${hits.length} violation${hits.length === 1 ? "" : "s"}</span>`);
  if (deliberate) badges.push(`<span class="badge alarm" title="More than 2 violations in a day — treated as deliberate">deliberate</span>`);
  if (unmMs > HOUR_MS) badges.push(`<span class="badge exposed" title="glocker was uninstalled for part of this day">unmanaged</span>`);
  const head = `<div class="dd-head"><span class="dd-date">${dateLabel}</span><span class="dd-badges">${badges.join("")}</span></div>`;

  // Coverage line — always shown when any of the day was unmanaged, so the
  // managed remainder's stats are never hidden behind an "unmanaged" label.
  let coverage = "";
  if (unmMs > 0) {
    const spans = state.data.unmanaged.filter((p) => t < (p.open ? state.data.now : p.end) && t + DAY > p.start);
    const reasons = [...new Set(spans.map((p) => [p.reason, p.note].filter(Boolean).join(" — ")).filter(Boolean))].join(", ");
    coverage = `<div class="dd-cov">
      <span class="cov-unm">Unmanaged <b>${fmtDur(unmMs)}</b> (${unmPct}%)</span>
      <span class="cov-man">Managed ${fmtDur(managedMs)} · ${hits.length} violation${hits.length === 1 ? "" : "s"}</span>
      ${reasons ? `<span class="cov-why">Reason: ${esc(reasons)}</span>` : ""}
    </div>`;
  }

  let body;
  if (hits.length) {
    const kw = miniRank(tally(hits, "keyword"), "var(--danger)");
    const dom = miniRank(tally(hits, "domain"), "var(--signal)");
    const list = hits
      .map((h) => {
        const time = new Date(h.ts).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hour12: false });
        const kind = h.type === "url-keyword" ? "url" : "page";
        return `<div class="hit">
          <span class="ht">${time}</span>
          <span class="hk">${esc(h.keyword)}</span>
          <span class="hd" title="${esc(h.url)}">${esc(h.domain || "—")} <em>${kind}</em></span>
        </div>`;
      })
      .join("");
    body = coverage + `<div class="dd-grid">
      <div class="dd-sum">
        <h4>Keywords</h4>${kw}
        <h4>Domains</h4>${dom}
      </div>
      <div class="dd-hits"><h4>Hits (${hits.length})</h4><div class="hit-list">${list}</div></div>
    </div>`;
  } else if (fullyUnmanaged) {
    body = coverage + `<div class="dd-note exposed-note">glocker was unmanaged all day — no activity was recorded.</div>`;
  } else if (unmMs > 0) {
    body = coverage + `<div class="dd-note clean-note">No violations during the managed part of the day ✓</div>`;
  } else {
    body = `<div class="dd-note clean-note">No violations — clean day ✓</div>`;
  }

  document.getElementById("day-detail").innerHTML = head + body;
}

function miniRank(items, color) {
  if (!items.length) return `<div class="empty">none</div>`;
  const max = items[0].count;
  return items
    .slice(0, 6)
    .map(
      (it) => `<div class="mrank">
        <span class="mlabel" title="${esc(it.name)}">${esc(it.name)}</span>
        <span class="mbar"><span style="width:${Math.round((it.count / max) * 100)}%;background:${color}"></span></span>
        <span class="mcount">${it.count}</span>
      </div>`
    )
    .join("");
}

function renderRanklist(id, items, color) {
  const el = document.getElementById(id);
  if (!items.length) {
    el.innerHTML = `<div class="empty">no data in window</div>`;
    return;
  }
  const max = items[0].count;
  el.innerHTML = items
    .slice(0, 8)
    .map((it) => {
      const pct = Math.round((it.count / max) * 100);
      return `<div class="rank">
        <span class="label" title="${esc(it.name)}">${esc(it.name)}</span>
        <span class="count">${it.count}</span>
        <span class="track"><span class="fill" style="width:${pct}%;background:${color}"></span></span>
      </div>`;
    })
    .join("");
}

function renderUnmanaged(unmanaged) {
  const el = document.getElementById("unmanaged-list");
  if (!unmanaged.length) {
    el.innerHTML = `<div class="empty">none in window — fully covered ✓</div>`;
    return;
  }
  el.innerHTML = [...unmanaged]
    .sort((a, b) => b.start - a.start)
    .map((p) => {
      const why = [p.reason, p.note].filter(Boolean).join(" — ") || "no reason given";
      const when = new Date(p.start).toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
      return `<div class="um ${p.open ? "open" : ""}">
        <span class="when">${when}</span>
        <span class="why">${esc(why)}</span>
        <span class="dur">${fmtDur(p.end - p.start)}</span>
      </div>`;
    })
    .join("");
}

// ── Usage metrics ───────────────────────────────────────
// The usage log samples the focused window + idle time every ~10s. We turn the
// samples into durations by attributing the gap until the next sample to that
// sample's active window — unless the user was idle (AFK) or the tracker had a
// gap (capped so a tracker restart doesn't count as hours of use).
const USAGE_IDLE_THRESHOLD_MS = 60_000; // idle ≥ 60s → counted as away, not active
const USAGE_MAX_GAP_MS = 5 * 60_000;    // cap per-sample attribution across gaps

// Attribute each sample's duration (the gap until the next sample, capped) and
// call fn(entry, dt, away). `away` folds in idle and no-focus so both the raw
// metrics and the rule categorizer share one definition of "active".
// idleMs === -1 means the screensaver extension was unavailable; treat that as
// active since we can't prove otherwise.
function eachSampleDuration(entries, fn) {
  const gaps = [];
  for (let i = 1; i < entries.length; i++) gaps.push(entries[i].ts - entries[i - 1].ts);
  gaps.sort((a, b) => a - b);
  const nominal = gaps.length ? gaps[Math.floor(gaps.length / 2)] : 10_000;

  for (let i = 0; i < entries.length; i++) {
    const e = entries[i];
    let dt = i + 1 < entries.length ? entries[i + 1].ts - e.ts : nominal;
    if (dt <= 0) continue;
    dt = Math.min(dt, USAGE_MAX_GAP_MS);
    const away = e.idleMs >= USAGE_IDLE_THRESHOLD_MS || !e.active;
    fn(e, dt, away);
  }
}

const rankMap = (m) => [...m.entries()].map(([name, ms]) => ({ name, ms })).sort((a, b) => b.ms - a.ms);

function computeUsage(entries) {
  const byApp = new Map();
  const byTitle = new Map();
  const byDay = new Map();
  const byHour = new Array(24).fill(0);
  let activeMs = 0, idleMs = 0;

  eachSampleDuration(entries, (e, dt, away) => {
    if (away) {
      idleMs += dt;
      return;
    }
    activeMs += dt;
    const app = (e.active.class || "").trim() || "unknown";
    byApp.set(app, (byApp.get(app) || 0) + dt);
    const title = e.active.title || "—";
    byTitle.set(title, (byTitle.get(title) || 0) + dt);
    byDay.set(dayKey(e.ts), (byDay.get(dayKey(e.ts)) || 0) + dt);
    byHour[new Date(e.ts).getHours()] += dt;
  });

  return {
    activeMs,
    idleMs,
    trackedMs: activeMs + idleMs,
    apps: rankMap(byApp),
    titles: rankMap(byTitle),
    byDay,
    byHour,
  };
}

// ── Rule-based categorization (arbtt-style) ─────────────
// Rules are applied client-side so edits recategorize instantly. Each rule is
// { program, title, tag }: optional case-insensitive regexes on the active
// window's class and title. The first rule whose present regexes all match
// wins; $1..$9 in the tag interpolate that rule's regex captures.
function compileRules(rules) {
  return rules.map((r) => {
    let program = null, title = null, error = false;
    try { if (r.program) program = new RegExp(r.program, "i"); } catch { error = true; }
    try { if (r.title) title = new RegExp(r.title, "i"); } catch { error = true; }
    return { program, title, tag: (r.tag || "").trim(), error };
  });
}

function interpolateTag(tag, match) {
  if (!match) return tag;
  return tag.replace(/\$([1-9])/g, (_, n) => match[Number(n)] || "");
}

function tagForSample(e, compiled) {
  if (!e.active) return null;
  const cls = e.active.class || "";
  const title = e.active.title || "";
  for (const c of compiled) {
    if (c.error || !c.tag) continue;
    let pm = null, tm = null;
    if (c.program) { pm = c.program.exec(cls); if (!pm) continue; }
    if (c.title) { tm = c.title.exec(title); if (!tm) continue; }
    return interpolateTag(c.tag, tm || pm); // named/numbered captures from the last regex to match
  }
  return null;
}

const UNTAGGED = "(untagged)";

function computeTagUsage(entries, compiled) {
  const byTag = new Map();
  const hourByTag = new Map(); // tag -> [24] ms, for the stacked hour-of-day chart
  const untaggedByProg = new Map(); // program -> { ms, titles: Map<title, ms> }
  let untagged = 0, active = 0;

  const addHour = (name, hr, dt) => {
    let a = hourByTag.get(name);
    if (!a) { a = new Array(24).fill(0); hourByTag.set(name, a); }
    a[hr] += dt;
  };

  eachSampleDuration(entries, (e, dt, away) => {
    if (away) return;
    active += dt;
    const hr = new Date(e.ts).getHours();
    const tag = tagForSample(e, compiled);
    if (tag) {
      byTag.set(tag, (byTag.get(tag) || 0) + dt);
      addHour(tag, hr, dt);
    } else {
      untagged += dt;
      addHour(UNTAGGED, hr, dt);
      // Track what's untagged so the user can see it and turn it into a rule.
      const prog = (e.active.class || "").trim() || "unknown";
      let u = untaggedByProg.get(prog);
      if (!u) { u = { ms: 0, titles: new Map() }; untaggedByProg.set(prog, u); }
      u.ms += dt;
      const title = e.active.title || "—";
      u.titles.set(title, (u.titles.get(title) || 0) + dt);
    }
  });

  // Flatten untagged into a ranked list, each with its heaviest example title.
  const untaggedItems = [...untaggedByProg.entries()]
    .map(([program, u]) => {
      const top = [...u.titles.entries()].sort((a, b) => b[1] - a[1])[0];
      return { program, ms: u.ms, title: top ? top[0] : "", titleCount: u.titles.size };
    })
    .sort((a, b) => b.ms - a.ms);

  return { tags: rankMap(byTag), untagged, active, hourByTag, untaggedItems };
}

// Canonical ordering shared by the tag pie and the stacked hour chart, so a tag
// keeps the same colour in both. Tags sorted by total desc, untagged last-ish
// (sorted in by size but flagged so it always renders grey).
function orderedTagItems(tu) {
  const items = tu.tags.slice();
  if (tu.untagged > 0) items.push({ name: UNTAGGED, ms: tu.untagged, untagged: true });
  items.sort((a, b) => b.ms - a.ms);
  return items;
}

// A tag's colour is the user's saved override if any, else a stable palette
// slot by rank. Untagged is always the muted faint ink.
function tagColorMap(items) {
  const map = new Map();
  let ci = 0;
  for (const it of items) {
    if (it.untagged) {
      map.set(it.name, cssVar("var(--ink-faint)"));
      continue;
    }
    const fallback = TAG_COLORS[ci++ % TAG_COLORS.length];
    map.set(it.name, state.tagColors[it.name] || fallback);
  }
  return map;
}

function renderUsage(b) {
  const available = state.data.sources && state.data.sources.usage;
  const entries = (state.data.usage || []).filter((e) => within(e.ts, b));
  // Stash the in-window samples so rule edits can recategorize without a
  // full re-render of the rest of the view.
  state.usageWindow = { entries, available };
  const u = computeUsage(entries);

  renderUsageReadout(u, available);
  renderDurationRank("usage-apps", u.apps, "var(--signal)", available);
  renderDurationRank("usage-titles", u.titles, "var(--safe)", available);
  renderUsageTimeline(u, b);
  renderUsageHourChart(u);
  recategorize();
}

// Recompute and render only the "Time by tag" panel from the current window
// and rules — cheap enough to run on every keystroke in the rule editor.
function recategorize({ colorsOnly = false } = {}) {
  const win = state.usageWindow;
  if (!win) return;
  const tu = computeTagUsage(win.entries, compileRules(state.rules));
  renderUsageTags(tu, win.available);
  renderUsageTagHours(tu, win.available);
  renderUntagged(tu, win.available);
  // Skip re-rendering the colour pickers when a picker itself triggered this,
  // so the open colour dialog / focus isn't disturbed mid-drag.
  if (!colorsOnly) renderTagColors(tu, win.available);
}

// One colour swatch per tag (excluding untagged), shown in the Rules panel.
// Editing recolours the charts live; the value is saved with the rules.
function renderTagColors(tu, available) {
  const el = document.getElementById("tag-colors");
  if (!el) return;
  const ordered = orderedTagItems(tu);
  const items = ordered.filter((it) => !it.untagged);
  if (!available || !items.length) {
    el.innerHTML = `<span class="tagcolor-empty">Tag colours appear here once rules produce tags.</span>`;
    return;
  }
  const colors = tagColorMap(ordered);
  el.innerHTML = items
    .map(
      (it) => `<label class="tagcolor" title="Colour for ${esc(it.name)}">
        <input type="color" data-tagcolor="${esc(it.name)}" value="${colors.get(it.name)}" />
        <span>${esc(it.name)}</span>
      </label>`
    )
    .join("");
}

// The active time no rule matched, grouped by program with an example title, so
// the user can see what still needs a rule (and click to start one).
function renderUntagged(tu, available) {
  const el = document.getElementById("usage-untagged");
  if (!available) {
    el.innerHTML = `<div class="empty">usage tracking is off</div>`;
    return;
  }
  const items = tu.untaggedItems || [];
  if (!items.length) {
    el.innerHTML = `<div class="empty">nothing untagged — full coverage ✓</div>`;
    return;
  }
  const max = items[0].ms || 1;
  el.innerHTML = items
    .slice(0, 15)
    .map((it) => {
      const pct = Math.round((it.ms / max) * 100);
      const more = it.titleCount > 1 ? ` <em>+${it.titleCount - 1} more</em>` : "";
      return `<div class="untag">
        <div class="untag-info">
          <span class="untag-prog">${esc(it.program)}</span>
          <span class="untag-title" title="${esc(it.title)}">${esc(it.title)}${more}</span>
        </div>
        <span class="untag-time">${esc(fmtDur(it.ms))}</span>
        <button class="untag-tag" type="button" data-tag-program="${esc(it.program)}" title="Start a rule for ${esc(it.program)}">&plus; tag</button>
        <span class="untag-bar"><span style="width:${pct}%"></span></span>
      </div>`;
    })
    .join("");
}

// Start a rule for a program: append a prefilled row to the editor, focus its
// tag field, and let the user name it and Save.
function startRuleForProgram(program) {
  syncRulesFromDom();
  state.rules.push({ program: `^${escapeRegex(program)}$`, title: "", tag: "" });
  renderRulesEditor();
  markRuleValidity();
  const rows = document.querySelectorAll("#rules-rows .rule-row");
  const last = rows[rows.length - 1];
  if (last) {
    last.scrollIntoView({ behavior: "smooth", block: "center" });
    last.querySelector('[data-field="tag"]').focus();
  }
  recategorize();
}

function escapeRegex(s) {
  return String(s).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

// Categorical palette for the tag pie (distinct hues that read on the dark
// theme). "(untagged)" is always rendered in the muted faint ink instead.
const TAG_COLORS = [
  "#f0a020", "#2f9e8f", "#4a90d9", "#c0468f", "#7bc86c",
  "#9b6dff", "#ff4d4d", "#e8734a", "#54c7d6", "#c9a227",
];

function renderUsageTags(tu, available) {
  const wrap = document.getElementById("usage-tags-wrap");
  const items = orderedTagItems(tu); // already sorted by ms desc == percentage desc
  if (!available || !items.length) {
    destroy("chart-usage-tags");
    wrap.innerHTML = `<div class="empty">${available ? "no active time in window" : "usage tracking is off"}</div>`;
    return;
  }
  // Re-create the canvas if a previous empty state replaced it.
  if (!document.getElementById("chart-usage-tags")) {
    wrap.innerHTML = `<canvas id="chart-usage-tags"></canvas>`;
  }
  const total = items.reduce((s, it) => s + it.ms, 0) || 1;
  const colors = tagColorMap(items);
  hbarChart("chart-usage-tags", {
    labels: items.map((it) => it.name),
    pct: items.map((it) => (100 * it.ms) / total),
    ms: items.map((it) => it.ms),
    colors: items.map((it) => colors.get(it.name)),
  });
}

// Hour-of-day (00–23) on X, hours on Y, one stacked segment per tag. Shows when
// during the day each tagged activity happens.
function renderUsageTagHours(tu, available) {
  const wrap = document.getElementById("usage-taghours-wrap");
  const items = orderedTagItems(tu);
  if (!available || !items.length) {
    destroy("chart-usage-taghours");
    wrap.innerHTML = `<div class="empty">${available ? "no active time in window" : "usage tracking is off"}</div>`;
    return;
  }
  if (!document.getElementById("chart-usage-taghours")) {
    wrap.innerHTML = `<canvas id="chart-usage-taghours"></canvas>`;
  }
  const colors = tagColorMap(items);
  const labels = Array.from({ length: 24 }, (_, h) => (h % 2 === 0 ? String(h).padStart(2, "0") : ""));
  const datasets = items.map((it) => ({
    label: it.name,
    data: (tu.hourByTag.get(it.name) || new Array(24).fill(0)).map((ms) => ms / 3600000),
    backgroundColor: colors.get(it.name),
    stack: "tags",
    borderWidth: 0,
    maxBarThickness: 22,
  }));
  stackedBarChart("chart-usage-taghours", labels, datasets);
}

// ── Rule editor ─────────────────────────────────────────
function setupRules() {
  renderRulesEditor();

  const rows = document.getElementById("rules-rows");
  // Live feedback: sync + recategorize on every keystroke, without re-rendering
  // the editor (which would drop focus).
  rows.addEventListener("input", () => {
    syncRulesFromDom();
    markRuleValidity();
    recategorize();
  });
  rows.addEventListener("click", (e) => {
    const del = e.target.closest("[data-del]");
    if (!del) return;
    syncRulesFromDom();
    state.rules.splice(Number(del.dataset.del), 1);
    renderRulesEditor();
    markRuleValidity();
    recategorize();
  });

  document.getElementById("rule-add").addEventListener("click", () => {
    syncRulesFromDom();
    state.rules.push({ program: "", title: "", tag: "" });
    renderRulesEditor();
    markRuleValidity();
  });
  document.getElementById("rule-save").addEventListener("click", saveRulesToServer);

  // "＋ tag" on an untagged row prefills a rule for that program.
  document.getElementById("usage-untagged").addEventListener("click", (e) => {
    const btn = e.target.closest("[data-tag-program]");
    if (btn) startRuleForProgram(btn.dataset.tagProgram);
  });

  // Per-tag colour pickers: recolour charts live; the value is saved with rules.
  document.getElementById("tag-colors").addEventListener("input", (e) => {
    const inp = e.target.closest("[data-tagcolor]");
    if (!inp) return;
    state.tagColors[inp.dataset.tagcolor] = inp.value;
    setRulesStatus("unsaved colour — click Save rules", "");
    recategorize({ colorsOnly: true });
  });
}

function setRulesStatus(msg, cls) {
  const s = document.getElementById("rules-status");
  s.className = "rules-status" + (cls ? " " + cls : "");
  s.textContent = msg;
}

function renderRulesEditor() {
  const wrap = document.getElementById("rules-rows");
  if (!state.rules.length) {
    wrap.innerHTML = `<div class="rules-empty">No rules yet — add one to categorize your active time.</div>`;
    return;
  }
  wrap.innerHTML = state.rules
    .map(
      (r, i) => `<div class="rule-row" data-i="${i}">
        <input data-field="program" value="${esc(r.program || "")}" placeholder="^Emacs$" spellcheck="false" autocomplete="off" />
        <input data-field="title" value="${esc(r.title || "")}" placeholder="regex on title" spellcheck="false" autocomplete="off" />
        <input data-field="tag" value="${esc(r.tag || "")}" placeholder="Activity:dev" spellcheck="false" autocomplete="off" />
        <button class="rule-del" data-del="${i}" title="Delete rule" type="button">&times;</button>
      </div>`
    )
    .join("");
}

function syncRulesFromDom() {
  const rows = [...document.querySelectorAll("#rules-rows .rule-row")];
  state.rules = rows.map((row) => ({
    program: row.querySelector('[data-field="program"]').value.trim(),
    title: row.querySelector('[data-field="title"]').value.trim(),
    tag: row.querySelector('[data-field="tag"]').value.trim(),
  }));
}

// Flag rows whose program/title is not a valid regex so the user sees why a
// rule isn't matching.
function markRuleValidity() {
  for (const row of document.querySelectorAll("#rules-rows .rule-row")) {
    let bad = false;
    for (const f of ["program", "title"]) {
      const v = row.querySelector(`[data-field="${f}"]`).value.trim();
      if (v) {
        try { new RegExp(v); } catch { bad = true; }
      }
    }
    row.classList.toggle("invalid", bad);
  }
}

async function saveRulesToServer() {
  syncRulesFromDom();
  const btn = document.getElementById("rule-save");
  btn.disabled = true;
  setRulesStatus("saving…", "");
  try {
    const res = await fetch("/api/rules", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ rules: state.rules, colors: state.tagColors }),
    });
    if (!res.ok) throw new Error(`server returned ${res.status}`);
    const cfg = await res.json(); // canonical, server-sanitized
    state.rules = cfg.rules || [];
    state.tagColors = cfg.colors || {};
    renderRulesEditor();
    markRuleValidity();
    recategorize();
    setRulesStatus(`saved ${state.rules.length} rule${state.rules.length === 1 ? "" : "s"} ✓`, "ok");
  } catch (err) {
    setRulesStatus(`save failed: ${err.message}`, "err");
  } finally {
    btn.disabled = false;
  }
}

function renderUsageReadout(u, available) {
  const el = document.getElementById("usage-readout");
  if (!available) {
    el.innerHTML = `<div class="stat" title="No usage log was found on the server.">
      <div class="k">Usage tracking</div>
      <div class="v" style="font-size:20px">off</div>
      <div class="sub">no usage log — run usage-tracker or set GLOCKER_USAGE_LOG</div>
    </div>`;
    return;
  }
  const activePct = u.trackedMs ? Math.round((u.activeMs / u.trackedMs) * 100) : 0;
  const top = u.apps[0];
  const stats = [
    {
      k: "Active time", v: fmtDur(u.activeMs), sub: `${activePct}% of tracked`,
      help: "Time a window was focused and you were not idle, in the selected window.",
    },
    {
      k: "Idle time", v: fmtDur(u.idleMs), sub: `${100 - activePct}% of tracked`,
      help: `Tracked time with no input for ≥${USAGE_IDLE_THRESHOLD_MS / 1000}s (or no focused window).`,
    },
    {
      k: "Applications", v: u.apps.length, sub: "distinct apps",
      help: "Number of distinct window classes you were actively using.",
    },
    {
      k: "Busiest app", v: top ? top.name : "—", sub: top ? fmtDur(top.ms) : "no data", cls: "peak", big: false,
      help: "The application with the most active time in the window.",
    },
  ];
  el.innerHTML = stats
    .map(
      (s) => `<div class="stat ${s.cls || ""}" title="${esc(s.help)}">
        <div class="k">${s.k}</div>
        <div class="v" ${s.big === false ? 'style="font-size:20px"' : ""}>${esc(String(s.v))}</div>
        <div class="sub">${esc(s.sub)}</div>
      </div>`
    )
    .join("");
}

// Like renderRanklist, but the value is a duration rather than a count.
function renderDurationRank(id, items, color, available) {
  const el = document.getElementById(id);
  if (!available) {
    el.innerHTML = `<div class="empty">usage tracking is off</div>`;
    return;
  }
  if (!items.length) {
    el.innerHTML = `<div class="empty">no usage in window</div>`;
    return;
  }
  const max = items[0].ms;
  el.innerHTML = items
    .slice(0, 8)
    .map((it) => {
      const pct = max ? Math.round((it.ms / max) * 100) : 0;
      return `<div class="rank">
        <span class="label" title="${esc(it.name)}">${esc(it.name)}</span>
        <span class="count">${esc(fmtDur(it.ms))}</span>
        <span class="track"><span class="fill" style="width:${pct}%;background:${color}"></span></span>
      </div>`;
    })
    .join("");
}

function renderUsageTimeline(u, b) {
  const days = Math.round((b.end - b.start) / DAY) + 1;
  const labels = [], data = [];
  for (let i = 0; i < days; i++) {
    const ts = b.start + i * DAY;
    const d = new Date(ts);
    labels.push(d.getDate() === 1 || i === 0 ? d.toLocaleDateString(undefined, { month: "short", day: "numeric" }) : "");
    data.push(Math.round((u.byDay.get(dayKey(ts)) || 0) / 60000));
  }
  barChart("chart-usage-timeline", labels, data, "var(--signal)");
}

function renderUsageHourChart(u) {
  const data = u.byHour.map((ms) => Math.round(ms / 60000));
  const labels = Array.from({ length: 24 }, (_, h) => (h % 2 === 0 ? String(h).padStart(2, "0") : ""));
  barChart("chart-usage-hour", labels, data, "var(--safe)");
}

function renderFooter() {
  const s = state.data.sources;
  const mark = (ok) => (ok ? "✓" : "✗ missing");
  document.getElementById("footer-sources").textContent =
    `reports ${mark(s.reports)}  ·  unblocks ${mark(s.unblocks)}  ·  lifecycle ${mark(s.lifecycle)}  ·  usage ${mark(s.usage)}  ·  read ${new Date(state.data.now).toLocaleString()}`;
}

// ── Chart.js helpers ────────────────────────────────────
function cssVar(name) {
  return getComputedStyle(document.documentElement).getPropertyValue(name.replace("var(", "").replace(")", "")).trim();
}
const GRID = "rgba(255,255,255,0.08)";
const TICK = "#aeb9c9";

function destroy(id) {
  if (state.charts[id]) state.charts[id].destroy();
}

// Horizontal bar: percentage on X, category (tag) on Y, one coloured bar each.
// Pass parallel arrays; ms is used only for the tooltip.
function hbarChart(id, { labels, pct, ms, colors }) {
  destroy(id);
  const opts = baseOpts();
  opts.indexAxis = "y";
  opts.scales = {
    x: {
      beginAtZero: true,
      grid: { color: GRID },
      ticks: { color: TICK, font: { family: "IBM Plex Mono", size: 10 }, callback: (v) => `${v}%` },
    },
    y: {
      grid: { display: false },
      ticks: { color: TICK, font: { family: "IBM Plex Mono", size: 11 }, autoSkip: false },
    },
  };
  opts.plugins.legend = { display: false };
  opts.plugins.tooltip = {
    callbacks: { label: (ctx) => ` ${fmtDur(ms[ctx.dataIndex])} (${pct[ctx.dataIndex].toFixed(1)}%)` },
  };
  state.charts[id] = new Chart(document.getElementById(id), {
    type: "bar",
    data: {
      labels,
      datasets: [{ data: pct, backgroundColor: colors, borderWidth: 0, borderRadius: 3, maxBarThickness: 26 }],
    },
    options: opts,
  });
}

function stackedBarChart(id, labels, datasets) {
  destroy(id);
  const opts = baseOpts();
  opts.interaction = { mode: "index", intersect: false };
  opts.scales.x.stacked = true;
  opts.scales.y.stacked = true;
  opts.scales.y.ticks.callback = (v) => `${v}h`;
  opts.plugins.legend = {
    display: true,
    position: "bottom",
    labels: { color: TICK, font: { family: "IBM Plex Sans", size: 11 }, boxWidth: 12, padding: 8 },
  };
  opts.plugins.tooltip = {
    mode: "index",
    intersect: false,
    filter: (item) => item.parsed.y > 0, // hide the many zero-height segments
    callbacks: { label: (ctx) => ` ${ctx.dataset.label}: ${ctx.parsed.y.toFixed(1)}h` },
  };
  state.charts[id] = new Chart(document.getElementById(id), {
    type: "bar",
    data: { labels, datasets },
    options: opts,
  });
}

function barChart(id, labels, data, colorVar) {
  destroy(id);
  const color = cssVar(colorVar);
  state.charts[id] = new Chart(document.getElementById(id), {
    type: "bar",
    data: { labels, datasets: [{ data, backgroundColor: color, borderRadius: 3, maxBarThickness: 34 }] },
    options: baseOpts(),
  });
}

function lineChart(id, labels, data, { onPoint, gridAt } = {}) {
  destroy(id);
  const ctx = document.getElementById(id).getContext("2d");
  const grad = ctx.createLinearGradient(0, 0, 0, 240);
  grad.addColorStop(0, "rgba(255,77,77,0.35)");
  grad.addColorStop(1, "rgba(255,77,77,0)");
  const opts = baseOpts();
  opts.interaction = { mode: "index", intersect: false };
  opts.plugins.tooltip.enabled = false; // the day inspector replaces the native tooltip
  if (onPoint) {
    opts.onHover = (_e, els) => onPoint(els.length ? els[0].index : null);
  }
  if (gridAt) {
    // Vertical gridlines only at ticks the predicate selects (keeps 1y readable).
    opts.scales.x.grid = {
      display: true,
      drawTicks: false,
      color: (c) => (gridAt(c.index) ? GRID : "transparent"),
    };
  }
  state.charts[id] = new Chart(ctx, {
    type: "line",
    data: {
      labels,
      datasets: [{
        data,
        borderColor: cssVar("var(--danger)"),
        backgroundColor: grad,
        fill: true,
        tension: 0.3,
        pointRadius: 0,
        pointHoverRadius: 4,
        pointHoverBackgroundColor: cssVar("var(--danger)"),
        borderWidth: 1.5,
      }],
    },
    options: opts,
  });
}

function baseOpts() {
  return {
    responsive: true,
    maintainAspectRatio: false,
    plugins: { legend: { display: false }, tooltip: { intersect: false, mode: "index" } },
    scales: {
      x: { grid: { display: false }, ticks: { color: TICK, font: { family: "IBM Plex Mono", size: 10 }, maxRotation: 0, autoSkip: false } },
      y: { beginAtZero: true, grid: { color: GRID }, ticks: { color: TICK, font: { family: "IBM Plex Mono", size: 10 }, precision: 0 } },
    },
  };
}

// ── Misc ────────────────────────────────────────────────
function fmtDur(ms) {
  if (ms <= 0) return "0";
  const mins = Math.round(ms / 60000);
  if (mins < 60) return `${mins}m`;
  const d = Math.floor(mins / 1440), h = Math.floor((mins % 1440) / 60), m = mins % 60;
  if (d > 0) return `${d}d ${h}h`;
  return `${h}h ${m}m`;
}

function esc(s) {
  return String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
}
