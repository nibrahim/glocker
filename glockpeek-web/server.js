// glockpeek-web: a tiny server that parses the glocker logs and serves them to
// the browser dashboard. All aggregation happens client-side — the data set is
// small (a few hundred rows) so the API just hands over the parsed history.
import express from "express";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { loadAll, DEFAULT_PATHS } from "./lib/parse.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const PORT = process.env.PORT || 4317;

const app = express();
app.disable("x-powered-by");

// GET /api/data — the entire parsed history (violations, unblocks, lifecycle,
// and computed unmanaged periods). See openapi.yaml for the schema.
app.get("/api/data", async (_req, res) => {
  try {
    const data = await loadAll(DEFAULT_PATHS, Date.now());
    res.set("Cache-Control", "no-store");
    res.json(data);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// GET /api/health — liveness plus which log files were found.
app.get("/api/health", async (_req, res) => {
  try {
    const data = await loadAll(DEFAULT_PATHS, Date.now());
    res.json({ ok: true, sources: data.sources, paths: DEFAULT_PATHS });
  } catch (err) {
    res.status(500).json({ ok: false, error: err.message });
  }
});

app.use(express.static(join(__dirname, "public")));

app.listen(PORT, () => {
  console.log(`glockpeek-web on http://localhost:${PORT}`);
  console.log(`  reports:   ${DEFAULT_PATHS.reports}`);
  console.log(`  unblocks:  ${DEFAULT_PATHS.unblocks}`);
  console.log(`  lifecycle: ${DEFAULT_PATHS.lifecycle}`);
});
