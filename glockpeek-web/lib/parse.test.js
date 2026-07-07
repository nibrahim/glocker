// Tests for the log parsers. Run with: node --test
import { test } from "node:test";
import assert from "node:assert/strict";
import { writeFile, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  parseReports,
  parseUnblocks,
  parseLifecycle,
  unmanagedPeriods,
} from "./parse.js";

async function withFile(name, contents, fn) {
  const dir = await mkdtemp(join(tmpdir(), "glockpeek-"));
  const path = join(dir, name);
  await writeFile(path, contents);
  try {
    return await fn(path);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
}

test("parseReports reads keyword, url and explicit domain", async () => {
  const log = [
    "[2025-11-17 22:51:59] | content-keyword:boobs | https://xhamster44.desi/videos/x | xhamster44.desi",
    "garbage line that should be skipped",
    "[2025-11-17 15:35:46] | url-keyword:porn | https://www.google.com/search?q=porn",
  ].join("\n");
  await withFile("r.log", log, async (path) => {
    const { available, entries } = await parseReports(path);
    assert.equal(available, true);
    assert.equal(entries.length, 2);
    // sorted ascending by time -> the url-keyword (15:35) comes first
    assert.equal(entries[0].type, "url-keyword");
    assert.equal(entries[0].keyword, "porn");
    assert.equal(entries[0].domain, "www.google.com"); // derived from URL
    assert.equal(entries[1].keyword, "boobs");
    assert.equal(entries[1].domain, "xhamster44.desi"); // explicit column
  });
});

test("missing log file reports unavailable, not an error", async () => {
  const { available, entries } = await parseReports("/no/such/file.log");
  assert.equal(available, false);
  assert.deepEqual(entries, []);
});

test("parseUnblocks parses JSON lines", async () => {
  const log =
    '{"unblock_time":"2025-12-05T13:48:24+05:30","restore_time":"2025-12-05T14:18:24+05:30","reason":"work","domain":"youtube.com"}\n';
  await withFile("u.log", log, async (path) => {
    const { entries } = await parseUnblocks(path);
    assert.equal(entries.length, 1);
    assert.equal(entries[0].reason, "work");
    assert.equal(entries[0].domain, "youtube.com");
    assert.ok(entries[0].restoreTs > entries[0].ts);
  });
});

test("unmanagedPeriods pairs uninstall->install and skips short upgrades", async () => {
  const log = [
    // a real ~1 day exposure
    '{"timestamp":"2026-02-11T14:00:00+05:30","type":"uninstall","reason":"temp"}',
    '{"timestamp":"2026-02-12T14:00:00+05:30","type":"install"}',
    // a 30-second upgrade -> ignored
    '{"timestamp":"2026-03-01T10:00:00+05:30","type":"uninstall","reason":"maintenance","note":"upgrade"}',
    '{"timestamp":"2026-03-01T10:00:30+05:30","type":"install"}',
  ].join("\n");
  await withFile("l.log", log, async (path) => {
    const { entries } = await parseLifecycle(path);
    const periods = unmanagedPeriods(entries);
    assert.equal(periods.length, 1);
    assert.equal(periods[0].reason, "temp");
    assert.equal(periods[0].open, false);
  });
});

test("unmanagedPeriods leaves an open span when still uninstalled", async () => {
  const log = '{"timestamp":"2026-06-01T00:00:00+05:30","type":"uninstall","reason":"gone"}';
  await withFile("l.log", log, async (path) => {
    const { entries } = await parseLifecycle(path);
    const now = Date.parse("2026-06-02T00:00:00+05:30");
    const periods = unmanagedPeriods(entries, now);
    assert.equal(periods.length, 1);
    assert.equal(periods[0].open, true);
    assert.equal(periods[0].end, now);
  });
});
