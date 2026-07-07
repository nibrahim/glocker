// Persistence for usage categorization rules. Rules live in their own JSON
// file — the raw usage log is never modified. Each rule is a simple triple:
//
//   { program: "<regex>", title: "<regex>", tag: "Category:Value" }
//
// program/title are optional regex strings (empty = match anything); the first
// rule whose program AND title both match a sample wins, arbtt-style. The
// matching itself happens client-side (see public/app.js); this module only
// loads, validates, and saves the rule list.
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { dirname } from "node:path";

// Default store path; overridable so tests and alternate deployments can point
// elsewhere. Kept out of public/ so it is never served as a static asset.
export const DEFAULT_RULES_PATH =
  process.env.GLOCKER_USAGE_RULES || new URL("../usage-rules.json", import.meta.url).pathname;

const str = (v) => (typeof v === "string" ? v : "");

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

export async function loadRules(path = DEFAULT_RULES_PATH) {
  let text;
  try {
    text = await readFile(path, "utf8");
  } catch (err) {
    if (err.code === "ENOENT") return []; // no rules yet is fine
    throw err;
  }
  let parsed;
  try {
    parsed = JSON.parse(text);
  } catch {
    return []; // treat a corrupt file as empty rather than 500ing the UI
  }
  // Accept either a bare array or { rules: [...] }.
  return sanitizeRules(Array.isArray(parsed) ? parsed : parsed.rules);
}

export async function saveRules(rules, path = DEFAULT_RULES_PATH) {
  const clean = sanitizeRules(rules);
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, JSON.stringify(clean, null, 2) + "\n", "utf8");
  return clean;
}
