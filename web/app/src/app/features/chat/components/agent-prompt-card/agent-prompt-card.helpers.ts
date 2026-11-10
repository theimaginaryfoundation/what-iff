import {
  AgentPromptPayload,
  AgentPromptQuestion,
  AgentPromptResponse,
  ElicitationProperty,
  ElicitationSchema,
} from '../../../../core/models/job.model';

/**
 * One answer being written: the options picked and the person's own text. The text only counts
 * while `other` is on, so someone who starts typing, then picks an option, then changes their mind
 * gets their words back without them being sent in between.
 */
export interface QuestionDraft {
  selected: string[];
  text: string;
  /** The own-answer box is open: "Other", or an open question. */
  other: boolean;
}

/** One choice of an elicitation enum field. */
export interface FieldChoice {
  value: string;
  label: string;
}

/** An elicitation form field, ready to render. */
export interface FormField {
  name: string;
  label: string;
  description?: string;
  required: boolean;
  /** text, number, checkbox (boolean), select (single enum), multi (array of enums). */
  control: 'text' | 'number' | 'checkbox' | 'select' | 'multi' | 'date' | 'email' | 'url';
  integer: boolean;
  choices: FieldChoice[];
  min?: number;
  max?: number;
  minLength?: number;
  maxLength?: number;
  defaultValue: unknown;
}

/** A required open question has nothing to pick, so its box starts open. */
export function emptyDrafts(questions: readonly AgentPromptQuestion[] | undefined): Record<string, QuestionDraft> {
  const out: Record<string, QuestionDraft> = {};
  for (const q of questions ?? []) out[q.id] = { selected: [], text: '', other: !q.options?.length && !q.optional };
  return out;
}

/** Picks an option: single-select questions replace the pick and close "Other", multi add/remove. */
export function toggleOption(draft: QuestionDraft, question: AgentPromptQuestion, label: string): QuestionDraft {
  if (question.multi_select) {
    const selected = draft.selected.includes(label)
      ? draft.selected.filter(s => s !== label)
      : [...draft.selected, label];
    return { ...draft, selected };
  }
  return { ...draft, selected: [label], other: false };
}

/** Opens or closes the own-answer box. On a single-select question it replaces the picked option. */
export function setOtherChosen(draft: QuestionDraft, question: AgentPromptQuestion, chosen: boolean): QuestionDraft {
  if (chosen && !question.multi_select) return { ...draft, selected: [], other: true };
  return { ...draft, other: chosen };
}

export function setOwnText(draft: QuestionDraft, text: string): QuestionDraft {
  return { ...draft, text };
}

/** Clears a single-select optional question's answer. */
export function skipQuestion(draft: QuestionDraft): QuestionDraft {
  return { ...draft, selected: [], other: false };
}

/** Whether the draft holds something that would be sent. */
export function questionAnswered(draft: QuestionDraft | undefined): boolean {
  return !!draft && (draft.selected.length > 0 || (draft.other && draft.text.trim().length > 0));
}

/** Whether the question no longer holds up sending. */
export function questionComplete(question: AgentPromptQuestion, draft: QuestionDraft | undefined): boolean {
  return !!question.optional || questionAnswered(draft);
}

/** The accept response for a questions prompt; unanswered optional questions are left out. */
export function questionsResponse(drafts: Record<string, QuestionDraft>): AgentPromptResponse {
  const answers: NonNullable<AgentPromptResponse['answers']> = {};
  for (const [id, draft] of Object.entries(drafts)) {
    if (!questionAnswered(draft)) continue;
    const text = draft.other ? draft.text.trim() : '';
    answers[id] = {
      ...(draft.selected.length ? { selected: draft.selected } : {}),
      ...(text ? { text } : {}),
    };
  }
  return { action: 'accept', answers };
}

/** A question's short name for its tab: the agent's header, or a question number. */
export function questionTabLabel(question: AgentPromptQuestion, index: number): string {
  return question.header?.trim() || `Question ${index + 1}`;
}

/** Splits the " (Recommended)" the agent is told to add to an option's label from its text. */
export function optionLabelParts(label: string): { text: string; recommended: boolean } {
  const text = label.replace(/\s*\(recommended\)\s*$/i, '');
  return { text: text || label, recommended: !!text && text !== label };
}

/** The fields of an elicitation form, in schema order, with unsupported fields dropped. */
export function formFields(schema: ElicitationSchema | undefined): FormField[] {
  const required = new Set(schema?.required ?? []);
  return Object.entries(schema?.properties ?? {}).flatMap(([name, prop]) => {
    const field = formField(name, prop, required.has(name));
    return field ? [field] : [];
  });
}

function formField(name: string, prop: ElicitationProperty, required: boolean): FormField | null {
  const base = {
    name,
    label: prop.title?.trim() || name,
    description: prop.description,
    required,
    integer: prop.type === 'integer',
    choices: [] as FieldChoice[],
    min: prop.minimum,
    max: prop.maximum,
    minLength: prop.minLength,
    maxLength: prop.maxLength,
    defaultValue: prop.default,
  };
  switch (prop.type) {
    case 'boolean':
      return { ...base, control: 'checkbox' };
    case 'number':
    case 'integer':
      return { ...base, control: 'number' };
    case 'array': {
      const choices = enumChoices(prop.items?.enum, undefined, prop.items?.anyOf);
      return choices.length ? { ...base, control: 'multi', choices } : null;
    }
    case 'string': {
      const choices = enumChoices(prop.enum, prop.enumNames, prop.oneOf);
      if (choices.length) return { ...base, control: 'select', choices };
      const control = prop.format === 'date' ? 'date' : prop.format === 'email' ? 'email' : prop.format === 'uri' ? 'url' : 'text';
      return { ...base, control };
    }
    default:
      return null;
  }
}

function enumChoices(
  values: unknown[] | undefined,
  names: string[] | undefined,
  consts: { const: unknown; title?: string }[] | undefined,
): FieldChoice[] {
  const out: FieldChoice[] = [];
  (values ?? []).forEach((v, i) => {
    if (typeof v === 'string') out.push({ value: v, label: names?.[i] ?? v });
  });
  for (const c of consts ?? []) {
    if (typeof c.const === 'string') out.push({ value: c.const, label: c.title ?? c.const });
  }
  return out;
}

/** The form's starting values: each field's default when it has a usable one. */
export function initialFormValues(fields: readonly FormField[]): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const f of fields) {
    if (f.defaultValue === undefined || f.defaultValue === null) {
      if (f.control === 'multi') out[f.name] = [];
      if (f.control === 'checkbox') out[f.name] = false;
      continue;
    }
    out[f.name] = f.defaultValue;
  }
  return out;
}

/** The first problem with the form's values (shown before sending), or null. */
export function formError(fields: readonly FormField[], values: Record<string, unknown>): string | null {
  for (const f of fields) {
    const v = values[f.name];
    const empty = v === undefined || v === null || v === '' || (Array.isArray(v) && v.length === 0);
    if (empty) {
      if (f.required && f.control !== 'checkbox') return `${f.label} is required.`;
      continue;
    }
    if (f.control === 'number') {
      const n = Number(v);
      if (!Number.isFinite(n)) return `${f.label} must be a number.`;
      if (f.integer && !Number.isInteger(n)) return `${f.label} must be a whole number.`;
      if (f.min !== undefined && n < f.min) return `${f.label} must be at least ${f.min}.`;
      if (f.max !== undefined && n > f.max) return `${f.label} must be at most ${f.max}.`;
    }
    if (typeof v === 'string') {
      if (f.minLength !== undefined && v.length < f.minLength) return `${f.label} must be at least ${f.minLength} characters.`;
      if (f.maxLength !== undefined && v.length > f.maxLength) return `${f.label} must be at most ${f.maxLength} characters.`;
    }
  }
  return null;
}

/** The accept response for a form: values typed to the schema, empty optional fields left out. */
export function formResponse(fields: readonly FormField[], values: Record<string, unknown>): AgentPromptResponse {
  const content: Record<string, unknown> = {};
  for (const f of fields) {
    const v = values[f.name];
    if (v === undefined || v === null || v === '' || (Array.isArray(v) && v.length === 0 && !f.required)) continue;
    content[f.name] = f.control === 'number' ? Number(v) : v;
  }
  return { action: 'accept', content };
}

/** The host of a URL-mode elicitation's link, shown so the person sees where it goes; null if unparseable. */
export function linkHost(url: string | undefined): string | null {
  if (!url) return null;
  try {
    const parsed = new URL(url);
    if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') return null;
    return parsed.host;
  } catch {
    return null;
  }
}

/** The card's heading. */
export function promptHeading(payload: AgentPromptPayload, source: 'agent' | 'mcp_elicitation'): string {
  if (payload.kind === 'approval') return 'Approval needed';
  if (source === 'mcp_elicitation') return payload.server_name ? `${payload.server_name} is asking` : 'A connector is asking';
  return (payload.questions?.length ?? 0) > 1 ? 'A few questions' : 'A question';
}
