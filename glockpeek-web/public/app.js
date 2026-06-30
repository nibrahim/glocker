// glockpeek-web frontend. Pulls the full parsed history from /api/data, then
// does all aggregation/filtering in the browser (the data set is tiny). The
// vulnerability heatmap is the centrepiece: it surfaces the hour/weekday slots
// where you actually slip — and hatches the slots where glocker was off, so a
// blank cell is never mistaken for a safe one.

const WEEKDAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const DAY = 86400000;
const HOUR = 3600000;

const RANGES = [
  { id: "30d", label: "30d", days: 30 },
  { id: "90d", label: "90d", days: 90 },
  { id: "1y", label: "1y", days: 365 },
  { id: "all", label: "all", days: null },
];

const state = { data: null, range: "90d", charts: {} };

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
  document.getElementById("loading").hidden = true;
  document.getElementById("dash").hidden = false;
  renderFooter();
  render();
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
      wrap.querySelectorAll("button").forEach((x) => x.classList.toggle("active", x.dataset.id === r.id));
      render();
    });
    wrap.appendChild(b);
  }
}

function windowBounds() {
  const now = state.data.now;
  const r = RANGES.find((x) => x.id === state.range);
  if (!r.days) {
    // "all" — from the earliest event we have.
    const firstTs = Math.min(
      ...[state.data.violations, state.data.unblocks, state.data.lifecycle]
        .map((a) => (a.length ? a[0].ts : Infinity))
    );
    const start = Number.isFinite(firstTs) ? startOfDay(firstTs) : startOfDay(now - 30 * DAY);
    return { start, end: now };
  }
  return { start: startOfDay(now - (r.days - 1) * DAY), end: now };
}

const within = (ts, b) => ts >= b.start && ts <= b.end;

// ── Master render ───────────────────────────────────────
function render() {
  const b = windowBounds();
  const violations = state.data.violations.filter((v) => within(v.ts, b));
  const unblocks = state.data.unblocks.filter((u) => within(u.ts, b));
  const unmanaged = state.data.unmanaged
    .map((p) => ({ ...p, start: Math.max(p.start, b.start), end: Math.min(p.end, b.end) }))
    .filter((p) => p.end > p.start);

  const heat = buildHeatmap(violations, b);
  renderReadout(violations, unblocks, unmanaged, heat, b);
  renderHeatmap(heat);
  renderWeekdayChart(violations);
  renderHourChart(violations);
  renderCalendar(violations, b);
  renderTimeline(violations, b);
  renderRanklist("top-keywords", tally(violations, "keyword"), "var(--danger)");
  renderRanklist("top-domains", tally(violations, "domain"), "var(--signal)");
  renderRanklist("unblock-reasons", tally(unblocks, "reason"), "var(--safe)");
  renderUnmanaged(unmanaged);
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
  const labels = [], data = [];
  const counts = new Map();
  for (const v of violations) counts.set(dayKey(v.ts), (counts.get(dayKey(v.ts)) || 0) + 1);
  for (let i = 0; i < days; i++) {
    const ts = b.start + i * DAY;
    const d = new Date(ts);
    labels.push(d.getDate() === 1 || i === 0 ? d.toLocaleDateString(undefined, { month: "short", day: "numeric" }) : "");
    data.push(counts.get(dayKey(ts)) || 0);
  }
  lineChart("chart-timeline", labels, data);
}

function renderCalendar(violations, b) {
  const counts = new Map();
  for (const v of violations) counts.set(dayKey(v.ts), (counts.get(dayKey(v.ts)) || 0) + 1);
  const max = Math.max(1, ...counts.values());
  const periods = state.data.unmanaged;
  const today = startOfDay(state.data.now);
  const winStart = startOfDay(b.start);
  const winEnd = Math.min(startOfDay(b.end), today);

  const isUnmanaged = (t) =>
    periods.some((p) => t < (p.open ? state.data.now : p.end) && t + DAY > p.start);

  // One card per calendar month spanned by the window.
  const months = [];
  let m = new Date(winStart);
  m = new Date(m.getFullYear(), m.getMonth(), 1);
  const lastMonth = new Date(winEnd);
  while (m <= lastMonth) {
    months.push(renderMonth(m, counts, max, isUnmanaged, winStart, winEnd, today));
    m = new Date(m.getFullYear(), m.getMonth() + 1, 1);
  }

  const legend = `<div class="cal-legend">
    <span class="legend-scale">none
      ${RAMP.map((_, i) => `<i style="background:${heatColor(i / (RAMP.length - 1))}"></i>`).join("")}
      more</span>
    <span class="legend-key"><span class="swatch hatch"></span> unmanaged (glocker off)</span>
    <span class="legend-key"><span class="swatch" style="box-shadow:0 0 0 1.5px var(--signal)"></span> today</span>
  </div>`;

  document.getElementById("calendar").innerHTML = months.join("") + legend;
}

function renderMonth(monthStart, counts, max, isUnmanaged, winStart, winEnd, today) {
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
    const unmanaged = inWindow && isUnmanaged(t);
    const cls = [
      "cal-day",
      inWindow ? "" : "out",
      count > 0 && inWindow ? "has" : "",
      unmanaged ? "unmanaged" : "",
      t === today ? "today" : "",
    ].filter(Boolean).join(" ");
    const bg = inWindow && count > 0 && !unmanaged ? `background:${heatColor(count / max)}` : "";
    const label = new Date(t).toLocaleDateString(undefined, { weekday: "long", month: "long", day: "numeric", year: "numeric" });
    const title = inWindow
      ? `${label} · ${count} violation${count === 1 ? "" : "s"}${unmanaged ? " · glocker unmanaged this day" : ""}`
      : `${label} · outside selected window`;
    cells.push(`<div class="${cls}" style="${bg}" title="${title}">${day}</div>`);
  }

  const heading = monthStart.toLocaleDateString(undefined, { month: "long", year: "numeric" });
  return `<div class="cal-month">
    <h3>${heading} <span class="mtotal">${monthTotal} hit${monthTotal === 1 ? "" : "s"}</span></h3>
    <div class="cal-weekdays">${wdHeader}</div>
    <div class="cal-days">${cells.join("")}</div>
  </div>`;
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

function renderFooter() {
  const s = state.data.sources;
  const mark = (ok) => (ok ? "✓" : "✗ missing");
  document.getElementById("footer-sources").textContent =
    `reports ${mark(s.reports)}  ·  unblocks ${mark(s.unblocks)}  ·  lifecycle ${mark(s.lifecycle)}  ·  read ${new Date(state.data.now).toLocaleString()}`;
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

function barChart(id, labels, data, colorVar) {
  destroy(id);
  const color = cssVar(colorVar);
  state.charts[id] = new Chart(document.getElementById(id), {
    type: "bar",
    data: { labels, datasets: [{ data, backgroundColor: color, borderRadius: 3, maxBarThickness: 34 }] },
    options: baseOpts(),
  });
}

function lineChart(id, labels, data) {
  destroy(id);
  const ctx = document.getElementById(id).getContext("2d");
  const grad = ctx.createLinearGradient(0, 0, 0, 240);
  grad.addColorStop(0, "rgba(255,77,77,0.35)");
  grad.addColorStop(1, "rgba(255,77,77,0)");
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
        borderWidth: 1.5,
      }],
    },
    options: baseOpts(),
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
