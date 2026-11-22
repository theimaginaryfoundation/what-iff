import { LEARNING_OBJECTIVES, MAIN_BRANCHES } from './constellation.data';
import type { LearningObjective, LearningProgress } from './constellation.model';

export function completedBranchCount(completed: ReadonlySet<string>): number {
  return MAIN_BRANCHES.filter(branch => LEARNING_OBJECTIVES.filter(node => node.branch === branch).every(node => completed.has(node.id)))
    .length;
}

export function objectiveReady(node: LearningObjective, completed: ReadonlySet<string>): boolean {
  return (
    node.requires.every(id => completed.has(id)) &&
    (!node.requiresAnyBranches || completedBranchCount(completed) >= node.requiresAnyBranches)
  );
}

/** Re-evaluate saved credit against prerequisites; corrupted/out-of-order storage cannot skip a path. */
export function completedObjectives(progress: LearningProgress): ReadonlySet<string> {
  const completed = new Set<string>();
  let changed = true;
  while (changed) {
    changed = false;
    for (const node of LEARNING_OBJECTIVES) {
      if (completed.has(node.id) || !progress[node.id] || !objectiveReady(node, completed)) continue;
      completed.add(node.id);
      changed = true;
    }
  }
  return completed;
}

export function learningRank(completed: ReadonlySet<string>): string {
  if (completed.has('B3')) return 'Builder';
  if (!completed.has('S6')) return 'Noob';
  if (completedBranchCount(completed) >= 2) return 'Builder in training';
  if (completedBranchCount(completed)) return 'Maker';
  return 'Explorer';
}

export function parseLearningProgress(raw: string | null): LearningProgress {
  if (!raw) return {};
  try {
    const value: unknown = JSON.parse(raw);
    if (!value || typeof value !== 'object' || Array.isArray(value)) return {};
    const parsed: Record<string, { note: string; completedAt: string }> = {};
    for (const node of LEARNING_OBJECTIVES) {
      const entry = (value as Record<string, unknown>)[node.id];
      if (!entry || typeof entry !== 'object') continue;
      const record = entry as Record<string, unknown>;
      const note = record['note'];
      const completedAt = record['completedAt'];
      if (
        typeof note !== 'string' ||
        note.trim().length < 12 ||
        note.length > 2000 ||
        typeof completedAt !== 'string' ||
        !Number.isFinite(Date.parse(completedAt))
      )
        continue;
      parsed[node.id] = { note, completedAt };
    }
    return parsed;
  } catch {
    return {};
  }
}
