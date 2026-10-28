/**
 * Turn settings (ADR 0x026): how hard a turn asks the model to think (`reasoning_effort`) and which
 * tool loop drives it (`agent_loop`). Both are nullable at every level (account default,
 * personality, thread, mode, agent job); null means "not set at this level".
 *
 * Precedence for a turn is agent job → active mode → thread → personality → account default, read
 * at each turn (see {@link resolveEffectiveTurnSettings}). A sandboxed thread uses only its own
 * values (and a job's); the mode, personality and account default do not apply there.
 */

export const REASONING_EFFORTS = ['off', 'low', 'medium', 'high'] as const;
export type ReasoningEffort = (typeof REASONING_EFFORTS)[number];

export const AGENT_LOOPS = ['standard', 'advanced'] as const;
export type AgentLoop = (typeof AGENT_LOOPS)[number];

/**
 * How a model takes a reasoning effort (`Model.reasoning_support`):
 * - `none`: no control; the setting is ignored and the control is hidden.
 * - `optional`: Off / Low / Medium / High.
 * - `toggle`: Off / On (On is stored as `medium`).
 * - `always`: Low / Medium / High; a stored `off` runs as `low`.
 */
export type ReasoningSupport = 'none' | 'optional' | 'toggle' | 'always';

/**
 * A value on an update route that preserves omitted fields (`PUT /user/preferences`,
 * `PATCH /chat/{id}`, `PUT /agent-job/{id}`): `''` clears the level back to "not set".
 */
export type ReasoningEffortUpdate = ReasoningEffort | '';
export type AgentLoopUpdate = AgentLoop | '';

/** One level's stored values, as carried on preferences, personalities, modes, threads and jobs. */
export interface TurnSettingValues {
  reasoning_effort?: ReasoningEffort | null;
  agent_loop?: AgentLoop | null;
}

/**
 * A change to one level's turn settings: names at least one key explicitly. A key set to null
 * clears that level; an omitted key is left as it is.
 */
export type TurnSettingsPatch =
  | { reasoning_effort: ReasoningEffort | null; agent_loop?: AgentLoop | null }
  | { reasoning_effort?: ReasoningEffort | null; agent_loop: AgentLoop | null };

export const REASONING_EFFORT_LABELS: Record<ReasoningEffort, string> = {
  off: 'Off',
  low: 'Low',
  medium: 'Medium',
  high: 'High',
};

export const AGENT_LOOP_LABELS: Record<AgentLoop, string> = {
  standard: 'Standard',
  advanced: 'Advanced',
};

/** Short explanations for help text and tooltips. */
export const REASONING_EFFORT_DESCRIPTION =
  'How hard the model thinks before it answers. Higher levels are slower and can use more credits.';
export const AGENT_LOOP_DESCRIPTIONS: Record<AgentLoop, string> = {
  standard: 'Answers in one go, calling tools as it needs them.',
  advanced: 'Works in steps on longer tasks, showing its progress as it goes. Uses more credits.',
};

/** Label for "on" on a model that can only switch thinking on or off. */
export const REASONING_TOGGLE_ON_LABEL = 'On';
/** What a toggle model's "On" is stored as. */
export const REASONING_TOGGLE_ON_VALUE: ReasoningEffort = 'medium';

export interface TurnSettingOption<T extends string> {
  value: T;
  label: string;
}

/** Every effort level, for editors that are not tied to one model (account, personality, mode, job). */
export const REASONING_EFFORT_OPTIONS: readonly TurnSettingOption<ReasoningEffort>[] = REASONING_EFFORTS.map(value => ({
  value,
  label: REASONING_EFFORT_LABELS[value],
}));

export const AGENT_LOOP_OPTIONS: readonly TurnSettingOption<AgentLoop>[] = AGENT_LOOPS.map(value => ({
  value,
  label: AGENT_LOOP_LABELS[value],
}));

export function isReasoningEffort(value: unknown): value is ReasoningEffort {
  return typeof value === 'string' && (REASONING_EFFORTS as readonly string[]).includes(value);
}

export function isAgentLoop(value: unknown): value is AgentLoop {
  return typeof value === 'string' && (AGENT_LOOPS as readonly string[]).includes(value);
}

/** Reads a `<select>` value: a level, or null for the "not set" option (`''`) and anything unknown. */
export function parseReasoningEffort(value: unknown): ReasoningEffort | null {
  return isReasoningEffort(value) ? value : null;
}

/** Reads a `<select>` value: a loop, or null for the "not set" option (`''`) and anything unknown. */
export function parseAgentLoop(value: unknown): AgentLoop | null {
  return isAgentLoop(value) ? value : null;
}

/** The effort choices a model with this support offers; empty when the control should be hidden. */
export function reasoningEffortOptions(support: ReasoningSupport | null | undefined): TurnSettingOption<ReasoningEffort>[] {
  switch (support) {
    case 'optional':
      return [...REASONING_EFFORT_OPTIONS];
    case 'toggle':
      return [
        { value: 'off', label: REASONING_EFFORT_LABELS.off },
        { value: REASONING_TOGGLE_ON_VALUE, label: REASONING_TOGGLE_ON_LABEL },
      ];
    case 'always':
      return REASONING_EFFORT_OPTIONS.filter(option => option.value !== 'off');
    default:
      return [];
  }
}

/**
 * What a stored effort means on a model with this support (mirrors the server's
 * `EffectiveReasoningEffort`): null when not set or the model has no control; a toggle model
 * runs every level as on (`medium`); an always-on model runs `off` as `low`.
 */
export function effectiveReasoningEffort(
  support: ReasoningSupport | null | undefined,
  effort: ReasoningEffort | null | undefined,
): ReasoningEffort | null {
  if (!isReasoningEffort(effort)) return null;
  switch (support) {
    case 'optional':
      return effort;
    case 'toggle':
      return effort === 'off' ? 'off' : REASONING_TOGGLE_ON_VALUE;
    case 'always':
      return effort === 'off' ? 'low' : effort;
    default:
      return null;
  }
}

/** Display label for a stored effort on a model with this support; null when not set or unsupported. */
export function reasoningEffortLabel(
  support: ReasoningSupport | null | undefined,
  effort: ReasoningEffort | null | undefined,
): string | null {
  const effective = effectiveReasoningEffort(support, effort);
  if (!effective) return null;
  if (support === 'toggle') return effective === 'off' ? REASONING_EFFORT_LABELS.off : REASONING_TOGGLE_ON_LABEL;
  return REASONING_EFFORT_LABELS[effective];
}

/** Which level an effective value came from; null when no level sets it. */
export type TurnSettingSource = 'job' | 'mode' | 'thread' | 'personality' | 'account';

export interface ResolvedTurnSetting<T> {
  value: T | null;
  source: TurnSettingSource | null;
}

export interface ResolvedTurnSettings {
  reasoningEffort: ResolvedTurnSetting<ReasoningEffort>;
  agentLoop: ResolvedTurnSetting<AgentLoop>;
}

/** Every level a turn's settings can come from; any may be absent. */
export interface TurnSettingLevels {
  /** The agent job driving the turn, if any (not part of a chat turn). */
  job?: TurnSettingValues | null;
  /** The thread's active mode. */
  mode?: TurnSettingValues | null;
  /** The thread's own values. */
  thread?: TurnSettingValues | null;
  /** The thread's personality. */
  personality?: TurnSettingValues | null;
  /** The account defaults (`UserPreferences`). */
  account?: TurnSettingValues | null;
  /** A sandboxed thread (`Chat.context_scope === 'sandbox'`): only the job and thread levels apply. */
  sandboxed?: boolean;
}

/** The levels below the thread that a thread setting nothing of its own inherits from. */
export type InheritedTurnSettings = Pick<TurnSettingLevels, 'personality' | 'account' | 'sandboxed'>;

/** Highest precedence first, as the server resolves a turn. */
const LEVEL_ORDER: readonly TurnSettingSource[] = ['job', 'mode', 'thread', 'personality', 'account'];
const SANDBOX_LEVELS: ReadonlySet<TurnSettingSource> = new Set(['job', 'thread']);

/**
 * Resolves what a turn uses, per setting, the way the server does: agent job → active mode →
 * thread → personality → account default, the first level that sets a valid value winning. In a
 * sandboxed thread only the job and the thread apply. Invalid stored values are skipped. An unset
 * loop runs Standard; an unset effort is the model's default.
 */
export function resolveEffectiveTurnSettings(levels: TurnSettingLevels): ResolvedTurnSettings {
  const applicable = LEVEL_ORDER.filter(level => !levels.sandboxed || SANDBOX_LEVELS.has(level));
  const resolve = <T>(read: (values: TurnSettingValues) => T | null): ResolvedTurnSetting<T> => {
    for (const level of applicable) {
      const values = levels[level];
      const value = values ? read(values) : null;
      if (value !== null) return { value, source: level };
    }
    return { value: null, source: null };
  };
  return {
    reasoningEffort: resolve(values => parseReasoningEffort(values.reasoning_effort)),
    agentLoop: resolve(values => parseAgentLoop(values.agent_loop)),
  };
}
