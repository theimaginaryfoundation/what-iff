#!/usr/bin/env node
// Writes public/version.json — the frontend's build provenance, the
// counterpart of the API's GET /api/version. Served as a static asset at
// /version.json so a deployment can be asked "what build is this?" without
// any JavaScript running, and a deployed-test preflight can compare it with
// what it expects before a single test runs.
//
// Runs automatically before every `npm run build*`, `npm start`, `npm run
// serve:coverage`, and `npm run watch` (the pre-scripts in package.json —
// every entry point that serves or builds this app needs one). Values come
// from the environment when a pipeline sets them — VERSION, COMMIT,
// BUILT_AT, and OVERLAY_COMMIT for a downstream build that composes more
// source on top of this tree — and fall back to git / "dev" / now, so a
// local build still produces a truthful file. BUILT_AT must be RFC 3339 /
// ISO 8601 (e.g. 2026-08-21T00:00:00Z) if supplied — it's rejected otherwise
// so a broken pipeline value can't silently corrupt provenance.
// The output is gitignored: it is derived, never hand-edited.
//
// The field set (version/commit/built_at/overlay_commit) mirrors the API's
// GET /api/version response (see internal/handlers/version and
// openapi.yaml) — keep the two in sync; api-tests/version.spec.ts and
// e2e/tests/functional/nav/version-json.spec.ts both assert this shape.
import { execSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const out = join(here, "..", "public", "version.json");

// Env values come from wherever a build pipeline is configured, so treat
// them as untrusted input rather than assuming a well-formed short string.
const MAX_ENV_VALUE_LENGTH = 512;

function errorMessage(err) {
  return err instanceof Error ? err.message : String(err);
}

function readEnv(name) {
  const raw = process.env[name];
  if (raw === undefined) return undefined;
  const trimmed = String(raw).trim().slice(0, MAX_ENV_VALUE_LENGTH);
  return trimmed || undefined;
}

function gitHead() {
  try {
    return execSync("git rev-parse HEAD", { cwd: here, stdio: ["ignore", "pipe", "ignore"] })
      .toString()
      .trim();
  } catch (err) {
    console.warn(`write-version: git rev-parse HEAD failed, falling back to 'unknown': ${errorMessage(err)}`);
    return "unknown";
  }
}

function builtAt() {
  const supplied = readEnv("BUILT_AT");
  if (supplied === undefined) return new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
  if (Number.isNaN(Date.parse(supplied))) {
    console.warn(`write-version: BUILT_AT="${supplied}" is not a parseable timestamp; falling back to now`);
    return new Date().toISOString().replace(/\.\d{3}Z$/, "Z");
  }
  return supplied;
}

const info = {
  version: readEnv("VERSION") || "dev",
  commit: readEnv("COMMIT") || gitHead(),
  built_at: builtAt(),
};
const overlayCommit = readEnv("OVERLAY_COMMIT");
if (overlayCommit) {
  info.overlay_commit = overlayCommit;
}

try {
  mkdirSync(dirname(out), { recursive: true });
  writeFileSync(out, JSON.stringify(info, null, 2) + "\n");
} catch (err) {
  // This runs as an npm pre-hook, so a thrown error here otherwise surfaces
  // as an opaque "process exited early" from whatever it's a pre-hook for
  // (npm start, ng build, Playwright's webServer) with the real cause buried
  // in a nested log.
  console.error(`write-version: could not write ${out} — /version.json will 404: ${errorMessage(err)}`);
  throw err;
}

const shortRev = value => (value === "unknown" ? "unknown" : value.slice(0, 12));
console.log(
  `wrote ${out}: ${info.version} (${shortRev(info.commit)}${info.overlay_commit ? `, overlay ${shortRev(info.overlay_commit)}` : ""})`,
);
