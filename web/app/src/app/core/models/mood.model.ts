export interface Mood {
  id: string;
  name: string;
  description: string;
  prompt_snippet: string;
  /** Optional model name to switch to when this mood becomes active. */
  recommended_model?: string;
  /** Zero or one image ID attached to this mood. */
  image_ids: string[];
  ritual_ids: string[];
  /** UUIDs of personalities this mood is attached to (populated on GET /mood/{id}). */
  personality_ids?: string[];
  thumbnail_data?: string; // base64-encoded JPEG (~128×128 for newly saved moods)
  created_at: string;
  updated_at: string;
}

export interface CreateMoodRequest {
  name: string;
  description: string;
  prompt_snippet: string;
  recommended_model?: string;
  /** Zero or one image ID attached to this mood. */
  image_ids?: string[];
  ritual_ids?: string[];
}

export interface UpdateMoodRequest {
  name: string;
  description: string;
  prompt_snippet: string;
  recommended_model?: string;
  /** Zero or one image ID attached to this mood. */
  image_ids?: string[];
  ritual_ids?: string[];
}

/** Replaces all personality associations for a mood. */
export interface AttachMoodToPersonalitiesRequest {
  personality_ids: string[];
}
