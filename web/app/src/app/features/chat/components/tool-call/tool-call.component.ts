
import { ChangeDetectionStrategy, Component, computed, input, output, signal } from '@angular/core';

import { ToolCall } from '../../../../core/models/toolcall.model';

@Component({
  selector: 'app-tool-call',
  standalone: true,
  imports: [],
  template: `
    <article class="tool-call" [class.tool-call--grouped]="grouped()">
      <button
        type="button"
        class="tool-call__toggle"
        [attr.aria-expanded]="expanded()"
        [attr.aria-controls]="panelId()"
        (click)="toggle()"
      >
        <span class="tool-call__head">
          <span class="tool-call__status-dot" [class.tool-call__status-dot--error]="hasError()" aria-hidden="true"></span>
          <span class="tool-call__name">{{ displayName() }}</span>
          <span class="tool-call__summary">{{ summary() }}</span>
        </span>
        <span class="tool-call__side">
          <span class="tool-call__status">{{ hasError() ? 'Failed' : 'Complete' }}</span>
          <time class="tool-call__stamp" [attr.datetime]="toolCall().created_at">{{ shortTime(toolCall().created_at) }}</time>
          <span class="tool-call__chevron" [class.tool-call__chevron--open]="expanded()" aria-hidden="true">›</span>
        </span>
      </button>
      @if (expanded()) {
        <div class="tool-call__panel" [id]="panelId()">
          <pre>{{ toolCall().tool_output || toolCall().tool_error || 'No output' }}</pre>
          <button type="button" class="tool-call__details" (click)="openDetail.emit(toolCall())">
            View details
          </button>
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

    pre {
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
  readonly openDetail = output<ToolCall>();
  readonly expanded = signal(false);
  readonly hasError = computed(() => Boolean(this.toolCall().tool_error?.trim()));
  readonly panelId = computed(() => `tool-call-${this.toolCall().id}`);
  readonly displayName = computed(() => this.toolCall().tool_name.trim().toLowerCase());
  readonly summary = computed(() => summarizeToolCall(this.toolCall()));

  toggle(): void {
    this.expanded.set(!this.expanded());
  }

  shortTime(value: string): string {
    return new Date(value).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  }
}

function summarizeToolCall(toolCall: ToolCall): string {
  const output = toolCall.tool_output?.trim();
  if (output) return output.replace(/\s+/g, ' ').slice(0, 120);

  const error = toolCall.tool_error?.trim();
  if (error) return error.replace(/\s+/g, ' ').slice(0, 120);

  const input = toolCall.tool_input?.trim();
  if (input) return input.replace(/\s+/g, ' ').slice(0, 120);

  return 'No summary';
}
