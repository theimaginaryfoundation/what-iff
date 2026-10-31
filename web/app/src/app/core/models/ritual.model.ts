/**
 * Agent Skills fields a skill carries beside its name, description and content (ADR 0x035).
 * On update, an omitted field keeps its stored value.
 */
export interface RitualSkillFields {
  /** The skill's name in the Agent Skills sense: 1-64 chars of [a-z0-9-], unique per account. */
  slug: string;
  /** SKILL.md `license`, free text. */
  license: string;
  /** SKILL.md `compatibility`: what the skill needs from its environment. */
  compatibility: string;
  /** SKILL.md `metadata`, string to string; keys starting with `whatiff-` are reserved. */
  metadata: Record<string, string>;
  /** SKILL.md `allowed-tools`: tools loaded with the skill, never a permission grant. */
  allowed_tools: string[];
  /** When true the model never sees the skill in its index; only you, a mode or a job can activate it. */
  disable_model_invocation: boolean;
  /** When false the skill is hidden from the / picker and hotkeys; the model may still load it. */
  user_invocable: boolean;
}

/** The skill fields a new skill has before the editor touches them; also handy for test fixtures. */
export const RITUAL_SKILL_FIELD_DEFAULTS: Readonly<RitualSkillFields> = {
  slug: '',
  license: '',
  compatibility: '',
  metadata: {},
  allowed_tools: [],
  disable_model_invocation: false,
  user_invocable: true,
};

export interface Ritual extends RitualSkillFields {
  id: string;
  name: string;
  description: string;
  content: string;
  hotkeys: string;
  personality_id: string | null;
  mcp_server_ids?: string[];
  created_at: string;
  updated_at: string;
}

export interface CreateRitualRequest extends Partial<RitualSkillFields> {
  name: string;
  description: string;
  content: string;
  hotkeys?: string;
  personality_id?: string | null;
  mcp_server_ids?: string[];
}

export interface UpdateRitualRequest extends Partial<RitualSkillFields> {
  name: string;
  description: string;
  content: string;
  hotkeys?: string;
  personality_id?: string | null;
  mcp_server_ids?: string[];
}

/** Response of POST /ritual/import. */
export interface RitualImportResult {
  ritual: Ritual;
  warnings: string[];
  /** Bundle files other than SKILL.md, which this version does not store. */
  skipped_files: number;
  connectors_linked: string[];
  connectors_missing: string[];
}

/** Bounds the editor shows; the server enforces the same (internal/skills). */
export const RITUAL_SLUG_MAX = 64;
export const RITUAL_DESCRIPTION_MAX = 1024;
export const RITUAL_SHORT_FIELD_MAX = 500;
export const RITUAL_BODY_SOFT_TOKENS = 5000;
export const RITUAL_BODY_MAX_BYTES = 64 * 1024;

/** Derives a spec-conforming slug from a display name, like the server's skills.Slugify. */
export function slugifySkillName(name: string): string {
  const slug = name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, RITUAL_SLUG_MAX)
    .replace(/-+$/g, '');
  return slug || 'skill';
}

/** Rough token count for a body, about four characters a token, like the server's estimate. */
export function estimateSkillTokens(text: string): number {
  return Math.ceil(new TextEncoder().encode(text).length / 4);
}

export interface RitualFilters {
  name?: string;
  search?: string;
  personality_id?: string;
  personality_ids?: string[];
  global_only?: boolean;
  has_hotkeys?: boolean;
  sort?: RitualSort;
  min_date?: string;
  max_date?: string;
}

export type RitualSort = 'name_asc' | 'created_desc' | 'updated_desc';

export interface PaginatedRitualResponse {
  results: Ritual[];
  total_count: number;
  page: number;
}
