import { DatePipe, DecimalPipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';

import { ContextBreakdown, ContextSegmentStat } from '../../../../../core/models/message.model';

/** Display metadata for a model-context segment kind. Colors are fixed hues chosen to read
 *  on both light and dark themes; unknown kinds fall back to a neutral accent. */
interface KindMeta {
  label: string;
  description: string;
  color: string;
}

const KIND_META: Record<string, KindMeta> = {
  system_prompt: { label: 'System prompt', description: 'Base instructions + personality', color: 'hsl(221 70% 58%)' },
  checkpoint_summary: { label: 'Checkpoint summary', description: 'Compressed recap of earlier turns', color: 'hsl(199 75% 50%)' },
  scratchpad: { label: 'Scratchpad', description: 'The assistant’s working notes', color: 'hsl(48 85% 55%)' },
  memory_context: { label: 'Memories', description: 'Long-term memories retrieved for this turn', color: 'hsl(280 60% 62%)' },
  mood: { label: 'Mood', description: 'Active mood snippet', color: 'hsl(330 70% 62%)' },
  history_turn: { label: 'History', description: 'Recent verbatim conversation turns', color: 'hsl(162 62% 45%)' },
  attachment_context: { label: 'Attachments', description: 'Labels + content from attached files', color: 'hsl(24 80% 56%)' },
  tool_result: { label: 'Tool results', description: 'Tool calls and outputs carried in context', color: 'hsl(14 72% 58%)' },
  developer_context: { label: 'Developer notes', description: 'Extra per-turn instructions', color: 'hsl(210 12% 55%)' },
  tool_definitions: { label: 'Tool definitions', description: 'Function schemas available for this turn', color: 'hsl(262 52% 60%)' },
  vendor_prompt_other: { label: 'Vendor prompt / other', description: 'Vendor prompt, image tokens, and anything else we cannot accurately attribute', color: 'hsl(210 12% 55%)' },
  user_message: { label: 'Your message', description: 'The current user turn', color: 'hsl(146 55% 50%)' },
};

const FALLBACK_META: KindMeta = { label: 'Other', description: 'Additional context', color: 'var(--color-accent)' };

interface BreakdownRow {
  kind: string;
  label: string;
  description: string;
  color: string;
  tokens: number;
  segments: number;
  cacheable: boolean;
  images: number;
  /** Share of the total context, 0..100. */
  sharePct: number;
  /** Width against the token budget, 0..100 (what the stacked meter uses). */
  budgetPct: number;
}

@Component({
  selector: 'app-context-breakdown-tab',
  standalone: true,
  imports: [DatePipe, DecimalPipe],
  template: `
    <section class="tab-body" aria-label="Context breakdown">
      @if (breakdown(); as b) {
        @if (rows().length === 0) {
          <p class="state">No context snapshot for this turn yet.</p>
        } @else {
          <header class="xray-head">
            <span class="xray-head__label">Turn context</span>
            @if (b.captured_at) {
              <time class="xray-head__time" [attr.datetime]="b.captured_at">{{ b.captured_at | date: 'MMM d, h:mm a' }}</time>
            }
          </header>
          <div class="gauge">
            <div class="gauge__top">
              <span class="gauge__total">{{ format(total()) }}</span>
              <span class="gauge__budget">/ {{ format(displayBudget()) }} tokens</span>
            </div>
            <div
              class="gauge__track"
              role="img"
              [attr.aria-label]="ariaSummary()"
              [class.gauge__track--over]="overBudget()"
            >
              @for (row of rows(); track row.kind) {
                <span
                  class="gauge__slice"
                  [style.width.%]="row.budgetPct"
                  [style.background]="row.color"
                  [title]="row.label"
                ></span>
              }
            </div>
            <div class="gauge__scale">
              <span>{{ fillPct() }}% full</span>
              @if (overBudget()) {
                <span class="gauge__warn">Heavy turns can exceed this budget until the next compaction.</span>
              } @else {
                <span>{{ format(remaining()) }} free</span>
              }
            </div>
          </div>

          <ul class="legend">
            @for (row of rows(); track row.kind) {
              <li class="legend__row">
                <span class="legend__swatch" [style.background]="row.color"></span>
                <div class="legend__main">
                  <div class="legend__head">
                    <span class="legend__label">{{ row.label }}</span>
                    <span class="legend__percent">{{ row.sharePct | number: '1.0-0' }}%</span>
                  </div>
                  <div class="legend__bar">
                    <span class="legend__bar-fill" [style.width.%]="row.sharePct" [style.background]="row.color"></span>
                  </div>
                  <div class="legend__meta">
                    <span>{{ row.segments }} {{ row.segments === 1 ? 'segment' : 'segments' }}</span>
                    @if (row.images > 0) {
                      <span>·</span>
                      <span>{{ row.images }} img</span>
                    }
                    @if (row.cacheable) {
                      <span>·</span>
                      <span class="legend__cache" title="Part of the cacheable prompt prefix">cached</span>
                    }
                  </div>
                  <p class="legend__desc">{{ row.description }}</p>
                </div>
              </li>
            }
          </ul>

          <footer class="foot">
            @if (b.model) {
              <span class="foot__model">{{ b.model }}</span>
            }
            <span class="foot__note">Total reflects vendor-reported input usage when available; named buckets are estimates.</span>
          </footer>
        }
      } @else {
        <p class="state">
          Send a message to see what fills the model’s working memory for a turn.
        </p>
      }
    </section>
  `,
  styles: [`
    .tab-body { display: grid; gap: 0.9rem; }

    .state {
      color: var(--color-text-muted);
      font-size: 0.8rem;
      margin: 0;
    }

    .xray-head {
      align-items: baseline;
      border-bottom: 1px solid var(--color-border-base);
      display: flex;
      gap: 0.5rem;
      justify-content: space-between;
      padding-bottom: 0.5rem;
    }
    .xray-head__label {
      color: var(--color-text-muted);
      font-size: 0.625rem;
      font-weight: 700;
      letter-spacing: 0.06em;
      text-transform: uppercase;
    }
    .xray-head__time { color: var(--color-text-secondary); font-size: 0.72rem; font-variant-numeric: tabular-nums; }

    .gauge { display: grid; gap: 0.4rem; }

    .gauge__top { align-items: baseline; display: flex; gap: 0.35rem; }
    .gauge__total { color: var(--color-text-primary); font-size: 1.35rem; font-weight: 700; }
    .gauge__budget { color: var(--color-text-muted); font-size: 0.8rem; }

    .gauge__track {
      background: var(--color-surface-base);
      border: 1px solid var(--color-border-base);
      border-radius: 0.5rem;
      display: flex;
      height: 0.85rem;
      overflow: hidden;
      width: 100%;
    }
    .gauge__track--over { box-shadow: 0 0 0 1px var(--color-warning, #d97706) inset; }
    .gauge__slice { display: block; height: 100%; }
    .gauge__slice:not(:last-child) { border-right: 1px solid color-mix(in srgb, var(--color-surface-base) 55%, transparent); }

    .gauge__scale {
      color: var(--color-text-muted);
      display: flex;
      font-size: 0.72rem;
      justify-content: space-between;
    }
    .gauge__warn { color: var(--color-warning, #d97706); font-weight: 600; }

    .legend { display: grid; gap: 0.7rem; list-style: none; margin: 0; padding: 0; }
    .legend__row { display: grid; gap: 0.35rem; grid-template-columns: 0.6rem 1fr; }
    .legend__swatch { border-radius: 0.2rem; height: 0.6rem; margin-top: 0.28rem; width: 0.6rem; }
    .legend__main { display: grid; gap: 0.25rem; min-width: 0; }

    .legend__head { align-items: baseline; display: flex; gap: 0.5rem; justify-content: space-between; }
    .legend__label { color: var(--color-text-primary); font-size: 0.83rem; font-weight: 600; }
    .legend__percent { color: var(--color-text-secondary); font-size: 0.8rem; font-variant-numeric: tabular-nums; }
    .legend__bar {
      background: var(--color-surface-base);
      border-radius: 999px;
      height: 0.3rem;
      overflow: hidden;
      width: 100%;
    }
    .legend__bar-fill { border-radius: 999px; display: block; height: 100%; min-width: 2px; }

    .legend__meta {
      align-items: center;
      color: var(--color-text-muted);
      display: flex;
      flex-wrap: wrap;
      font-size: 0.72rem;
      gap: 0.3rem;
    }
    .legend__cache {
      background: color-mix(in srgb, var(--color-accent) 16%, transparent);
      border-radius: 0.35rem;
      color: var(--color-accent);
      padding: 0 0.3rem;
    }
    .legend__desc { color: var(--color-text-muted); font-size: 0.74rem; margin: 0; }

    .foot {
      border-top: 1px solid var(--color-border-base);
      color: var(--color-text-muted);
      display: grid;
      font-size: 0.72rem;
      gap: 0.2rem;
      padding-top: 0.6rem;
    }
    .foot__model { color: var(--color-text-secondary); font-weight: 600; }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ContextBreakdownTabComponent {
  readonly breakdown = input<ContextBreakdown | null>(null);

  readonly total = computed(() => {
    const b = this.breakdown();
    if (!b) return 0;
    if (typeof b.total_tokens === 'number' && b.total_tokens > 0) return b.total_tokens;
    return this.sumSegments(b.segments);
  });

  readonly budget = computed(() => {
    const b = this.breakdown();
    const declared = b?.budget_tokens ?? 0;
    // Never let the denominator fall below the actual usage, so the meter stays truthful.
    return Math.max(declared, this.total(), 1);
  });

  /** The configured compaction ceiling, kept visible even when the gauge expands for overflow. */
  readonly displayBudget = computed(() => {
    const declared = this.breakdown()?.budget_tokens ?? 0;
    return declared > 0 ? declared : this.budget();
  });

  readonly remaining = computed(() => Math.max(this.budget() - this.total(), 0));
  readonly overBudget = computed(() => this.total() > (this.breakdown()?.budget_tokens ?? Infinity));
  readonly fillPct = computed(() => Math.round(Math.min(this.total() / this.budget(), 1) * 100));

  readonly rows = computed<BreakdownRow[]>(() => {
    const b = this.breakdown();
    if (!b || !Array.isArray(b.segments)) return [];
    const total = this.total() || 1;
    const budget = this.budget() || 1;
    const merged = new Map<string, ContextSegmentStat>();
    for (const segment of b.segments) {
      if (!segment || segment.tokens <= 0) continue;
      const kind = segment.kind === 'expression_portrait' ? 'developer_context' : segment.kind;
      const existing = merged.get(kind);
      if (existing) {
        existing.tokens += segment.tokens;
        existing.segments += segment.segments ?? 0;
        existing.cacheable ||= !!segment.cacheable;
        existing.images = (existing.images ?? 0) + (segment.images ?? 0);
      } else {
        merged.set(kind, { ...segment, kind });
      }
    }
    return [...merged.values()]
      .map(seg => this.toRow(seg, total, budget))
      .sort((a, b2) => b2.tokens - a.tokens);
  });

  ariaSummary(): string {
    const parts = this.rows().map(r => `${r.label} ${r.sharePct.toFixed(0)}%`);
    return `Context is ${this.fillPct()}% of budget: ${parts.join(', ')}.`;
  }

  format(n: number): string {
    return Math.round(n).toLocaleString();
  }

  private toRow(seg: ContextSegmentStat, total: number, budget: number): BreakdownRow {
    const meta = KIND_META[seg.kind] ?? FALLBACK_META;
    return {
      kind: seg.kind,
      label: meta.label,
      description: meta.description,
      color: meta.color,
      tokens: seg.tokens,
      segments: seg.segments ?? 0,
      cacheable: !!seg.cacheable,
      images: seg.images ?? 0,
      sharePct: (seg.tokens / total) * 100,
      budgetPct: Math.min((seg.tokens / budget) * 100, 100),
    };
  }

  private sumSegments(segments: ContextSegmentStat[] | undefined): number {
    if (!Array.isArray(segments)) return 0;
    return segments.reduce((acc, s) => acc + (s?.tokens ?? 0), 0);
  }
}
