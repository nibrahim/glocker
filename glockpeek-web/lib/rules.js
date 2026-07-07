// Persistence for usage categorization config: the rule list plus a per-tag
// colour map. Stored in its own JSON file — the raw usage log is never touched.
//
//   {
//     "rules":  [ { "program": "<regex>", "title": "<regex>", "tag": "Cat:Val" } ],
//     "colors": { "Cat:Val": "#rrggbb" }
//   }
//
// program/title are optional regex strings (empty = match anything); the first
// rule whose program AND title both match a sample wins, arbtt-style. Matching
// and colouring happen client-side (see public/app.js); this module only loads,
// validates, and saves the config.
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { dirname } from "node:path";

// Default store path; overridable so tests and alternate deployments can point
// elsewhere. Kept out of public/ so it is never served as a static asset.
export const DEFAULT_RULES_PATH =
  process.env.GLOCKER_USAGE_RULES || new URL("../usage-rules.json", import.meta.url).pathname;

const str = (v) => (typeof v === "string" ? v : "");
const HEX = /^#[0-9a-fA-F]{6}$/;

// Coerce arbitrary client input into a clean array of rules. Anything that is
// not an object, or a rule with no tag, is dropped — a malformed request can
// never corrupt the store.
export function sanitizeRules(input) {
  if (!Array.isArray(input)) return [];
  const out = [];
  for (const r of input) {
    if (!r || typeof r !== "object") continue;
    const tag = str(r.tag).trim();
    if (!tag) continue; // a rule with no tag does nothing
    out.push({
      program: str(r.program).trim(),
      title: str(r.title).trim(),
      tag,
    });
  }
  return out;
}

// Keep only string tag -> #rrggbb entries; drop anything malformed.
export function sanitizeColors(input) {
  if (!input || typeof input !== "object" || Array.isArray(input)) return {};
  const out = {};
  for (const [k, v] of Object.entries(input)) {
    if (typeof k === "string" && k.trim() && typeof v === "string" && HEX.test(v)) {
      out[k] = v.toLowerCase();
    }
  }
  return out;
}

// Read the config, tolerating a missing/corrupt file and the legacy formats
// (a bare rules array, or { rules: [...] } with no colours).
export async function loadConfig(path = DEFAULT_RULES_PATH) {
  let text;
  try {
    text = await readFile(path, "utf8");
  } catch (err) {
    if (err.code === "ENOENT") return { rules: [], colors: {} };
    throw err;
  }
  let parsed;
  try {
    parsed = JSON.parse(text);
  } catch {
    return { rules: [], colors: {} }; // corrupt file -> empty, don't 500 the UI
  }
  if (Array.isArray(parsed)) return { rules: sanitizeRules(parsed), colors: {} };
  return {
    rules: sanitizeRules(parsed.rules),
    colors: sanitizeColors(parsed.colors),
  };
}

export async function saveConfig(input, path = DEFAULT_RULES_PATH) {
  const clean = {
    rules: sanitizeRules(input && input.rules),
    colors: sanitizeColors(input && input.colors),
  };
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, JSON.stringify(clean, null, 2) + "\n", "utf8");
  return clean;
}
