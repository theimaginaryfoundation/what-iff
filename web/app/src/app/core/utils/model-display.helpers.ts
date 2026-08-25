import { Model } from '../models/model.model';

/**
 * Presentation helpers for `Model` — tier labels and sort orders.
 *
 * These read `Model` metadata (including its `subscription_tier`) purely to
 * format or order it for display; they carry no pricing logic and must not
 * depend on any. Keeping model display self-contained here is what lets the
 * model picker sort and label models without pulling in unrelated concerns.
 */

/**
 * Maps a model's tier to a display rank (1–4). Returns null for an unknown or
 * absent tier so callers can omit the badge rather than render a placeholder.
 */
export function modelTierRank(tier?: string | null): number | null {
  switch (tier?.toLowerCase()) {
    case 'low':
      return 1;
    case 'medium':
      return 2;
    case 'high':
      return 3;
    case 'ultra':
      return 4;
    default:
      return null;
  }
}

/** Long tier label, e.g. "Tier 3". Em dash when the tier is unknown. */
export function formatModelTierDisplay(tier?: string | null): string {
  const rank = modelTierRank(tier);
  if (rank == null) {
    return '—';
  }
  return `Tier ${rank}`;
}

/** Short tier name for badges and tables. */
export function modelTierName(tier?: string | null): string {
  switch (tier?.toLowerCase()) {
    case 'low':
      return 'Low';
    case 'medium':
      return 'Medium';
    case 'high':
      return 'High';
    case 'ultra':
      return 'Ultra';
    default:
      return '—';
  }
}

/** Compact tier label for tight UI (e.g. the model picker). Empty when unknown — omit the badge. */
export function modelTierCompactLabel(tier?: string | null): string {
  const rank = modelTierRank(tier);
  if (rank == null) {
    return '';
  }
  return `T${rank}`;
}

export function compareModelsByName(a: Model, b: Model): number {
  const nameA = (a.display_name || a.name || '').toLowerCase();
  const nameB = (b.display_name || b.name || '').toLowerCase();
  return nameA.localeCompare(nameB);
}

/** Models sorted alphabetically by display name. */
export function sortedModelsByName(models: readonly Model[]): Model[] {
  return [...models].sort(compareModelsByName);
}
