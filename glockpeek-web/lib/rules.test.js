// Tests for the usage-rules store. Run with: node --test
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, rm, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { sanitizeRules, loadRules, saveRules } from "./rules.js";

test("sanitizeRules keeps valid triples and drops junk", () => {
  const out = sanitizeRules([
    { program: "^Emacs$", title: "glocker", tag: "Project:glocker" },
    { tag: "Activity:idle" }, // program/title default to ""
    { program: "x", title: "y" }, // no tag -> dropped
    "not an object", // dropped
    null, // dropped
    { program: 5, title: {}, tag: "Weird:1" }, // non-strings coerced to ""
  ]);
  assert.deepEqual(out, [
    { program: "^Emacs$", title: "glocker", tag: "Project:glocker" },
    { program: "", title: "", tag: "Activity:idle" },
    { program: "", title: "", tag: "Weird:1" },
  ]);
});

test("sanitizeRules returns [] for non-array input", () => {
  assert.deepEqual(sanitizeRules(null), []);
  assert.deepEqual(sanitizeRules({}), []);
});

test("loadRules returns [] when the file is missing", async () => {
  assert.deepEqual(await loadRules("/no/such/rules.json"), []);
});

test("saveRules writes sanitized JSON and loadRules round-trips it", async () => {
  const dir = await mkdtemp(join(tmpdir(), "glockrules-"));
  const path = join(dir, "usage-rules.json");
  try {
    const saved = await saveRules(
      [
        { program: "firefox", title: "YouTube", tag: "Activity:leisure" },
        { junk: true }, // dropped on save
      ],
      path
    );
    assert.equal(saved.length, 1);

    // On disk it is a bare array of clean triples.
    const onDisk = JSON.parse(await readFile(path, "utf8"));
    assert.deepEqual(onDisk, [{ program: "firefox", title: "YouTube", tag: "Activity:leisure" }]);

    assert.deepEqual(await loadRules(path), saved);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test("loadRules accepts the { rules: [...] } envelope too", async () => {
  const dir = await mkdtemp(join(tmpdir(), "glockrules-"));
  const path = join(dir, "usage-rules.json");
  try {
    await saveRules([{ program: "", title: "", tag: "A:b" }], path);
    // Rewrite as an envelope shape and confirm loadRules unwraps it.
    const { writeFile } = await import("node:fs/promises");
    await writeFile(path, JSON.stringify({ rules: [{ tag: "C:d" }] }));
    assert.deepEqual(await loadRules(path), [{ program: "", title: "", tag: "C:d" }]);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});
