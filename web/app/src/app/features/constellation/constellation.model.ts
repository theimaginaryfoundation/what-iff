/** Learning progress is self-reported; it never grants product permissions. */
export interface LearningObjective {
  id: string;
  branch: string;
  title: string;
  requires: readonly string[];
  objective: string;
  acceptance: string;
  prompt: string;
  guides: readonly string[];
  xp: number;
  minutes: number;
  requiresAnyBranches?: number;
}

export interface LearningGuide {
  slug: string;
  title: string;
  description: string;
}

export interface LearningCompletion {
  note: string;
  completedAt: string;
}

export type LearningProgress = Readonly<Record<string, LearningCompletion>>;
