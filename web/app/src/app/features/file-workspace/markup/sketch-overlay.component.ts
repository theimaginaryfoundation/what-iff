import { ChangeDetectionStrategy, Component, computed, inject, input, output, signal } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';

import { imageEditingUnavailableReason } from '../../../core/models/user.model';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { UserPreferencesService } from '../../../core/services/user-preferences.service';
import { OverlayComponent } from '../../../shared/ui/overlay/overlay.component';
import { XIconComponent } from '../../../shared/ui/icons/icons';
import { ImageMarkupComponent, MarkupResult } from './image-markup.component';
import { injectMarkupSender } from './markup-upload';
import { LoggerService } from '../../../core/services/logger.service';

/**
 * A blank page to sketch on, from the composer's + menu (ADR 0x047 §7). The sketch becomes a new
 * file in the thread, attached to the message being written; the person says what to make of it.
 */
@Component({
  selector: 'app-sketch-overlay',
  standalone: true,
  imports: [OverlayComponent, ImageMarkupComponent, XIconComponent],
  template: `
    <ui-overlay [open]="open()" labelledBy="sketch-overlay-title" variant="inset" (dismiss)="requestClose()">
      <header class="sketch__bar">
        <h2 id="sketch-overlay-title" class="sketch__title">New canvas</h2>
        <button type="button" class="sketch__close" aria-label="Close canvas" (click)="requestClose()">
          <ui-x-icon [size]="14" />
        </button>
      </header>
      @if (open()) {
        <app-image-markup
          name="Sketch"
          [notice]="notice()"
          [busy]="sending()"
          (dirtyChange)="dirty.set($event)"
          (submitted)="onSubmitted($event)"
          (cancel)="requestClose()"
        />
      }
      @if (error(); as message) {
        <p class="sketch__error" role="alert">{{ message }}</p>
      }
    </ui-overlay>
  `,
  styles: [
    `
      .sketch__bar {
        align-items: center;
        border-bottom: 1px solid var(--border);
        display: flex;
        justify-content: space-between;
        padding: 0.6rem 1rem;
      }
      .sketch__title {
        font-size: 0.95rem;
        font-weight: 600;
        margin: 0;
      }
      .sketch__close {
        border-radius: 0.5rem;
        color: var(--text-secondary);
        padding: 0.35rem;
      }
      .sketch__close:hover {
        background: var(--accent-bg);
      }
      .sketch__error {
        color: var(--danger);
        margin: 0;
        padding: 0 1rem 0.75rem;
      }
      app-image-markup {
        flex: 1 1 auto;
        min-height: 0;
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SketchOverlayComponent {
  private readonly logger = inject(LoggerService);
  private readonly sendMarkup = injectMarkupSender();
  private readonly confirmationService = inject(ConfirmationService);
  private readonly preferences = toSignal(inject(UserPreferencesService).preferences$, { initialValue: null });

  readonly open = input(false);
  /** The thread the sketch is added to. */
  readonly chatId = input<string | null>(null);
  readonly closed = output<void>();

  readonly notice = computed(() => imageEditingUnavailableReason(this.preferences()));
  readonly dirty = signal(false);
  readonly sending = signal(false);
  readonly error = signal<string | null>(null);

  async requestClose(): Promise<void> {
    if (this.sending()) return;
    if (this.dirty() && !(await this.confirmationService.confirmDiscardChanges())) return;
    this.finish();
  }

  async onSubmitted(result: MarkupResult): Promise<void> {
    if (this.sending()) return;
    const chatId = this.chatId();
    if (!chatId) {
      // The entry points need a thread; say so rather than ignore the press if one slips through.
      this.error.set('Start the chat with a first message, then add your sketch.');
      return;
    }
    this.sending.set(true);
    this.error.set(null);
    try {
      await this.sendMarkup(chatId, null, result);
      this.finish();
    } catch (error) {
      this.logger.error('Could not add the sketch to the message', error);
      this.error.set('Could not add the sketch to your message. Please try again.');
    } finally {
      this.sending.set(false);
    }
  }

  private finish(): void {
    this.dirty.set(false);
    this.error.set(null);
    this.closed.emit();
  }
}
