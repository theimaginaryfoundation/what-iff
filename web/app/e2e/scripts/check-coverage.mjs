#!/usr/bin/env node
/**
 * Acceptance checks for the merged frontend e2e lcov.
 *
 * These are properties of the *merged* report, not of any one test, so they
 * cannot be Playwright assertions — they run in the merge job, right after
 * `merge-coverage.mjs`, and fail the job when the collection pipeline has
 * quietly stopped working. Every one of them corresponds to a way the pipeline
 * has actually broken during development. The per-test half of the acceptance
 * criteria lives in `e2e/tests/functional/coverage/collection.spec.ts`, which
 * also drives the navigation check below.
 *
 * Usage:
 *   node e2e/scripts/check-coverage.mjs [<lcov file>]
 */
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const APP_ROOT = 'web/app';
const appDir = path.resolve(fileURLToPath(import.meta.url), '../../..');

/** Parses lcov into `sourcePath -> {lines, hit}`. */
function parseLcov(text) {
  const records = new Map();
  let file = null;
  for (const line of text.split('\n')) {
    if (line.startsWith('SF:')) {
      file = line.slice(3).trim();
      records.set(file, { lines: 0, hit: 0 });
    } else if (file && line.startsWith('LF:')) {
      records.get(file).lines = Number(line.slice(3));
    } else if (file && line.startsWith('LH:')) {
      records.get(file).hit = Number(line.slice(3));
    } else if (line.startsWith('end_of_record')) {
      file = null;
    }
  }
  return records;
}

const checks = [
  {
    // The whole point of monocart's `all` option. Without it the denominator is
    // only what the run happened to load, and adding a lazy route nobody tests
    // would *raise* the reported percentage.
    name: 'an unvisited lazy component appears with 0 hits',
    run: (records) => {
      const unvisited = [...records].filter(([, r]) => r.lines > 5 && r.hit === 0);
      if (unvisited.length === 0) {
        return 'no file has lines but zero hits — the `all` option is not contributing untested files';
      }
      return null;
    },
  },
  {
    // `startJSCoverage({ resetOnNavigation: false })` does not accumulate across
    // a full navigation; the fixture banks and restarts around one. Driven by
    // `e2e/tests/functional/coverage/collection.spec.ts`.
    name: 'code executed before and after a full navigation both survive',
    run: (records) => {
      const before = `${APP_ROOT}/src/app/features/auth/components/login/login.component.ts`;
      const after = `${APP_ROOT}/src/app/features/auth/components/register/register.component.ts`;
      // Absent and present-but-uncovered are different failures, and the paths
      // below are hardcoded: a component that was renamed or moved shows up as
      // absent, which is a stale check rather than a broken pipeline. Say which
      // one it is, so nobody has to read this file to find out.
      const absent = [before, after].filter((f) => !records.has(f));
      if (absent.length > 0) {
        return (
          `${absent.join(' and ')} is not in the report at all — if the component was renamed or ` +
          'moved, update the paths in this check; otherwise the collection pipeline is not seeing it'
        );
      }
      const uncovered = [before, after].filter((f) => !(records.get(f).hit > 0));
      return uncovered.length > 0 ? `no hits for ${uncovered.join(' and ')}` : null;
    },
  },
  {
    // Guards against the merge silently producing a well-formed empty report,
    // which is what a broken artifact download looks like.
    name: 'the report is non-empty and rooted in the app source tree',
    run: (records) => {
      if (records.size === 0) return 'no records at all';
      const stray = [...records.keys()].filter((f) => !f.startsWith(`${APP_ROOT}/src/`));
      if (stray.length > 0) return `paths outside ${APP_ROOT}/src/: ${stray.slice(0, 5).join(', ')}`;
      const covered = [...records.values()].filter((r) => r.hit > 0).length;
      return covered === 0 ? 'not one file has a covered line' : null;
    },
  },
];

async function main() {
  const file = process.argv[2] ?? path.join(appDir, 'coverage/web-e2e/report/lcov.info');
  let text;
  try {
    text = await fs.readFile(file, 'utf8');
  } catch {
    console.error(`check-coverage: no lcov at ${file}`);
    process.exit(1);
  }

  const records = parseLcov(text);
  console.log(`check-coverage: ${records.size} files in ${file}`);

  // The one number a human actually wants off this job. monocart's
  // console-summary prints it too, but buried in a table the merge job's log
  // scrolls past — this line is greppable, so CI can lift it into the run
  // summary without re-parsing the lcov itself.
  let lines = 0;
  let hit = 0;
  for (const r of records.values()) {
    lines += r.lines;
    hit += r.hit;
  }
  const pct = lines > 0 ? ((hit / lines) * 100).toFixed(1) : '0.0';
  console.log(`check-coverage: total ${pct}% lines (${hit}/${lines})`);

  let failed = 0;
  for (const check of checks) {
    const failure = check.run(records);
    if (failure) {
      failed++;
      console.error(`  FAIL  ${check.name}: ${failure}`);
    } else {
      console.log(`  ok    ${check.name}`);
    }
  }

  if (failed > 0) {
    console.error(`check-coverage: ${failed} of ${checks.length} checks failed`);
    process.exit(1);
  }
}

await main();
