export type JobStatus =
  | 'pending'
  | 'processing'
  | 'inference_complete'
  | 'expression_complete'
  | 'compaction_complete'
  | 'complete'
  | 'cancelled'
  | 'failed';

export interface Job {
  id: string;
  user_id: string;
  job_type: string;
  reference: string;
  status: JobStatus;
  error?: string;
  result_id?: string;
  progress_current?: number;
  progress_total?: number;
  status_message?: string;
  /**
   * Opaque JSON-encoded progress payload for long-running jobs (parse per job_type).
   * For `chat_import` this decodes to {@link ChatImportProgress}.
   */
  progress?: string;
  /**
   * Incremental assistant text chunks emitted while inference is still in progress.
   */
  draft_deltas?: string[];
  created_at: string;
  updated_at: string;
}

/** Decoded shape of {@link Job.progress} for `chat_import` jobs. */
export interface ChatImportProgress {
  phase: 'uploading' | 'parsing' | 'importing' | 'complete' | 'failed' | string;
  source?: string;
  total: number;
  imported: number;
  skipped: number;
  /** Chat IDs created during this import run (present when phase is complete). */
  imported_ids?: string[];
}

export interface JobFilters {
  job_type?: string;
  reference?: string;
  status?: JobStatus;
}

/** Active non-terminal chat_message job for a user turn (resume / polling). */
export interface ActiveChatMessageJob {
  job_id: string;
  status: JobStatus;
}
