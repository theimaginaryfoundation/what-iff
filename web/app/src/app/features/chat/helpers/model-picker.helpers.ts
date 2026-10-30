import { Model } from '../../../core/models/model.model';
import { compareModelsByName, modelTierRank } from '../../../core/utils/model-display.helpers';

export const MODEL_PROVIDER_ORDER = ['openai', 'anthropic', 'zai', 'google', 'mistral', 'deepseek', 'qwen', 'xiaomi'] as const;

/** A tier bucket for the "by tier" view: rank 1–4, or null for untiered models. */
export interface ModelTierGroup {
  /** Numeric tier rank (1–4), or null when the model has no known tier. */
  rank: number | null;
  /** Short heading label, e.g. "Tier 2" or "Other". */
  label: string;
  models: Model[];
}

/**
 * Groups models into ordered tier buckets (T1→T4, untiered last), each sorted
 * by display name. Empty buckets are omitted so the picker only renders tiers
 * the user actually has access to.
 */
export function groupModelsByTier(models: readonly Model[]): ModelTierGroup[] {
  const byRank = new Map<number | null, Model[]>();
  for (const model of models) {
    const rank = modelTierRank(model.subscription_tier);
    const existing = byRank.get(rank);
    if (existing) {
      existing.push(model);
    } else {
      byRank.set(rank, [model]);
    }
  }

  const groups: ModelTierGroup[] = [];
  for (const rank of [1, 2, 3, 4]) {
    const bucket = byRank.get(rank);
    if (bucket?.length) {
      groups.push({ rank, label: `Tier ${rank}`, models: [...bucket].sort(compareModelsByName) });
    }
  }
  const untiered = byRank.get(null);
  if (untiered?.length) {
    groups.push({ rank: null, label: 'Other', models: [...untiered].sort(compareModelsByName) });
  }
  return groups;
}

const PROVIDER_LABELS: Record<string, string> = {
  openai: 'GPT',
  anthropic: 'Claude',
  zai: 'GLM',
  google: 'Gemini',
  mistral: 'Mistral',
  deepseek: 'DeepSeek',
  qwen: 'Qwen',
  xiaomi: 'MiMo',
  // Models enabled on an account's own connections (ADR 0x027), by the preset they came from.
  openrouter: 'OpenRouter',
  ollama: 'Ollama',
  lmstudio: 'LM Studio',
  custom: 'Custom',
  other: 'Other',
};

/** Resolve the backend provider field, with a legacy name fallback when absent. */
export function modelProvider(model: Model): string {
  const provider = model.provider?.trim().toLowerCase();
  if (provider) {
    return provider;
  }
  return legacyProviderFromName(model);
}

export function providerLabel(provider: string): string {
  const normalized = provider.trim().toLowerCase();
  return PROVIDER_LABELS[normalized] ?? titleCaseProvider(normalized);
}

export function sortedProviders(providers: Iterable<string>): string[] {
  const knownSet = new Set<string>(MODEL_PROVIDER_ORDER);
  const unique = [...new Set([...providers].map(provider => provider.trim().toLowerCase()).filter(Boolean))];
  const known = MODEL_PROVIDER_ORDER.filter(provider => unique.includes(provider));
  const rest = unique.filter(provider => !knownSet.has(provider)).sort();
  return [...known, ...rest];
}

function legacyProviderFromName(model: Model): string {
  const name = (model.name || '').toLowerCase();
  const display = (model.display_name || '').toLowerCase();
  if (name.startsWith('claude') || display.startsWith('claude')) {
    return 'anthropic';
  }
  if (name.startsWith('glm') || display.startsWith('glm')) {
    return 'zai';
  }
  if (name.startsWith('gemini') || display.startsWith('gemini')) {
    return 'google';
  }
  if (
    name.startsWith('gpt') ||
    display.startsWith('gpt') ||
    name.startsWith('o1') ||
    name.startsWith('o3') ||
    name.startsWith('o4')
  ) {
    return 'openai';
  }
  return 'other';
}

function titleCaseProvider(provider: string): string {
  if (!provider) {
    return PROVIDER_LABELS['other'];
  }
  return provider
    .split(/[-_\s]+/)
    .filter(Boolean)
    .map(part => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

/**
 * A group in the picker's vendor view. The built-in providers each have one,
 * holding the deployment's models and the account's own from its connection to
 * that provider, since they go through the same path and key. Every other
 * connection (OpenRouter, a server on this computer, Custom) is its own group,
 * named as the account named it, so two servers of the same kind stay apart
 * (ADR 0x027).
 */
export interface ModelGroup {
  key: string;
  label: string;
}

const BUILTIN_PROVIDERS = new Set<string>(MODEL_PROVIDER_ORDER);

/** Whether a model belongs to one of the account's own connections other than a built-in provider's. */
export function isConnectionGroup(model: Model): boolean {
  return !!model.connection_id && !BUILTIN_PROVIDERS.has(modelProvider(model));
}

/** The key of the group a model is listed under. */
export function modelGroupKey(model: Model): string {
  return isConnectionGroup(model) ? `connection:${model.connection_id}` : modelProvider(model);
}

/** The name of the group a model is listed under. */
export function modelGroupLabel(model: Model): string {
  return isConnectionGroup(model) && model.connection_name ? model.connection_name : providerLabel(modelProvider(model));
}

/**
 * The groups models fall into: the built-in providers in their usual order,
 * then any other provider, then the account's connections, by name.
 */
export function sortedGroups(models: readonly Model[]): ModelGroup[] {
  const byKey = new Map<string, ModelGroup>();
  for (const model of models) {
    const key = modelGroupKey(model);
    if (!byKey.has(key)) {
      byKey.set(key, { key, label: modelGroupLabel(model) });
    }
  }
  const groups = [...byKey.values()];
  const providerOrder = sortedProviders(groups.filter(g => !g.key.startsWith('connection:')).map(g => g.key));
  const providers = providerOrder.map(key => byKey.get(key) as ModelGroup);
  const connections = groups
    .filter(g => g.key.startsWith('connection:'))
    .sort((a, b) => a.label.localeCompare(b.label, undefined, { sensitivity: 'base' }));
  return [...providers, ...connections];
}
