import { ChangeDetectionStrategy, Component, computed, input, output, signal } from '@angular/core';

import { ToolCall, ToolCallView } from '../../../../core/models/toolcall.model';
import { ToolCallComponent } from '../tool-call/tool-call.component';
import { TooltipDirective } from '../../../../shared/ui/tooltip/tooltip.directive';

@Component({
  selector: 'app-tool-call-group',
  standalone: true,
  imports: [ToolCallComponent, TooltipDirective],
  template: `
    <section class="tool-call-group" [attr.aria-label]="live() ? 'Tool calls in progress' : 'Tool calls'">
      @if (hasMultipleCalls()) {
        <button
          type="button"
          class="tool-call-group__toggle"
          [attr.aria-expanded]="showItems()"
          [attr.aria-controls]="panelId"
          [uiTooltip]="showItems() ? 'Hide the tools used for this reply' : 'Show the tools used for this reply'"
          (click)="toggle()"
        >
          <span class="tool-call-group__title">{{ title() }}</span>
          <span class="tool-call-group__chevron" [class.tool-call-group__chevron--open]="showItems()" aria-hidden="true">›</span>
        </button>
        @if (showItems()) {
          <div class="tool-call-group__items" [id]="panelId">
            @for (toolCall of toolCalls(); track toolCall.id) {
              <app-tool-call [toolCall]="toolCall" [status]="toolCall.status ?? null" [grouped]="true" (openDetail)="openDetail.emit($event)" />
            }
          </div>
        }
      } @else {
        @for (toolCall of toolCalls(); track toolCall.id) {
          <app-tool-call [toolCall]="toolCall" [status]="toolCall.status ?? null" (openDetail)="openDetail.emit($event)" />
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
  readonly toolCalls = input.required<readonly ToolCallView[]>();
  /** The in-flight turn's timeline: shown open (unless collapsed) so progress is visible. */
  readonly live = input(false);
  readonly openDetail = output<ToolCall>();
  /** User override of the default open state (live groups default open, saved ones closed). */
  private readonly expandedOverride = signal<boolean | null>(null);
  readonly showItems = computed(() => this.expandedOverride() ?? this.live());
  readonly panelId = `tool-call-group-${Math.random().toString(36).slice(2)}`;
  readonly title = computed(() => {
    const count = this.toolCalls().length;
    const noun = `${count} tool ${count === 1 ? 'call' : 'calls'}`;
    const running = this.toolCalls().some(call => call.status === 'running');
    return this.live() && running ? `working · ${noun}` : noun;
  });

  hasMultipleCalls(): boolean {
    return this.toolCalls().length > 1;
  }

  toggle(): void {
    this.expandedOverride.set(!this.showItems());
  }
}
