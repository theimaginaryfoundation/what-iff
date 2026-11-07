import {
  AgentPromptPayload,
  AgentPromptQuestion,
  AgentPromptResponse,
  ElicitationProperty,
  ElicitationSchema,
} from '../../../../core/models/job.model';

/** One answer being written: the options picked and the person's own text. */
export interface QuestionDraft {
  selected: string[];
  text: string;
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

export function emptyDrafts(questions: readonly AgentPromptQuestion[] | undefined): Record<string, QuestionDraft> {
  const out: Record<string, QuestionDraft> = {};
  for (const q of questions ?? []) out[q.id] = { selected: [], text: '' };
  return out;
}

/** Toggles an option: single-select questions replace the pick (and clear own text), multi add/remove. */
export function toggleOption(draft: QuestionDraft, question: AgentPromptQuestion, label: string): QuestionDraft {
  if (question.multi_select) {
    const selected = draft.selected.includes(label)
      ? draft.selected.filter(s => s !== label)
      : [...draft.selected, label];
    return { ...draft, selected };
  }
  if (draft.selected[0] === label) return { ...draft, selected: [] };
  return { selected: [label], text: '' };
}

/** Typing an own answer on a single-select question replaces the picked option. */
export function setOwnText(draft: QuestionDraft, question: AgentPromptQuestion, text: string): QuestionDraft {
  if (!question.multi_select && text.trim()) return { selected: [], text };
  return { ...draft, text };
}

export function questionAnswered(draft: QuestionDraft | undefined): boolean {
  return !!draft && (draft.selected.length > 0 || draft.text.trim().length > 0);
}

/** The accept response for a questions prompt; unanswered questions are left out (skipped). */
export function questionsResponse(drafts: Record<string, QuestionDraft>): AgentPromptResponse {
  const answers: AgentPromptResponse['answers'] = {};
  for (const [id, draft] of Object.entries(drafts)) {
    if (!questionAnswered(draft)) continue;
    answers[id] = {
      ...(draft.selected.length ? { selected: draft.selected } : {}),
      ...(draft.text.trim() ? { text: draft.text.trim() } : {}),
    };
  }
  return { action: 'accept', answers };
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
