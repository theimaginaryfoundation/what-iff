#!/usr/bin/env node
/**
 * Turns a Playwright JSON report into (a) a machine-readable per-suite
 * summary and (b) the markdown section of the sticky E2E status comment.
 *
 *   node report-summary.mjs summary <report.json>
 *     Emits JSON: { sha, run_url, event, duration, suites: { chrome: {...} } }.
 *     Main-branch CI runs upload this as the `e2e-summary` artifact so PR
 *     runs can show a "vs main" column.
 *
 *   node report-summary.mjs comment <report.json>
 *     Emits markdown for .github/actions/e2e-status-comment. Reads env:
 *       TITLE          section heading, e.g. "E2E · mock LLM"
 *       SHA, SHA_URL   commit actually tested (PR head), linked if URL given
 *       RUN_URL        the Actions run
 *       REPORT_URL     direct artifact URL of the HTML report (optional)
 *       BASELINE_FILE  path to a main-run `summary` JSON (optional)
 *       NOTE_<SUITE>   note shown for a suite with no results, e.g.
 *                      NOTE_MOBILE="label `e2e-full`" (optional)
 *       SUITES         comma-separated rows to always render, in order
 *                      (default "chrome,mobile,visual")
 *
 * Suites are buckets, not Playwright projects: the desktop and mobile
 * projects each run both functional and visual specs, and the comment wants
 * "visual" as its own row. Classification:
 *   - project `setup` is auth bootstrap, not a result — dropped
 *   - specs under tests/visual/ (recorded relative to testDir as visual/...)
 *     -> visual, whatever project ran them
 *   - chromium-desktop -> chrome; chromium-mobile / webkit-mobile -> mobile
 *   - anything else keeps its project name so a new project cannot silently
 *     vanish from the comment
 */
import { readFileSync } from "node:fs";

const MOBILE_PROJECTS = new Set(["chromium-mobile", "webkit-mobile"]);
const VISUAL_FILE = /(^|[\\/])visual[\\/]/;

function bucketOf(file, projectName) {
  if (VISUAL_FILE.test(file)) return "visual";
  if (projectName === "chromium-desktop") return "chrome";
  if (MOBILE_PROJECTS.has(projectName)) return "mobile";
  return projectName || "unknown";
}

function collect(report) {
  const suites = {};
  const failures = [];
  const visit = (suite, file) => {
    const here = suite.file || file;
    for (const spec of suite.specs ?? []) {
      const specFile = spec.file || here || "";
      for (const test of spec.tests ?? []) {
        if (test.projectName === "setup") continue;
        const bucket = bucketOf(specFile, test.projectName);
        const s = (suites[bucket] ??= { passed: 0, failed: 0, flaky: 0, skipped: 0 });
        switch (test.status) {
          case "expected":
            s.passed++;
            break;
          case "unexpected":
            s.failed++;
            failures.push(`${spec.title} (${test.projectName})`);
            break;
          case "flaky":
            s.flaky++;
            break;
          default:
            s.skipped++;
        }
      }
    }
    for (const child of suite.suites ?? []) visit(child, here);
  };
  for (const suite of report.suites ?? []) visit(suite, suite.file);
  return { suites, failures };
}

// "✅ 96/96" — passed (flaky counts: it did pass) over everything that ran.
// Skipped tests are excluded from the denominator; they show in the header.
function cell(s) {
  if (!s) return null;
  const ran = s.passed + s.failed + s.flaky;
  const icon = s.failed ? "❌" : s.flaky ? "⚠️" : "✅";
  return `${icon} ${s.passed + s.flaky}/${ran}${s.flaky ? ` (${s.flaky} flaky)` : ""}`;
}

function baselineCell(s) {
  if (!s) return "–";
  const ran = s.passed + s.failed + s.flaky;
  return `${s.passed + s.flaky}/${ran}${s.failed ? " ❌" : ""}`;
}

function shortSha(sha) {
  return (sha || "").slice(0, 7);
}

function renderComment(report, env) {
  const { suites, failures } = collect(report);
  const stats = report.stats ?? {};
  const duration = Math.floor((stats.duration ?? 0) / 1000);

  let baseline = null;
  if (env.BASELINE_FILE) {
    try {
      baseline = JSON.parse(readFileSync(env.BASELINE_FILE, "utf8"));
    } catch {
      /* no baseline: column shows – */
    }
  }

  const rows = (env.SUITES || "chrome,mobile,visual")
    .split(",")
    .map(s => s.trim())
    .filter(Boolean);
  // A bucket the run produced but the row list doesn't name still gets a row.
  for (const name of Object.keys(suites)) if (!rows.includes(name)) rows.push(name);

  const icon = stats.unexpected ? "❌" : stats.flaky ? "⚠️" : "✅";
  const out = [];
  out.push(`### ${env.TITLE || "E2E"} ${icon}`);
  out.push("");

  const headBits = [];
  if (stats.unexpected) headBits.push(`**${stats.unexpected} failed**, ${stats.expected} passed`);
  else headBits.push(`**${stats.expected} passed**`);
  if (stats.flaky) headBits.push(`${stats.flaky} flaky`);
  if (stats.skipped) headBits.push(`${stats.skipped} skipped`);
  let head = `${headBits.join(", ")} · ${duration}s`;
  if (env.SHA) {
    const sha = shortSha(env.SHA);
    head += ` · ${env.SHA_URL ? `[\`${sha}\`](${env.SHA_URL})` : `\`${sha}\``}`;
  }
  out.push(head);
  out.push("");

  const hasBaseline = !!baseline?.suites;
  out.push(hasBaseline ? "| Suite | This PR | `main` |" : "| Suite | This PR |");
  out.push(hasBaseline ? "|---|---|---|" : "|---|---|");
  for (const name of rows) {
    const mine = cell(suites[name]) ?? `– ${env[`NOTE_${name.toUpperCase().replace(/\W/g, "_")}`] || "not run"}`;
    out.push(hasBaseline ? `| ${name} | ${mine} | ${baselineCell(baseline.suites[name])} |` : `| ${name} | ${mine} |`);
  }
  out.push("");

  if (failures.length) {
    out.push("<details><summary>Failed tests</summary>");
    out.push("");
    for (const f of failures) out.push(`- ${f}`);
    out.push("");
    out.push("</details>");
    out.push("");
  }

  const links = [];
  links.push(`[HTML report](${env.REPORT_URL || `${env.RUN_URL}#artifacts`})`);
  if (env.RUN_URL) links.push(`[run](${env.RUN_URL})`);
  if (hasBaseline && baseline.sha) {
    const ref = baseline.run_url ? `[\`${shortSha(baseline.sha)}\`](${baseline.run_url})` : `\`${shortSha(baseline.sha)}\``;
    links.push(`main baseline: ${ref}${baseline.event === "schedule" ? " (nightly)" : ""}`);
  } else {
    links.push("main baseline: none yet");
  }
  out.push(links.join(" · "));
  return out.join("\n") + "\n";
}

const [mode, reportPath] = process.argv.slice(2);
if (!mode || !reportPath) {
  console.error("usage: report-summary.mjs summary|comment <report.json>");
  process.exit(2);
}
const report = JSON.parse(readFileSync(reportPath, "utf8"));

if (mode === "summary") {
  const { suites } = collect(report);
  process.stdout.write(
    JSON.stringify(
      {
        sha: process.env.SHA || null,
        run_url: process.env.RUN_URL || null,
        event: process.env.EVENT || null,
        duration: Math.floor((report.stats?.duration ?? 0) / 1000),
        suites,
      },
      null,
      2,
    ) + "\n",
  );
} else if (mode === "comment") {
  process.stdout.write(renderComment(report, process.env));
} else {
  console.error(`unknown mode: ${mode}`);
  process.exit(2);
}
