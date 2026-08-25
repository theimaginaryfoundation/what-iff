
import { ChangeDetectionStrategy, Component, input, output, signal } from '@angular/core';

import { ToolCall } from '../../../../core/models/toolcall.model';
import { ToolCallComponent } from '../tool-call/tool-call.component';

@Component({
  selector: 'app-tool-call-group',
  standalone: true,
  imports: [ToolCallComponent],
  template: `
    <section class="tool-call-group" aria-label="Tool calls">
      @if (hasMultipleCalls()) {
        <button
          type="button"
          class="tool-call-group__toggle"
          [attr.aria-expanded]="expanded()"
          [attr.aria-controls]="panelId"
          (click)="toggle()"
        >
          <span class="tool-call-group__title">{{ toolCalls().length }} tool {{ toolCalls().length === 1 ? 'call' : 'calls' }}</span>
          <span class="tool-call-group__chevron" [class.tool-call-group__chevron--open]="expanded()" aria-hidden="true">›</span>
        </button>
        @if (expanded()) {
          <div class="tool-call-group__items" [id]="panelId">
            @for (toolCall of toolCalls(); track toolCall.id) {
              <app-tool-call [toolCall]="toolCall" [grouped]="true" (openDetail)="openDetail.emit($event)" />
            }
          </div>
        }
      } @else {
        @for (toolCall of toolCalls(); track toolCall.id) {
          <app-tool-call [toolCall]="toolCall" (openDetail)="openDetail.emit($event)" />
        }
      }
    </section>
  `,
  styles: [`
    .tool-call-group {
      align-items: center;
      display: flex;
      flex-direction: column;
      padding: 0.125rem 0;
    }

    .tool-call-group__toggle,
    .tool-call-group__items {
      border: 1px solid var(--color-border-base);
      width: min(100%, 35rem);
    }

    .tool-call-group__toggle {
      align-items: center;
      background: color-mix(in srgb, var(--color-surface-muted) 64%, var(--color-surface-base));
      border-radius: 0.5rem;
      color: var(--color-text-muted);
      display: flex;
      font-size: 0.6875rem;
      font-weight: 600;
      justify-content: space-between;
      letter-spacing: 0.02em;
      padding: 0.4375rem 0.75rem;
    }

    .tool-call-group__items {
      background: color-mix(in srgb, var(--color-surface-muted) 64%, var(--color-surface-base));
      border-top: none;
      border-radius: 0 0 0.5rem 0.5rem;
      display: grid;
      grid-template-columns: minmax(0, 1fr);
      overflow: hidden;
    }

    .tool-call-group__items app-tool-call + app-tool-call {
      border-top: 1px solid var(--color-border-base);
    }

    .tool-call-group__items app-tool-call {
      min-width: 0;
    }

    .tool-call-group__title {
      text-transform: lowercase;
    }

    .tool-call-group__chevron {
      display: inline-block;
      font-size: 0.875rem;
      line-height: 1;
      transform: rotate(0deg);
      transition: transform 150ms ease;
    }

    .tool-call-group__chevron--open {
      transform: rotate(90deg);
    }

    .tool-call-group__items {
      margin-top: -1px;
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ToolCallGroupComponent {
  readonly toolCalls = input.required<readonly ToolCall[]>();
  readonly openDetail = output<ToolCall>();
  readonly expanded = signal(false);
  readonly panelId = `tool-call-group-${Math.random().toString(36).slice(2)}`;

  hasMultipleCalls(): boolean {
    return this.toolCalls().length > 1;
  }

  toggle(): void {
    this.expanded.set(!this.expanded());
  }
}
