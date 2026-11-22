import { ChangeDetectionStrategy, Component, ElementRef, Injector, afterNextRender, computed, inject, input, signal } from '@angular/core';

import { VoiceCostOutletComponent } from '../../../../extensions/voice-cost-outlet.component';
import { SpeechAutoplayService } from '../../../../core/services/speech-autoplay.service';
import { SpeechPlayerService } from '../../../../core/services/speech-player.service';
import { RadioComponent } from '../../../../shared/ui/radio/radio.component';
import { TooltipDirective } from '../../../../shared/ui/tooltip/tooltip.directive';

type Choice = 'default' | 'on' | 'off';

let nextId = 0;

/**
 * The chat's "read replies aloud" setting (ADR 0x052 §9): follow the personality, always, or
 * never. Shown only when the chat's personality has a voice. Also offers "Tap to listen" for a
 * reply waiting on a tap before the browser lets it play.
 */
@Component({
  selector: 'app-autoplay-toggle',
  standalone: true,
  imports: [RadioComponent, TooltipDirective, VoiceCostOutletComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { '(document:click)': 'onDocumentClick($event)', '(document:keydown.escape)': 'onEscape($event)' },
  template: `
    @if (shown()) {
      <span class="autoplay">
        @if (waiting()) {
          <button type="button" class="autoplay__tap" data-testid="autoplay-tap" (click)="tap()">▶ Tap to listen</button>
        }
        <button
          type="button"
          class="autoplay__button"
          data-testid="autoplay-toggle"
          [class.autoplay__button--on]="on()"
          aria-haspopup="true"
          [attr.aria-expanded]="open()"
          [attr.aria-controls]="open() ? menuId : null"
          [attr.aria-label]="on() ? 'Replies are read aloud as they arrive' : 'Replies are not read aloud'"
          [uiTooltip]="on() ? 'Replies are read aloud as they arrive' : 'Read replies aloud as they arrive'"
          (click)="toggle()"
        >
          <svg
            width="14"
            height="14"
            viewBox="0 0 16 16"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path d="M2.5 6v4h2.5l3.5 3V3L5 6z" />
            @if (on()) {
              <path d="M11 5.5a3.5 3.5 0 0 1 0 5M12.8 3.8a6 6 0 0 1 0 8.4" />
            } @else {
              <path d="M11 6l3.5 4M14.5 6L11 10" />
            }
          </svg>
        </button>
        @if (open()) {
          <span class="autoplay__menu" role="radiogroup" aria-label="Read replies aloud" data-testid="autoplay-menu" [id]="menuId">
            <span class="autoplay__title">Read replies aloud</span>
            @for (c of choices(); track c.value) {
              <label class="autoplay__choice">
                <ui-radio [name]="menuId" [value]="c.value" [checked]="choice() === c.value" (selected)="pick(c.value)" />
                {{ c.label }}
              </label>
            }
            <app-voice-cost-outlet place="autoplay" />
            @if (error()) {
              <span class="autoplay__error" role="alert">{{ error() }}</span>
            }
          </span>
        }
      </span>
    }
  `,
  styles: `
    :host {
      display: contents;
    }
    .autoplay {
      align-items: center;
      display: inline-flex;
      gap: 0.35rem;
      position: relative;
    }
    .autoplay__button {
      align-items: center;
      background: transparent;
      border: 1px solid var(--border);
      border-radius: 0.4375rem;
      color: var(--text-secondary);
      cursor: pointer;
      display: inline-flex;
      height: 1.875rem;
      justify-content: center;
      width: 1.875rem;
    }
    .autoplay__button--on {
      color: var(--color-accent);
    }
    .autoplay__tap {
      background: var(--color-accent);
      border: 0;
      border-radius: 999px;
      color: white;
      cursor: pointer;
      font-size: 0.75rem;
      padding: 0.2rem 0.6rem;
    }
    .autoplay__menu {
      background: var(--bg-secondary);
      border: 1px solid var(--border);
      border-radius: 0.5rem;
      bottom: calc(100% + 0.4rem);
      box-shadow: 0 6px 20px rgb(0 0 0 / 0.18);
      display: flex;
      flex-direction: column;
      gap: 0.35rem;
      padding: 0.6rem 0.75rem;
      position: absolute;
      right: 0;
      width: max-content;
      z-index: 6;
    }
    .autoplay__title {
      color: var(--text-primary);
      font-size: 0.8rem;
      font-weight: 600;
    }
    .autoplay__choice {
      align-items: center;
      color: var(--text-primary);
      cursor: pointer;
      display: flex;
      font-size: 0.8rem;
      gap: 0.4rem;
    }
    .autoplay__error {
      color: var(--color-danger);
      font-size: 0.75rem;
    }
  `,
})
export class AutoplayToggleComponent {
  private readonly autoplay = inject(SpeechAutoplayService);
  private readonly player = inject(SpeechPlayerService);

  readonly chatId = input<string | null>(null);

  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly injector = inject(Injector);

  readonly open = signal(false);
  readonly error = signal('');
  /** The menu's id, and its radios' name: unique to this toggle, as there may be several. */
  readonly menuId = `autoplay-menu-${nextId++}`;

  private readonly settings = computed(() => this.autoplay.chatAutoplay(this.chatId()));
  readonly shown = computed(() => this.player.chatHasVoice(this.chatId()) && !!this.settings());
  readonly on = computed(() => this.autoplay.effective(this.chatId()));
  readonly waiting = computed(() => this.autoplay.tapToListen()?.chatId === this.chatId());
  readonly choice = computed<Choice>(() => {
    const o = this.settings()?.override;
    return o == null ? 'default' : o ? 'on' : 'off';
  });
  readonly choices = computed(() => [
    { value: 'default' as Choice, label: `Personality default (${this.settings()?.personality ? 'on' : 'off'})` },
    { value: 'on' as Choice, label: 'Always, in this chat' },
    { value: 'off' as Choice, label: 'Never, in this chat' },
  ]);

  toggle(): void {
    if (this.open()) {
      this.close(false);
      return;
    }
    this.open.set(true);
    // Into the menu, on the choice that holds now.
    afterNextRender(
      () => {
        const menu = this.host.nativeElement.querySelector(`#${this.menuId}`);
        (menu?.querySelector<HTMLElement>('input:checked') ?? menu?.querySelector<HTMLElement>('input'))?.focus();
      },
      { injector: this.injector },
    );
  }

  /** Closes the menu, giving focus back to its button when it had it. */
  close(refocus: boolean): void {
    this.open.set(false);
    this.error.set('');
    if (refocus) this.host.nativeElement.querySelector<HTMLElement>('[data-testid="autoplay-toggle"]')?.focus();
  }

  onDocumentClick(event: Event): void {
    if (this.open() && !this.host.nativeElement.contains(event.target as Node)) this.close(false);
  }

  onEscape(event: Event): void {
    if (!this.open() || event.defaultPrevented) return;
    event.preventDefault();
    this.close(true);
  }

  async pick(choice: Choice): Promise<void> {
    const chatId = this.chatId();
    if (!chatId) return;
    if (choice === this.choice()) {
      this.close(true);
      return;
    }
    this.error.set('');
    try {
      await this.autoplay.setOverride(chatId, choice === 'default' ? null : choice === 'on');
      this.close(true);
    } catch {
      this.error.set("Couldn't save that. Try again.");
    }
  }

  tap(): void {
    void this.autoplay.tapToPlay();
  }
}
