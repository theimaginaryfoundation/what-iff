export interface Memory {
  id: string;
  chat_id?: string;
  chat_name?: string;
  content: string;
  level: 'global' | 'personality' | 'thread' | 'summary';
  type: 'Context';
  status: 'active' | 'inactive';
  // Stored confidence is a float in [0,1] (LLM buckets map to anchors 0.3/0.6/0.9; other signals
  // can refine it). Create/patch still accept the coarse buckets below.
  confidence: number;
  chain_metadata?: {
    duplicate_count: number;
    verified_timestamps_first?: string[];
    verified_timestamps_last?: string[];
    merged_from_memory_ids?: string[];
  } | null;
  /** Set when this memory is cross-referenced with related-but-distinct memories (link, not merge). */
  link_group_id?: string | null;
  starred: boolean;
  pinned_personality_id?: string | null;
  created_at: string;
  updated_at: string;
}

export type MemorySort = 'created_desc' | 'created_asc' | 'updated_desc';

export interface MemoryFilters {
  chat_id?: string;
  level?: 'global' | 'personality' | 'thread' | 'summary';
  type?: 'Context';
  starred?: boolean;
  pinned_personality_id?: string;
  pinned_personality_ids?: string[];
  global_only?: boolean;
  query?: string;
  status?: 'active' | 'inactive';
  sort?: MemorySort;
  min_date?: string;
  max_date?: string;
}

export interface PaginatedMemoryResponse {
  results: Memory[];
  total_count: number;
  page: number;
}

export interface CreateMemoryInput {
  chat_id?: string;
  content: string;
  level: 'global' | 'personality' | 'thread' | 'summary';
  type?: 'Context';
  starred?: boolean;
  pinned_personality_id?: string | null;
  confidence?: 'low' | 'medium' | 'high';
}

export interface BatchCreateMemoryInput {
  memories: CreateMemoryInput[];
}

export interface BatchCreateMemoryResponse {
  memories: Memory[];
}

export interface MemoryPatch {
  content?: string;
  level?: 'global' | 'personality' | 'thread' | 'summary';
  type?: 'Context';
  starred?: boolean;
  pinned_personality_id?: string | null;
  status?: 'active' | 'inactive';
  confidence?: 'low' | 'medium' | 'high';
}

export interface MemoryImportResult {
  imported_count: number;
  duplicate_count: number;
  invalid_record_count: number;
  invalid_reasons?: {
    malformed_json: number;
    missing_id: number;
    empty_content: number;
    missing_created_at: number;
    missing_chat_id: number;
  };
  skipped_missing_chat_count: number;
  skipped_missing_personality_count: number;
}

export type MemoryMergeType = 'create' | 'fold_live' | 'link';

export interface MemoryMergeSourceMember {
  content: string;
  scope: string;
  confidence?: 'low' | 'medium' | 'high';
  memory_id?: string | null;
  is_new: boolean;
}

export interface MemoryMergeEvent {
  id: string;
  survivor_memory_id: string;
  merge_type: MemoryMergeType;
  content: string;
  duplicates_folded: number;
  /** Set for link events: the shared id assigned to the cross-referenced members. */
  link_group_id?: string | null;
  /** The compaction (checkpoint) that produced this merge event, if any. */
  compaction_event_id?: string | null;
  source_members?: MemoryMergeSourceMember[];
  reverted_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface PaginatedMemoryMergeEventResponse {
  results: MemoryMergeEvent[];
  total_count: number;
  page: number;
}

export type CheckpointSnapshotKind = 'summary' | 'scratchpad';

/** A content-addressed capture of a conversation summary or personality scratchpad. */
export interface CheckpointSnapshot {
  id: string;
  kind: CheckpointSnapshotKind;
  chat_id?: string | null;
  personality_id?: string | null;
  content: string;
  created_at: string;
}

/** One memory that was live in the compacted segment. */
export interface CompactionLoadedMemory {
  memory_id?: string | null;
  content: string;
  scope: string;
  confidence?: number;
}

/** The audit record for one conversation checkpoint ("compaction"). */
export interface CompactionEvent {
  id: string;
  chat_id: string;
  chat_name?: string;
  personality_id?: string | null;
  assistant_message_id?: string | null;
  provider?: string;
  reason?: string;
  old_summary?: CheckpointSnapshot | null;
  new_summary?: CheckpointSnapshot | null;
  old_scratchpad?: CheckpointSnapshot | null;
  new_scratchpad?: CheckpointSnapshot | null;
  loaded_memories?: CompactionLoadedMemory[];
  created_memories?: CompactionLoadedMemory[];
  merge_events?: MemoryMergeEvent[];
  created_at: string;
  updated_at: string;
}

export interface PaginatedCompactionEventResponse {
  results: CompactionEvent[];
  total_count: number;
  page: number;
}
