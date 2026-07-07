// Tests for the usage-rules/config store. Run with: node --test
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, rm, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { sanitizeRules, sanitizeColors, loadConfig, saveConfig } from "./rules.js";

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

test("sanitizeColors keeps #rrggbb entries and drops the rest", () => {
  assert.deepEqual(
    sanitizeColors({
      "Activity:work": "#F0A020", // upper-cased -> normalized
      "Activity:games": "#7bc86c",
      "Bad:short": "#fff", // not 6 digits -> dropped
      "Bad:named": "red", // not hex -> dropped
      "": "#000000", // empty key -> dropped
      "Bad:num": 123, // non-string -> dropped
    }),
    { "Activity:work": "#f0a020", "Activity:games": "#7bc86c" }
  );
  assert.deepEqual(sanitizeColors(null), {});
  assert.deepEqual(sanitizeColors(["#ffffff"]), {});
});

test("loadConfig returns empty config when the file is missing", async () => {
  assert.deepEqual(await loadConfig("/no/such/rules.json"), { rules: [], colors: {} });
});

test("saveConfig writes sanitized rules+colors and loadConfig round-trips them", async () => {
  const dir = await mkdtemp(join(tmpdir(), "glockrules-"));
  const path = join(dir, "usage-rules.json");
  try {
    const saved = await saveConfig(
      {
        rules: [
          { program: "firefox", title: "YouTube", tag: "Activity:leisure" },
          { junk: true }, // dropped
        ],
        colors: { "Activity:leisure": "#c0468f", "Bad:x": "nope" },
      },
      path
    );
    assert.deepEqual(saved.rules, [{ program: "firefox", title: "YouTube", tag: "Activity:leisure" }]);
    assert.deepEqual(saved.colors, { "Activity:leisure": "#c0468f" });

    // On disk it is the { rules, colors } envelope.
    const onDisk = JSON.parse(await readFile(path, "utf8"));
    assert.deepEqual(onDisk, {
      rules: [{ program: "firefox", title: "YouTube", tag: "Activity:leisure" }],
      colors: { "Activity:leisure": "#c0468f" },
    });

    assert.deepEqual(await loadConfig(path), saved);
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});

test("loadConfig accepts a legacy bare-array rules file", async () => {
  const dir = await mkdtemp(join(tmpdir(), "glockrules-"));
  const path = join(dir, "usage-rules.json");
  try {
    await writeFile(path, JSON.stringify([{ program: "", title: "", tag: "A:b" }]));
    assert.deepEqual(await loadConfig(path), {
      rules: [{ program: "", title: "", tag: "A:b" }],
      colors: {},
    });
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
});
