#!/usr/bin/env node
/**
 * Turns the raw V8 coverage the Playwright shards collected into a single
 * `lcov.info` for Codecov's `web-e2e` flag.
 *
 * The shards only collect (see `e2e/fixtures/coverage.ts`); all the analysis
 * happens here, once, over every shard's output at the same time. That is what
 * makes the merged number mean "covered by the suite" rather than "covered by
 * shard 2".
 *
 * Usage:
 *   node e2e/scripts/merge-coverage.mjs [<input-dir> ...] [--out <dir>]
 *                                       [--sourcemaps <dir>]
 *
 * Each input dir is a collection root holding `raw/` and `sources/` — either
 * the local `coverage/web-e2e`, or one downloaded artifact per shard. Defaults
 * to `coverage/web-e2e`, output defaults to `coverage/web-e2e/report`.
 *
 * `--sourcemaps` points at an unpacked `sourcemaps-<app>-<env>-<sha>` artifact
 * from a deploy run (a `manifest.json` plus one `.map` per bundle). Deployed builds
 * use `sourceMap: { hidden: true }`, which strips the `//# sourceMappingURL`
 * comment, so the shards have nothing to resolve at collection time and the
 * maps have to be attached here instead. In this mode an entry that still has
 * no map is a hard error rather than a dropped runtime helper: against a
 * deployed build every bundle is mapped, so an unmapped one means the artifact
 * does not match the build that was measured.
 */
import fs from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import { gunzip as gunzipCallback } from 'node:zlib';
import { transform } from 'esbuild';
import { CoverageReport } from 'monocart-coverage-reports';

const gunzip = promisify(gunzipCallback);

/** Repo-relative prefix every reported path gets, so Codecov can find the file. */
const APP_ROOT = 'web/app';

const appDir = path.resolve(fileURLToPath(import.meta.url), '../../..');

function parseArgs(argv) {
  const inputs = [];
  let out;
  let sourcemaps;
  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === '--out') {
      out = argv[++i];
    } else if (argv[i] === '--sourcemaps') {
      sourcemaps = argv[++i];
    } else {
      inputs.push(argv[i]);
    }
  }
  return {
    inputs: inputs.length > 0 ? inputs : [path.join(appDir, 'coverage/web-e2e')],
    out: out ?? path.join(appDir, 'coverage/web-e2e/report'),
    sourcemaps,
  };
}

async function readJsonGz(file) {
  return JSON.parse((await gunzip(await fs.readFile(file))).toString('utf8'));
}

async function listDir(dir) {
  try {
    return await fs.readdir(dir);
  } catch {
    return [];
  }
}

/**
 * Rebuilds the hash → `{source, sourceMap}` table the shards wrote.
 *
 * Shards store each script once and reference it by content hash, so the same
 * hash appears in every input dir; first one wins, and the rest are skipped
 * without being read.
 */
async function loadSources(inputs) {
  const sources = new Map();
  for (const input of inputs) {
    const dir = path.join(input, 'sources');
    for (const file of await listDir(dir)) {
      const hash = file.replace(/\.json\.gz$/, '');
      if (sources.has(hash)) continue;
      sources.set(hash, await readJsonGz(path.join(dir, file)));
    }
  }
  return sources;
}

/**
 * Loads a deploy run's sourcemap artifact into a `bundle filename → map` lookup.
 *
 * Keyed off the manifest rather than off what happens to be in the directory:
 * the deploy workflow writes the manifest from the maps it actually moved out
 * of the publish tree, so a mismatch between the two shows up here instead of
 * as a quietly unmapped bundle. Maps are read lazily — a deployed build ships
 * tens of megabytes of them and a run touches only the bundles it loaded.
 */
async function loadSourceMaps(dir) {
  const manifestPath = path.join(dir, 'manifest.json');
  let manifest;
  try {
    manifest = JSON.parse(await fs.readFile(manifestPath, 'utf8'));
  } catch (err) {
    console.error(`merge-coverage: could not read ${manifestPath}: ${err.message}`);
    process.exit(1);
  }
  const maps = manifest?.maps;
  if (!maps || Object.keys(maps).length === 0) {
    console.error(`merge-coverage: ${manifestPath} lists no sourcemaps`);
    process.exit(1);
  }

  const cache = new Map();
  return {
    sha: manifest.sha,
    size: Object.keys(maps).length,
    async get(bundle) {
      if (!Object.hasOwn(maps, bundle)) return undefined;
      if (!cache.has(bundle)) {
        cache.set(bundle, JSON.parse(await fs.readFile(path.join(dir, maps[bundle]), 'utf8')));
      }
      return cache.get(bundle);
    },
  };
}

/** The bundle filename an entry was served as, or `''` for a non-URL entry. */
function bundleName(url) {
  try {
    return path.basename(new URL(url).pathname);
  } catch {
    return '';
  }
}

/**
 * Compiles a source file the suite never loaded, so it counts as 0% instead of
 * disappearing from the denominator.
 *
 * Monocart parses entries with acorn, which cannot read TypeScript — without
 * this every untested file would be skipped, and the app lazy-loads most of its
 * routes (`src/app/app.routes.ts`), so "untested" is the majority of them. The
 * sourcemap is what ties the compiled output back to the original lines;
 * `sourcefile` is app-relative so the reported paths match the runtime entries'.
 */
async function transformUntestedFile(entry) {
  const sourcefile = path.relative(appDir, fileURLToPath(entry.url));
  const { code, map } = await transform(entry.source, {
    loader: 'ts',
    format: 'esm',
    sourcefile,
    sourcemap: true,
    // Without this esbuild leaves `@Component({...})` as standard-decorator
    // syntax, which monocart's acorn parser cannot read — every decorated
    // class silently collapsed to a one-line file. Downlevelling them to
    // `__decorateClass` calls keeps the output parseable.
    tsconfigRaw: { compilerOptions: { experimentalDecorators: true } },
  });
  entry.source = code;
  entry.sourceMap = JSON.parse(map);
}

async function main() {
  const { inputs, out, sourcemaps } = parseArgs(process.argv.slice(2));

  const sources = await loadSources(inputs);
  if (sources.size === 0) {
    console.error(`No coverage sources found under: ${inputs.join(', ')}`);
    process.exit(1);
  }

  const maps = sourcemaps ? await loadSourceMaps(sourcemaps) : null;
  if (maps) {
    console.log(`merge-coverage: ${maps.size} sourcemaps for build ${maps.sha} from ${sourcemaps}`);
  }

  const report = new CoverageReport({
    // The deployed name carries the caveat with the report, because the report
    // is the artifact someone opens weeks later with no run log next to it:
    // these bundles are minified, so several statements share a line and a
    // branch count is a property of the optimised output, not of the source.
    // Line coverage is the number to read; the rest is coarser than the
    // dev-server run's and is not comparable to it.
    name: maps
      ? `Personal Assistant deployed E2E coverage — build ${maps.sha.slice(0, 12)} (minified bundles: line % is the comparable metric)`
      : 'Personal Assistant E2E coverage',
    outputDir: out,
    reports: [['lcovonly', { file: 'lcov.info' }], ['v8'], ['console-summary']],
    // Paths are rewritten to repo-relative here rather than at collection time:
    // the shards see only URLs, and Codecov matches on paths from the repo root.
    sourcePath: (filePath) => (filePath.startsWith(`${APP_ROOT}/`) ? filePath : `${APP_ROOT}/${filePath}`),
    // First-party TypeScript only. A sourcemap also names templates, styles, and
    // anything the build pulled in from `node_modules`; none of those are code
    // this suite is meant to be measuring.
    sourceFilter: (sourcePath) => sourcePath.startsWith(`${APP_ROOT}/src/`) && sourcePath.endsWith('.ts'),
    // Only mapped entries are unpacked into sources and reach `sourceFilter`;
    // an entry without a map is reported as-is, under a synthesised path like
    // `localhost-4200/chunk-XXXX.js`. Those are esbuild's runtime helpers, not
    // first-party code. Dropped here rather than at collection time, so the
    // deployed-coverage job can attach hidden sourcemaps to entries later.
    entryFilter: (entry) => Boolean(entry.sourceMap),
    // Every first-party source, not just the ones this run happened to load.
    all: {
      dir: [path.join(appDir, 'src')],
      // A function, not the glob form monocart also accepts: it hands the
      // filter an absolute path, and minimatch's `**` skips any path with a
      // dot-directory in it — which silently matched nothing at all from a
      // checkout under `.claude/worktrees/`.
      filter: (filePath) => filePath.endsWith('.ts') && !/\.(spec|d)\.ts$/.test(filePath),
      transformer: transformUntestedFile,
    },
    logging: 'info',
    clean: true,
  });

  let tests = 0;
  let entries = 0;
  /** Documents the fixture saw replaced before it could bank their coverage. */
  let lostNavigations = 0;
  /** Bundles the deployed run measured but could not map, reported once each. */
  const unresolved = new Set();
  for (const input of inputs) {
    const dir = path.join(input, 'raw');
    for (const file of await listDir(dir)) {
      const { entries: raw, lostNavigations: lost } = await readJsonGz(path.join(dir, file));
      // Summed before the `continue` below: a test whose entries all failed to
      // rehydrate still lost whatever navigations it lost.
      lostNavigations += lost ?? 0;
      const rehydrated = (
        await Promise.all(
          raw.map(async (entry) => {
            const stored = sources.get(entry.sourceHash);
            if (!stored) {
              console.warn(`merge-coverage: no stored source for ${entry.url} (${entry.sourceHash})`);
              return null;
            }
            const rehydratedEntry = { url: entry.url, scriptId: entry.scriptId, functions: entry.functions, ...stored };
            // Only the deployed path needs this: against the dev server the
            // shards resolved the map themselves from `//# sourceMappingURL`.
            if (maps && !rehydratedEntry.sourceMap) {
              const bundle = bundleName(entry.url);
              const map = await maps.get(bundle);
              if (map) {
                rehydratedEntry.sourceMap = map;
              } else {
                unresolved.add(bundle || entry.url);
              }
            }
            return rehydratedEntry;
          }),
        )
      ).filter(Boolean);
      if (rehydrated.length === 0) continue;
      await report.add(rehydrated);
      tests++;
      entries += rehydrated.length;
    }
  }

  console.log(`merge-coverage: merged ${entries} entries from ${tests} tests`);
  // Printed unconditionally, including the zero. This is the only signal that
  // distinguishes "the suite covers 38% of the app" from "the suite covers more
  // than that and the pipeline threw the difference away" — a document replaced
  // before its counts were taken looks exactly like a document never visited,
  // so without this number an undercount is invisible.
  console.log(`merge-coverage: lost navigations: ${lostNavigations}`);
  if (lostNavigations > 0) {
    console.warn(
      `merge-coverage: ${lostNavigations} navigation(s) were not intercepted; the reported ` +
        'percentage is an undercount. See `watchNavigations` in e2e/fixtures/coverage.ts.',
    );
  }
  const results = await report.generate();
  if (!results) {
    console.error('merge-coverage: monocart produced no report');
    process.exit(1);
  }
  console.log(`merge-coverage: lcov at ${path.join(out, 'lcov.info')}`);

  if (maps) {
    // Published unconditionally, including the happy-path zero: a deployed
    // report whose number quietly rests on half the bundles being unmapped
    // looks exactly like a real one, and the count is what distinguishes them.
    console.log(`merge-coverage: unresolved entries: ${unresolved.size}`);
    if (unresolved.size > 0) {
      console.error(
        `merge-coverage: ${unresolved.size} measured bundle(s) had no sourcemap in the artifact for ${maps.sha} — ` +
          'the deployed build and the downloaded sourcemaps are out of step:\n  ' +
          [...unresolved].sort().join('\n  '),
      );
      process.exit(1);
    }
    // `sourceFilter` already drops anything outside `src/`, so a report with no
    // files at all is the shape a wrong-but-loadable artifact takes: maps that
    // resolve, to sources belonging to some other tree.
    const files = results.files ?? [];
    const inSrc = files.filter((f) => f.sourcePath?.startsWith(`${APP_ROOT}/src/`));
    if (inSrc.length === 0) {
      console.error(`merge-coverage: no files under ${APP_ROOT}/src/ survived — sourcemaps resolved to another tree`);
      process.exit(1);
    }
  }
}

await main();
