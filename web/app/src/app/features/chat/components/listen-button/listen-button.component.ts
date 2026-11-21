import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';

import { ChatMessage } from '../../../../core/models/message.model';
import { SpeechPlayerService } from '../../../../core/services/speech-player.service';
import { TooltipDirective } from '../../../../shared/ui/tooltip/tooltip.directive';

/**
 * Listen / Stop on a reply whose chat's personality has a voice (ADR 0x052 §7). Hidden
 * otherwise. It reads the reply's final round; the narration before tool calls stays unread.
 */
@Component({
  selector: 'app-listen-button',
  standalone: true,
  imports: [TooltipDirective],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (shown()) {
      <button
        type="button"
        class="listen"
        data-testid="listen-button"
        [class.listen--on]="active()"
        [attr.aria-pressed]="active()"
        [uiTooltip]="active() ? 'Stop reading aloud' : 'Read this reply aloud'"
        (click)="toggle()"
      >
        {{ loading() ? 'Loading…' : active() ? 'Stop' : 'Listen' }}
      </button>
      @if (error(); as text) {
        <span class="listen__error" role="alert" data-testid="listen-error">{{ text }}</span>
      }
    }
  `,
  styles: `
    :host {
      display: contents;
    }
    .listen {
      background: none;
      border: 0;
      color: inherit;
      cursor: pointer;
      font: inherit;
      padding: 0;
    }
    .listen:hover,
    .listen--on {
      color: var(--text-primary);
    }
    .listen__error {
      color: var(--color-danger);
      font-size: 0.72rem;
    }
  `,
})
export class ListenButtonComponent {
  private readonly player = inject(SpeechPlayerService);

  readonly message = input.required<ChatMessage>();

  readonly shown = computed(() => this.message().origin === 'Assistant' && this.player.chatHasVoice(this.message().chat_id));
  readonly loading = computed(() => this.player.loading() === this.message().id);
  readonly active = computed(() => this.loading() || this.player.playing() === this.message().id);
  readonly error = computed(() => {
    const e = this.player.error();
    return e && e.messageId === this.message().id ? e.text : null;
  });

  toggle(): void {
    if (this.active()) {
      this.player.stop();
    } else {
      void this.player.play(this.message().id);
    }
  }
}
