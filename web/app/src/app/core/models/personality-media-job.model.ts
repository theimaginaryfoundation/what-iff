export interface PersonalityMediaJobResponse {
  job_id: string;
  job_type: 'expression_grid' | 'personality_portrait' | 'personality_generation';
}

export interface ActivePersonalityMediaJob {
  job_id: string;
  job_type: 'expression_grid' | 'personality_portrait' | 'personality_generation';
  reference: string;
  status: string;
  personality_id?: string;
  personality_name?: string;
  flow_id?: string;
  error?: string;
}

export interface PersonalityMediaJobConflict {
  message: string;
  active: ActivePersonalityMediaJob;
}

/** One generated-but-unassigned expression portrait (gallery image pinned to the personality). */
export interface ExpressionCandidate {
  expression_key: string;
  image_id: string;
}

/**
 * Decoded shape of `Job.progress` for expression candidate runs
 * (`POST /personality/{id}/expressions/generate-candidates`). `candidates` is present once the
 * job completes.
 */
export interface ExpressionCandidatesProgress {
  mode: 'candidates';
  expressions: string[];
  reference_image_id?: string;
  candidates?: ExpressionCandidate[];
}

/** Parses a job's opaque progress string as {@link ExpressionCandidatesProgress}, or null. */
export function parseExpressionCandidatesProgress(progress: string | undefined | null): ExpressionCandidatesProgress | null {
  if (!progress) return null;
  try {
    const parsed = JSON.parse(progress) as Partial<ExpressionCandidatesProgress>;
    if (parsed?.mode !== 'candidates' || !Array.isArray(parsed.expressions)) return null;
    return parsed as ExpressionCandidatesProgress;
  } catch {
    return null;
  }
}
