#!/usr/bin/env node
/**
 * `npm run design:review` — build the visual design review report.
 *
 * Reads baselines out of git rather than running anything, so the default
 * invocation takes about a second and needs no backend, no browser and no
 * Docker. That is the point: a report you have to schedule twenty minutes
 * for is a report nobody opens mid-iteration.
 *
 * Running the visual suite is still worth doing — it is what catches an
 * *unintended* change, where the code moved and the baseline did not — but
 * it answers a different question, and `e2e:mock-llm:visual:docker` already
 * answers it. This tool shows the changes that were made on purpose, which
 * is precisely the set that a passing visual suite renders invisible.
 */

import { writeFile, mkdir } from 'node:fs/promises';
import path from 'node:path';
import { collect } from './collect.mjs';
import { render, reportTitle } from './render.mjs';
import { renderMarkdown } from './markdown.mjs';
import { WORKTREE } from './lib/git.mjs';

const USAGE = `
Design review report — before/after of every screen the visual suite covers.

  npm run design:review [-- <options>]

Options
  --base <ref>     Compare against this ref's merge base.   (default: origin/main)
  --head <ref>     Treat this ref as "after".               (default: working tree)
  --out <path>     Where to write the report.               (default: .dev/design-review/report.html)
  --json <path>    Also write the underlying model as JSON.
  --markdown <p>   Also write a pull-request-comment summary as markdown.
  --report-link <url>  URL to link to from that summary (where the report is published).
  --open           Open the report when it is written.
  --help           Show this.

Examples
  npm run design:review                              # my uncommitted work vs main
  npm run design:review -- --head HEAD               # my branch as committed
  npm run design:review -- --base v1.4.0             # since a release
  npm run design:review -- --head abc1234 --open
  npm run design:review -- --markdown /tmp/comment.md   # paste into a PR
`;

function parseArgs(argv) {
  const options = { base: 'origin/main', head: WORKTREE, out: null, json: null, markdown: null, reportLink: null, open: false };
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    // A `--flag=value` form is accepted alongside `--flag value` because npm
    // run-script arguments get retyped constantly and both are muscle memory.
    const [flag, inlineValue] = arg.startsWith('--') && arg.includes('=') ? [arg.slice(0, arg.indexOf('=')), arg.slice(arg.indexOf('=') + 1)] : [arg, null];
    const next = () => inlineValue ?? argv[++i];
    switch (flag) {
      case '--base': options.base = next(); break;
      case '--head': options.head = next(); break;
      case '--out': options.out = next(); break;
      case '--json': options.json = next(); break;
      case '--markdown': options.markdown = next(); break;
      case '--report-link': options.reportLink = next(); break;
      case '--open': options.open = true; break;
      case '--help':
      case '-h': options.help = true; break;
      default:
        throw new Error(`Unknown option: ${arg}\n${USAGE}`);
    }
  }
  for (const key of ['base', 'head', 'out', 'json', 'markdown', 'reportLink']) {
    if (options[key] === undefined) throw new Error(`Option --${key} needs a value.\n${USAGE}`);
  }
  return options;
}

/**
 * A path to show the reader.
 *
 * Relative to the repository root, not to `process.cwd()`. These commands
 * are run from `web/app` (that is where the npm script lives) while the
 * report lands in the repo's `.dev/`, so a cwd-relative path comes out as
 * `../../.dev/design-review/report.html` — technically correct and useless
 * to paste anywhere.
 */
function display(target, root) {
  const relative = path.relative(root, target);
  return relative.startsWith('..') ? target : relative;
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  if (options.help) {
    process.stdout.write(USAGE);
    return;
  }

  const model = await collect({ cwd: process.cwd(), baseRef: options.base, headRef: options.head });

  // `.dev/` is the repo's gitignored scratch directory. A report runs to a
  // few megabytes of inlined PNGs, so writing it anywhere tracked would put
  // it one careless `git add -A` away from the history of a public repo.
  const outPath = path.resolve(options.out ?? path.join(model.repoRoot, '.dev', 'design-review', 'report.html'));
  await mkdir(path.dirname(outPath), { recursive: true });
  await writeFile(outPath, await render(model), 'utf8');

  if (options.json) {
    const jsonPath = path.resolve(options.json);
    await mkdir(path.dirname(jsonPath), { recursive: true });
    // Image buffers are dropped: the JSON exists for a CI job to assert on
    // ("did any screen change?"), and megabytes of base64 would make it
    // unreadable for the one case it is for.
    await writeFile(
      jsonPath,
      JSON.stringify(
        { ...model, screens: model.screens.map(screen => ({ ...screen, variants: screen.variants.map(({ before, after, ...rest }) => ({ ...rest, hasBefore: Boolean(before), hasAfter: Boolean(after) })) })) },
        null,
        2,
      ),
      'utf8',
    );
    process.stdout.write(`  model  ${display(jsonPath, model.repoRoot)}\n`);
  }

  if (options.markdown) {
    const markdownPath = path.resolve(options.markdown);
    await mkdir(path.dirname(markdownPath), { recursive: true });
    await writeFile(markdownPath, renderMarkdown(model, { reportLink: options.reportLink }), 'utf8');
    process.stdout.write(`  comment ${display(markdownPath, model.repoRoot)}\n`);
  }

  const { changed, added, removed, unchanged } = model.summary;
  process.stdout.write(`\n  ${reportTitle(model)}\n\n`);
  process.stdout.write(`  ${changed} changed · ${added} new · ${removed} removed · ${unchanged} unchanged\n`);
  process.stdout.write(`  report ${display(outPath, model.repoRoot)}\n`);
  if (!model.base.sha) {
    process.stdout.write(`\n  No merge base with ${model.base.ref} — the report shows the current state with nothing to compare against.\n`);
  } else if (changed + added + removed === 0) {
    process.stdout.write(`\n  No baseline changed. If you expected one to, the screen may not have a visual spec yet.\n`);
  }
  process.stdout.write('\n');

  if (options.open) {
    const { execFile } = await import('node:child_process');
    const opener = process.platform === 'darwin' ? 'open' : process.platform === 'win32' ? 'start' : 'xdg-open';
    execFile(opener, [outPath], () => {});
  }
}

main().catch(error => {
  process.stderr.write(`\n  ${error.message}\n\n`);
  process.exitCode = 1;
});
