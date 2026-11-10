/**
 * What a conversion could not carry (ADR 0x043, office suite). Every converter returns one with
 * its data; the workspace shows it before the first save of a document with losses (§7).
 */
export interface FidelityReport {
  /** Features present in the source that the model does not keep; shown to the user. */
  readonly dropped: readonly FidelityItem[];
  /** Things that were kept but approximated. */
  readonly warnings: readonly FidelityItem[];
}

export interface FidelityItem {
  /** A short label such as "Charts" or "Pivot tables". */
  readonly feature: string;
  /** Where, such as a sheet name or a slide number. */
  readonly where?: string;
  /** How many occurrences, when counted. */
  readonly count?: number;
}

export class FidelityCollector {
  private readonly droppedItems = new Map<string, FidelityItem & { count: number }>();
  private readonly warningItems = new Map<string, FidelityItem & { count: number }>();

  drop(feature: string, where?: string): void {
    add(this.droppedItems, feature, where);
  }

  warn(feature: string, where?: string): void {
    add(this.warningItems, feature, where);
  }

  get hasLosses(): boolean {
    return this.droppedItems.size > 0;
  }

  report(): FidelityReport {
    return { dropped: [...this.droppedItems.values()], warnings: [...this.warningItems.values()] };
  }
}

function add(map: Map<string, FidelityItem & { count: number }>, feature: string, where?: string): void {
  const key = `${feature}|${where ?? ''}`;
  const existing = map.get(key);
  if (existing) existing.count += 1;
  else map.set(key, { feature, where, count: 1 });
}

/** One line per dropped feature, for a dialog: "Charts (Sales, 2), Pivot tables". */
export function describeLosses(report: FidelityReport): string[] {
  return report.dropped.map(item => {
    const parts = [item.where, item.count && item.count > 1 ? String(item.count) : undefined].filter(Boolean);
    return parts.length ? `${item.feature} (${parts.join(', ')})` : item.feature;
  });
}
