# glockpeek-web

A tiny web companion to the `glockpeek` CLI. It reads the same glocker log
files and lets you walk through your history visually — to find the times you
are most **vulnerable to failure**.

The headline view is the **vulnerability map**: a weekday × hour heatmap of
violations. Slots where glocker was uninstalled are hatched, so a blank cell is
never mistaken for a clean one.

## What it shows

- **Vulnerability map** — violations by weekday × hour, with the peak slot glowing.
- **Readout strip** — totals, deliberate days (>2 hits), clean days, peak exposure window, unmanaged time, unblocks.
- **By weekday / by hour** — frequency distributions.
- **History calendar** — GitHub-style per-day grid; red = violations, hatched = unmanaged.
- **Timeline** — violations per day across the window.
- **Top keywords / domains** — what trips the blocker.
- **Unblocks** — deliberate temporary grants, by reason.
- **Unmanaged exposure** — every span when glocker was uninstalled.

Use the **window** control (30d / 90d / 1y / all) to walk through history.

## Data sources

Reads the three glocker logs (same defaults as `internal/reports`):

| Log | Default path | Override env var |
|-----|--------------|------------------|
| Violations | `/var/log/glocker-reports.log` | `GLOCKER_REPORTS_LOG` |
| Unblocks | `/var/log/glocker-unblocks.log` | `GLOCKER_UNBLOCKS_LOG` |
| Lifecycle | `/var/log/glocker-lifecycle.log` | `GLOCKER_LIFECYCLE_LOG` |

Unmanaged periods are computed from the lifecycle log (uninstall→install pairs),
ignoring spans under 2 minutes — matching `cmd/glockpeek/main.go`.

## Run

```bash
cd glockpeek-web
npm install
npm start                 # http://localhost:4317
```

`PORT` overrides the listen port. The browser must reach a host that can read
the glocker logs (run it on the same machine as glocker).

## API

Two read-only JSON endpoints, documented in [`openapi.yaml`](./openapi.yaml):

- `GET /api/data` — the full parsed history (violations, unblocks, lifecycle, unmanaged spans).
- `GET /api/health` — liveness and which log files were found.

All aggregation and date-range filtering happens client-side; the data set is
small (a few hundred rows).
