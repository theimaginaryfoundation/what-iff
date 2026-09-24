
import { ChangeDetectionStrategy, Component, computed, input, output, signal } from '@angular/core';

import { ToolCall, ToolCallStatus } from '../../../../core/models/toolcall.model';
import { formatToolPayload, friendlyToolName, summarizeToolCall } from '../../helpers/tool-call-format.helpers';


@Component({
  selector: 'app-tool-call',
  standalone: true,
  imports: [],
  template: `
    <article class="tool-call" [class.tool-call--grouped]="grouped()" [class.tool-call--running]="isRunning()">
      <button
        type="button"
        class="tool-call__toggle"
        [attr.aria-expanded]="expanded()"
        [attr.aria-controls]="panelId()"
        (click)="toggle()"
      >
        <span class="tool-call__head">
          <span
            class="tool-call__status-dot"
            [class.tool-call__status-dot--error]="hasError()"
            [class.tool-call__status-dot--running]="isRunning()"
            aria-hidden="true"
          ></span>
          <span class="tool-call__name">{{ displayName() }}</span>
          <span class="tool-call__summary">{{ summary() }}</span>
        </span>
        <span class="tool-call__side">
          <span class="tool-call__status" [attr.aria-live]="isRunning() ? 'polite' : null">{{ statusLabel() }}</span>
          <time class="tool-call__stamp" [attr.datetime]="toolCall().created_at">{{ shortTime(toolCall().created_at) }}</time>
          <span class="tool-call__chevron" [class.tool-call__chevron--open]="expanded()" aria-hidden="true">›</span>
        </span>
      </button>
      @if (expanded()) {
        <div class="tool-call__panel" [id]="panelId()">
          @if (formattedInput()) {
            <section class="tool-call__section">
              <h4 class="tool-call__section-label">Input</h4>
              <pre>{{ formattedInput() }}</pre>
            </section>
          }
          <section class="tool-call__section">
            <h4 class="tool-call__section-label">{{ hasError() ? 'Error' : 'Output' }}</h4>
            <pre>{{ formattedResult() }}</pre>
          </section>
          @if (!isRunning()) {
            <button type="button" class="tool-call__details" (click)="openDetail.emit(toolCall())">
              View details
            </button>
          }
        </div>
      }
    </article>
  `,
  styles: [`
    :host {
      display: block;
      width: 100%;
    }

    .tool-call {
      border: 1px solid var(--color-border-base);
      border-radius: 0.5rem;
      background: color-mix(in srgb, var(--color-surface-muted) 64%, var(--color-surface-base));
      overflow: hidden;
      margin-inline: auto;
      max-width: 35rem;
      width: 100%;
    }

    .tool-call--grouped {
      border: none;
      border-radius: 0;
      background: transparent;
      max-width: none;
    }

    .tool-call__toggle {
      align-items: flex-start;
      background: none;
      color: var(--color-text-primary);
      display: flex;
      justify-content: space-between;
      gap: 0.75rem;
      padding: 0.4375rem 0.75rem;
      text-align: left;
      width: 100%;
    }

    .tool-call__head,
    .tool-call__side {
      align-items: center;
      display: flex;
      min-width: 0;
    }

    .tool-call__head {
      flex: 1;
      gap: 0.5rem;
    }

    .tool-call__side {
      color: var(--color-text-muted);
      flex-shrink: 0;
      font-size: 0.625rem;
      gap: 0.5rem;
      font-family: 'Outfit', sans-serif;
    }

    .tool-call__status-dot {
      background: var(--color-success, #22c55e);
      border-radius: 999px;
      height: 0.375rem;
      width: 0.375rem;
    }

    .tool-call__status-dot--error {
      background: var(--color-danger, #ef4444);
    }

    .tool-call__status-dot--running {
      background: var(--color-accent);
      animation: tool-call-pulse 1.2s ease-in-out infinite;
    }

    @keyframes tool-call-pulse {
      0%, 100% { opacity: 1; transform: scale(1); }
      50% { opacity: 0.35; transform: scale(0.75); }
    }

    @media (prefers-reduced-motion: reduce) {
      .tool-call__status-dot--running { animation: none; }
    }

    .tool-call__name {
      color: var(--color-text-primary);
      font-size: 0.6875rem;
      font-weight: 600;
      text-transform: lowercase;
      white-space: nowrap;
    }

    .tool-call__summary {
      color: var(--color-text-muted);
      font-size: 0.6875rem;
      overflow-wrap: anywhere;
      white-space: normal;
    }

    .tool-call__status {
      font-size: 0.625rem;
    }

    .tool-call__chevron {
      display: inline-block;
      font-size: 0.875rem;
      line-height: 1;
      transform: rotate(0deg);
      transition: transform 150ms ease;
    }

    .tool-call__chevron--open {
      transform: rotate(90deg);
    }

    .tool-call__panel {
      display: grid;
      gap: 0.625rem;
      border-top: 1px solid var(--color-border-base);
      padding: 0.625rem 0.75rem 0.75rem;
    }

    .tool-call__section {
      display: grid;
      gap: 0.25rem;
      min-width: 0;
    }

    .tool-call__section-label {
      color: var(--color-text-muted);
      font-size: 0.625rem;
      font-weight: 600;
      letter-spacing: 0.04em;
      margin: 0;
      text-transform: uppercase;
    }

    pre {
      background: color-mix(in srgb, var(--color-surface-base) 70%, transparent);
      border-radius: 0.375rem;
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      max-height: 16rem;
      overflow-y: auto;
      padding: 0.5rem 0.625rem;
      color: var(--color-text-secondary);
      font-size: 0.75rem;
      margin: 0;
      overflow-x: auto;
      overflow-wrap: anywhere;
      word-break: break-word;
      white-space: pre-wrap;
    }

    .tool-call__details {
      justify-self: start;
      color: var(--color-accent);
      font-size: 0.6875rem;
      text-decoration: underline;
      text-underline-offset: 2px;
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ToolCallComponent {
  readonly toolCall = input.required<ToolCall>();
  readonly grouped = input(false);
  /** Set for live (in-flight) calls; saved calls derive complete/error from tool_error. */
  readonly status = input<ToolCallStatus | null>(null);
  readonly openDetail = output<ToolCall>();
  readonly expanded = signal(false);
  readonly resolvedStatus = computed<ToolCallStatus>(
    () => this.status() ?? (this.toolCall().tool_error?.trim() ? 'error' : 'complete'),
  );
  readonly isRunning = computed(() => this.resolvedStatus() === 'running');
  readonly hasError = computed(() => this.resolvedStatus() === 'error');
  readonly statusLabel = computed(() => STATUS_LABELS[this.resolvedStatus()]);
  readonly panelId = computed(() => `tool-call-${this.toolCall().id}`);
  readonly displayName = computed(() => friendlyToolName(this.toolCall().tool_name.trim()).toLowerCase());
  readonly summary = computed(() => summarizeToolCall(this.toolCall(), this.resolvedStatus()));
  readonly formattedInput = computed(() => formatToolPayload(this.toolCall().tool_input));
  readonly formattedResult = computed(() => {
    if (this.isRunning()) return 'Waiting for result…';
    const call = this.toolCall();
    return formatToolPayload(this.hasError() ? call.tool_error : call.tool_output) || 'No output';
  });

  toggle(): void {
    this.expanded.set(!this.expanded());
  }

  shortTime(value: string): string {
    return new Date(value).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  }
}

const STATUS_LABELS: Record<ToolCallStatus, string> = {
  running: 'Running…',
  complete: 'Complete',
  error: 'Failed',
};
