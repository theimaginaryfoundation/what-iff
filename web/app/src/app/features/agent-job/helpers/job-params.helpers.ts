import {
  AgentJobParameter,
  AgentJobParameterFormat,
  AgentJobParameterType,
  AgentJobParameterValue,
  AgentJobParameterValues,
} from '../../../core/models/agent-job.model';

/**
 * Job parameters (ADR 0x044), client side: the same rules as `internal/agentjobs/params`, so the
 * editor can show problems as you type. The server checks again on save.
 */

const PLACEHOLDER_RE = /\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}/g;
const NAME_RE = /^[A-Za-z_][A-Za-z0-9_]*$/;

export const MAX_PARAMETERS = 20;
export const MAX_OPTIONS = 50;
export const MAX_LIST_ITEMS = 50;

export const PARAMETER_TYPE_OPTIONS: ReadonlyArray<{ value: AgentJobParameterType; label: string; hint: string }> = [
  { value: 'text', label: 'Single input', hint: 'One free-text value' },
  { value: 'list', label: 'Multiple inputs', hint: 'Any number of free-text values' },
  { value: 'single_select', label: 'Single select', hint: 'One of a set of options' },
  { value: 'multi_select', label: 'Multi select', hint: 'One or more of a set of options' },
];

export function isMultiType(type: AgentJobParameterType): boolean {
  return type === 'multi_select' || type === 'list';
}

export function isSelectType(type: AgentJobParameterType): boolean {
  return type === 'single_select' || type === 'multi_select';
}

export function defaultFormat(type: AgentJobParameterType): AgentJobParameterFormat | undefined {
  if (type === 'multi_select') return 'comma';
  if (type === 'list') return 'bullets';
  return undefined;
}

export function isValidParameterName(name: string): boolean {
  return NAME_RE.test(name);
}

/** The distinct placeholder names in a prompt, in order of first use. */
export function promptPlaceholders(prompt: string): string[] {
  const names: string[] = [];
  for (const match of (prompt || '').matchAll(PLACEHOLDER_RE)) {
    if (!names.includes(match[1])) names.push(match[1]);
  }
  return names;
}

/** A definition as the server stores it: trimmed, label defaulting to the name, format only on multi types. */
export function normalizeDefinition(def: AgentJobParameter): AgentJobParameter {
  const name = (def.name || '').trim();
  const out: AgentJobParameter = { name, label: (def.label || '').trim() || name, type: def.type };
  const description = (def.description || '').trim();
  if (description) out.description = description;
  if (isSelectType(def.type)) {
    const options: string[] = [];
    for (const raw of def.options ?? []) {
      const option = raw.trim();
      if (option && !options.includes(option)) options.push(option);
    }
    out.options = options;
  }
  if (isMultiType(def.type)) out.format = def.format || defaultFormat(def.type);
  return out;
}

export interface ParameterProblem {
  /** The definition's index, or null for a problem with the whole list. */
  index: number | null;
  message: string;
}

export interface ParameterAnalysis {
  /** Placeholders in the prompt with no definition: errors. */
  undefinedPlaceholders: string[];
  /** Definitions no placeholder uses: notes, not errors. */
  unusedDefinitions: string[];
  /** Invalid definitions: errors. */
  problems: ParameterProblem[];
}

export function analyzeParameters(prompt: string, defs: AgentJobParameter[]): ParameterAnalysis {
  const placeholders = promptPlaceholders(prompt);
  const problems: ParameterProblem[] = [];
  if (defs.length > MAX_PARAMETERS) {
    problems.push({ index: null, message: `A job can have at most ${MAX_PARAMETERS} parameters.` });
  }
  const seen = new Set<string>();
  defs.forEach((def, index) => {
    const name = (def.name || '').trim();
    if (!isValidParameterName(name)) {
      problems.push({ index, message: 'Names start with a letter or underscore and use only letters, digits and underscores.' });
    } else if (seen.has(name)) {
      problems.push({ index, message: `"${name}" is defined more than once.` });
    }
    seen.add(name);
    if (isSelectType(def.type)) {
      const options = (def.options ?? []).map(option => option.trim()).filter(Boolean);
      if (options.length === 0) problems.push({ index, message: 'Add at least one option.' });
      if (new Set(options).size !== options.length) problems.push({ index, message: 'Options must be unique.' });
      if (options.length > MAX_OPTIONS) problems.push({ index, message: `At most ${MAX_OPTIONS} options.` });
    }
  });
  const defined = new Set(defs.map(def => (def.name || '').trim()));
  return {
    undefinedPlaceholders: placeholders.filter(name => !defined.has(name)),
    unusedDefinitions: defs.map(def => (def.name || '').trim()).filter(name => name && !placeholders.includes(name)),
    problems,
  };
}

export function hasParameterErrors(analysis: ParameterAnalysis): boolean {
  return analysis.undefinedPlaceholders.length > 0 || analysis.problems.length > 0;
}

/** Whether `value` is a usable value for `def` (ADR 0x044, decision 2). */
export function isValueSet(def: AgentJobParameter, value: AgentJobParameterValue | undefined): boolean {
  if (value === undefined || value === null) return false;
  const options = (def.options ?? []).map(option => option.trim());
  switch (def.type) {
    case 'text':
      return typeof value === 'string' && value.trim() !== '';
    case 'single_select':
      return typeof value === 'string' && options.includes(value.trim());
    case 'multi_select':
      return Array.isArray(value) && value.length > 0 && value.every(item => options.includes(item.trim()));
    case 'list':
      return Array.isArray(value) && value.some(item => item.trim() !== '');
  }
}

export function missingParameters(defs: AgentJobParameter[], values: AgentJobParameterValues): string[] {
  return defs.filter(def => !isValueSet(def, values[def.name])).map(def => def.name);
}

/** The values to save: only usable values of defined parameters, trimmed (what the server keeps). */
export function cleanParameterValues(defs: AgentJobParameter[], values: AgentJobParameterValues): AgentJobParameterValues {
  const out: AgentJobParameterValues = {};
  for (const def of defs) {
    const value = values[def.name];
    if (!isValueSet(def, value)) continue;
    if (Array.isArray(value)) {
      const items = value.map(item => item.trim()).filter(Boolean);
      out[def.name] = def.type === 'multi_select'
        ? (def.options ?? []).map(option => option.trim()).filter(option => items.includes(option))
        : items;
    } else {
      out[def.name] = (value as string).trim();
    }
  }
  return out;
}

/** Rewrites every `{{old}}` placeholder (spaces inside the braces allowed) to `{{new}}`. */
export function renamePlaceholder(prompt: string, oldName: string, newName: string): string {
  return (prompt || '').replace(PLACEHOLDER_RE, (whole, name: string) => (name === oldName ? `{{${newName}}}` : whole));
}

/** Labels for parameter names, for messages ("Topics and Region"). */
export function parameterLabels(defs: AgentJobParameter[], names: string[]): string[] {
  const byName = new Map(defs.map(def => [def.name, (def.label || '').trim() || def.name]));
  return names.map(name => byName.get(name) ?? name);
}

/** "A", "A and B", "A, B and C". */
export function joinNames(names: string[]): string {
  if (names.length <= 1) return names[0] ?? '';
  return `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`;
}

/** Stable comparison of definitions as the server would store them, for dirty checks. */
export function sameDefinitions(a: AgentJobParameter[] | undefined, b: AgentJobParameter[] | undefined): boolean {
  return JSON.stringify((a ?? []).map(normalizeDefinition)) === JSON.stringify((b ?? []).map(normalizeDefinition));
}

/** Stable comparison of values for dirty checks (key order ignored). */
export function sameValues(a: AgentJobParameterValues | undefined, b: AgentJobParameterValues | undefined): boolean {
  const canon = (values: AgentJobParameterValues | undefined) =>
    JSON.stringify(Object.keys(values ?? {}).sort().map(key => [key, values![key]]));
  return canon(a) === canon(b);
}
