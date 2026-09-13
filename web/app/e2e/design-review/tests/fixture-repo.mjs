/**
 * Builds a throwaway git repository for the tests to read.
 *
 * The collector's whole job is reading history, so a test that stubs git out
 * would only prove that the stub matches the stub. Every case here runs
 * against a real repository with real commits — it costs a few hundred
 * milliseconds and it is the only way the merge-base logic, the untracked
 * file handling and the rename detection get tested at all.
 */

import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { mkdtemp, mkdir, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';

const execFileAsync = promisify(execFile);

export const SNAPSHOT_DIR = 'web/app/e2e/tests/visual/demo.visual.spec.ts-snapshots';

export class FixtureRepo {
  constructor(root) {
    this.root = root;
  }

  static async create() {
    const root = await mkdtemp(path.join(tmpdir(), 'design-review-'));
    const repo = new FixtureRepo(root);
    await repo.git('init', '--initial-branch=main');
    // Identity and signing are set locally so the suite does not depend on
    // — or disturb — whatever the machine running it has configured.
    await repo.git('config', 'user.email', 'fixture@example.test');
    await repo.git('config', 'user.name', 'Fixture');
    await repo.git('config', 'commit.gpgsign', 'false');
    return repo;
  }

  git(...args) {
    return execFileAsync('git', ['-C', this.root, ...args], { encoding: 'utf8', maxBuffer: 16 * 1024 * 1024 });
  }

  async write(relPath, contents) {
    const full = path.join(this.root, relPath);
    await mkdir(path.dirname(full), { recursive: true });
    await writeFile(full, contents);
  }

  async commit(message) {
    await this.git('add', '-A');
    await this.git('commit', '-m', message);
  }

  async remove(relPath) {
    await rm(path.join(this.root, relPath));
  }

  async dispose() {
    await rm(this.root, { recursive: true, force: true });
  }
}
