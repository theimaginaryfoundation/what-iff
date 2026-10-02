/**
 * Pull request context, via the GitHub CLI.
 *
 * Entirely best-effort, and deliberately the only part of this tool that
 * knows GitHub exists. A missing `gh`, an unauthenticated one, no network,
 * or a branch with no pull request all return null, and the report loses one
 * header line. Nothing downstream — not the HTML, not the JSON, not the PR
 * comment — requires it; the CI workflow in particular never depends on `gh`
 * being installed in the runner image.
 */

import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { WORKTREE } from './git.mjs';

const execFileAsync = promisify(execFile);

export async function readPullRequest(root, headRef) {
  // `gh pr view` with no argument resolves the pull request for the branch
  // that is *checked out*, which is only the right answer when the report's
  // head is that branch. Asked for some other ref — `--head v1.2.0`, or a
  // colleague's commit — it would cheerfully return the current branch's PR
  // and stamp a report about one change with another change's title. Better
  // no header line than a confidently wrong one.
  if (headRef !== WORKTREE && headRef !== 'HEAD') return null;

  try {
    const { stdout } = await execFileAsync('gh', ['pr', 'view', '--json', 'number,title,url,author,headRefName,baseRefName,isDraft'], {
      cwd: root,
      encoding: 'utf8',
      timeout: 15_000,
    });
    const pr = JSON.parse(stdout);
    return { number: pr.number, title: pr.title, url: pr.url, author: pr.author?.login, head: pr.headRefName, base: pr.baseRefName, draft: pr.isDraft };
  } catch {
    return null;
  }
}
