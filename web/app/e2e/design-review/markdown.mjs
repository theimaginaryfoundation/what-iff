/**
 * The model as a pull request comment.
 *
 * A third rendering of the same `collect()` output, alongside the HTML report
 * and `--json`. The point of that shape is this file: the comment and the
 * report a reader opens from it cannot disagree about which screens changed,
 * because neither works it out for itself.
 *
 * Written for someone scanning a PR page, not reading a report. It says what
 * changed and by how much, names the thing nothing is showing them, and
 * stops. Everything else is behind the link.
 */

import path from 'node:path';

/** `11.2%`, or `0.04%` for a change small enough that one decimal reads as zero. */
function percent(ratio) {
  if (!ratio) return '0%';
  return `${(ratio * 100).toFixed(ratio < 0.001 ? 3 : ratio < 0.01 ? 2 : 1)}%`;
}

/** What a single viewport cell says. */
function cell(variant) {
  switch (variant.status) {
    case 'changed':
      // A changed variant whose images would not decode still changed; say so
      // rather than printing a confident "0%".
      return variant.diff ? `**${percent(variant.diff.ratio)}**` : '**changed**';
    case 'added':
      return 'new';
    case 'removed':
      return 'removed';
    default:
      return '·';
  }
}

/**
 * Short form of a path for a comment, where a full repo path is mostly
 * prefix.
 *
 * Cuts after the *last* `app` segment, not the first. The paths here look
 * like `web/app/src/app/features/billing/…`, so matching the first one
 * leaves `src/app/…` — still readable, but it keeps the two segments that
 * carry no information while the line is already long. The last match lands
 * on the source root, giving `features/billing/…`. A file directly under
 * the source root (`web/app/src/styles.scss`) has only the workspace `app`
 * to match and keeps its `src/` prefix, which is correct for it.
 */
function short(filePath) {
  const parts = filePath.split('/');
  const appIndex = parts.lastIndexOf('app');
  return appIndex === -1 ? filePath : parts.slice(appIndex + 1).join('/');
}

export function renderMarkdown(model, { reportLink, reportHint } = {}) {
  const { summary } = model;
  const touched = summary.changed + summary.added + summary.removed;
  const lines = [];

  // The heading carries the whole verdict, because on a busy PR page it is
  // often the only part that gets read.
  const headline = touched
    ? `${touched} screen${touched === 1 ? '' : 's'} changed`
    : summary.screens
      ? 'no screen changed'
      : 'no screens under visual coverage';
  lines.push(`### Design review · ${headline}`, '');

  if (!model.base.sha) {
    lines.push(`> No merge base with \`${model.base.ref}\` — nothing to compare against, so every screen below reads as new.`, '');
  }

  const moved = model.screens.filter(screen => screen.status !== 'unchanged');
  if (moved.length) {
    // Columns are the viewports actually present, in their canonical order,
    // so a report covering only desktop does not carry an empty mobile column.
    const projects = [];
    for (const screen of model.screens) {
      for (const variant of screen.variants) {
        if (!projects.some(entry => entry.project === variant.project)) projects.push(variant);
      }
    }
    projects.sort((a, b) => a.order - b.order);

    lines.push(`| Screen | ${projects.map(variant => variant.label).join(' | ')} |`);
    lines.push(`| --- | ${projects.map(() => '---').join(' | ')} |`);
    for (const screen of moved) {
      const cells = projects.map(entry => {
        const variant = screen.variants.find(candidate => candidate.project === entry.project);
        return variant ? cell(variant) : '—';
      });
      lines.push(`| ${screen.title} | ${cells.join(' | ')} |`);
    }
    lines.push('', `<sub>Percentages are the share of pixels that differ, compared at the default sensitivity.</sub>`, '');
  } else if (summary.screens) {
    lines.push(`All ${summary.screens} covered screens are byte-identical to \`${model.base.ref}\`.`, '');
  }

  // The gap list. Deliberately not folded into a <details>: it is the part
  // of this comment most worth acting on and least likely to be sought out.
  const uncovered = model.impact.uncovered ?? [];
  if (uncovered.length) {
    lines.push(
      `⚠️ **${uncovered.length} design file${uncovered.length === 1 ? '' : 's'} changed that no covered screen renders.** Nothing above shows what ${uncovered.length === 1 ? 'it looks' : 'they look'} like now.`,
      '',
    );
    // Capped, because a wide-ranging PR can touch dozens and a comment that
    // long stops being read at all.
    for (const file of uncovered.slice(0, 10)) lines.push(`- \`${short(file.path)}\``);
    if (uncovered.length > 10) lines.push(`- …and ${uncovered.length - 10} more`);
    lines.push('');
  }

  if (summary.unchanged && moved.length) {
    lines.push(`<sub>${summary.unchanged} other screen${summary.unchanged === 1 ? '' : 's'} unchanged.</sub>`, '');
  }

  if (reportLink) {
    lines.push(`**[Open the before/after report →](${reportLink})**${reportHint ? ` ${reportHint}` : ''}`, '');
  }

  lines.push(
    `<sub>Compared \`${model.base.ref}\` (\`${model.base.sha?.slice(0, 7) ?? 'none'}\`) with \`${model.head.worktree ? 'working tree' : model.head.ref}\` (\`${model.head.sha?.slice(0, 7) ?? '?'}\`). Baselines are read from git, not from a test run — see \`${path.posix.join('web/app/e2e/design-review', 'README.md')}\`.</sub>`,
  );

  return `${lines.join('\n').replace(/\n{3,}/g, '\n\n').trim()}\n`;
}
