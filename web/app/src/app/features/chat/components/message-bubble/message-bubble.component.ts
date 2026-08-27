
import { ChangeDetectionStrategy, Component, computed, input, output } from '@angular/core';

import { ChatMessage } from '../../../../core/models/message.model';
import { CHAT_PENDING_ASSISTANT_MESSAGE_ID } from '../../chat.constants';
import { TooltipDirective } from '../../../../shared/ui/tooltip/tooltip.directive';
import { StarIconComponent } from '../../../../shared/ui/icons/icons';
import { MessageContentComponent } from '../message-content/message-content.component';

@Component({
  selector: 'app-message-bubble',
  standalone: true,
  imports: [MessageContentComponent, TooltipDirective, StarIconComponent],
  template: `
    <article
      class="bubble"
      [class.bubble--user]="message().origin === 'User'"
      [class.bubble--pending]="showPendingDots()"
      [class.bubble--bookmarked]="message().bookmarked && !isPendingPlaceholder()"
      [attr.aria-label]="ariaLabel()"
    >
      <div class="bubble__body">
        @if (showPendingDots()) {
          <span class="bubble__pending-dots" aria-hidden="true">
            <span></span><span></span><span></span>
          </span>
        } @else {
          <app-message-content
            [content]="displayContent()"
            [attachments]="message().attachments ?? []"
            [preserveLineBreaks]="message().origin === 'User'"
          />
        }
      </div>
      @if (userSkillNames().length) {
        <div class="bubble__skills" aria-label="Skills sent with this message">
          @for (name of userSkillNames(); track name) {
            <span class="bubble__skill-pill">{{ name }}</span>
          }
        </div>
      }
      @if (!isPendingPlaceholder()) {
        <div class="bubble__meta" [class.bubble__meta--assistant]="message().origin === 'Assistant'">
          <div class="bubble__meta-start">
            <time class="bubble__timestamp" [attr.datetime]="message().sent_at" [uiTooltip]="formatTimestamp(message().sent_at)">
              <span>{{ shortDate(message().sent_at) }}</span>
              <span>{{ shortTime(message().sent_at) }}</span>
            </time>
            <button type="button" class="bubble__copy" (click)="copy.emit(message())">Copy</button>
            <button
              type="button"
              class="bubble__bookmark"
              [class.bubble__bookmark--active]="message().bookmarked"
              [attr.aria-pressed]="!!message().bookmarked"
              (click)="toggleBookmark.emit(message())"
              [title]="message().bookmarked ? 'Remove bookmark' : 'Bookmark this message'"
            >
              <ui-star-icon [size]="12" [filled]="!!message().bookmarked" />
              <span>{{ message().bookmarked ? 'Saved' : 'Save' }}</span>
            </button>
          </div>
          <div class="bubble__meta-end">
            @if (hasContextBreakdown()) {
              <button
                type="button"
                class="bubble__context"
                (click)="showContext.emit(message())"
                title="Show what filled the model's context window for this reply"
              >Context</button>
            }
            @if (modelMoodHint(); as hint) {
              <span class="bubble__hint" [attr.title]="hint">{{ hint }}</span>
            }
          </div>
        </div>
      }
    </article>
  `,
  styles: [`
    :host {
      display: block;
      max-width: 100%;
      min-width: 0;
      /* Bookmark accent; a warm gold that reads on both light and dark surfaces. */
      --bookmark-gold: #e0a53a;
    }

    .bubble {
      max-width: 100%;
      min-width: 0;
      position: relative;
    }

    .bubble--user {
      margin-left: auto;
    }

    .bubble--pending {
      display: inline-block;
      width: fit-content;
      max-width: fit-content;
    }

    .bubble__body {
      background: var(--ai-bubble, var(--color-surface-muted));
      border-radius: 0.625rem 0.625rem 0.625rem 0.25rem;
      color: var(--ai-text, var(--color-text-primary));
      font-size: 0.8125rem;
      line-height: 1.6;
      max-width: 100%;
      min-width: 0;
      overflow: hidden;
      padding: 0.625rem 0.875rem;
    }

    .bubble--pending .bubble__body {
      padding: 0.625rem 0.75rem;
    }

    .bubble--user .bubble__body {
      background: var(--user-bubble, color-mix(in srgb, var(--color-accent) 58%, var(--color-surface-base)));
      border-radius: 0.625rem 0.625rem 0.25rem;
      color: var(--user-text, var(--color-text-primary));
    }

    .bubble--user .bubble__body :where(p, li, blockquote, pre, code, strong, em, a, h1, h2, h3, h4, h5, h6, span) {
      color: inherit;
    }

    .bubble--user .bubble__body :where(a) {
      text-decoration-color: color-mix(in srgb, var(--user-text, #fff) 72%, transparent);
    }

    .bubble--user .bubble__body :where(code, pre) {
      background: color-mix(in srgb, currentColor 14%, transparent);
    }

    .bubble__skills {
      display: flex;
      flex-wrap: wrap;
      gap: 0.25rem;
      justify-content: flex-end;
      margin-top: 0.25rem;
      max-width: 100%;
    }

    .bubble__skill-pill {
      background: color-mix(in srgb, var(--color-accent) 18%, transparent);
      border: 1px solid color-mix(in srgb, var(--color-accent) 40%, var(--color-border-base));
      border-radius: 999px;
      color: var(--color-text-secondary);
      font-size: 0.5625rem;
      font-weight: 700;
      letter-spacing: 0.03em;
      max-width: 12rem;
      overflow: hidden;
      padding: 0.0625rem 0.4rem;
      text-overflow: ellipsis;
      text-transform: uppercase;
      white-space: nowrap;
    }

    .bubble__meta {
      align-items: center;
      color: var(--color-text-muted);
      display: flex;
      font-family: 'Outfit', sans-serif;
      font-size: 0.5625rem;
      gap: 0.5rem;
      justify-content: flex-start;
      margin-top: 0.25rem;
      opacity: 0;
      transition: opacity 150ms ease;
      width: 100%;
    }

    .bubble__meta--assistant {
      justify-content: space-between;
      gap: 0.375rem;
    }

    .bubble__meta-start {
      align-items: center;
      display: inline-flex;
      flex-shrink: 0;
      gap: 0.5rem;
    }

    .bubble__meta-end {
      align-items: center;
      display: inline-flex;
      gap: 0.5rem;
      margin-left: auto;
      min-width: 0;
    }

    .bubble__hint {
      color: var(--color-text-muted);
      flex: 1 1 auto;
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
      font-size: 0.5rem;
      letter-spacing: 0.02em;
      line-height: 1.2;
      max-width: none;
      min-width: 0;
      opacity: 0.92;
      overflow-wrap: anywhere;
    }

    .bubble__timestamp {
      align-items: baseline;
      display: inline-flex;
      gap: 0.375rem;
    }

    .bubble__copy,
    .bubble__context {
      border: 1px solid color-mix(in srgb, var(--color-accent) 35%, var(--color-border-base));
      border-radius: 999px;
      color: var(--color-accent);
      flex-shrink: 0;
      opacity: 0.8;
      padding: 0.125rem 0.375rem;
      transition: background 150ms ease, opacity 150ms ease;
    }

    .bubble__copy:hover,
    .bubble__copy:focus-visible,
    .bubble__context:hover,
    .bubble__context:focus-visible {
      background: color-mix(in srgb, var(--color-accent) 14%, transparent);
      opacity: 1;
    }

    .bubble__bookmark {
      align-items: center;
      border: 1px solid var(--color-border-base);
      border-radius: 999px;
      color: var(--color-text-muted);
      display: inline-flex;
      flex-shrink: 0;
      gap: 0.2rem;
      opacity: 0.8;
      padding: 0.125rem 0.375rem;
      transition: background 150ms ease, color 150ms ease, opacity 150ms ease;
    }

    .bubble__bookmark:hover,
    .bubble__bookmark:focus-visible { opacity: 1; }

    .bubble__bookmark--active {
      border-color: color-mix(in srgb, var(--bookmark-gold) 50%, var(--color-border-base));
      color: var(--bookmark-gold);
      opacity: 1;
    }

    /* Always-visible gold bar so bookmarks are spottable while scrolling (layout-safe inset). */
    .bubble--bookmarked .bubble__body {
      box-shadow: inset 3px 0 0 0 var(--bookmark-gold);
    }
    .bubble--bookmarked.bubble--user .bubble__body {
      box-shadow: inset -3px 0 0 0 var(--bookmark-gold);
    }

    .bubble--user .bubble__meta {
      justify-content: flex-end;
    }

    .bubble:hover .bubble__meta,
    .bubble:focus-within .bubble__meta {
      opacity: 1;
    }

    .bubble__pending-dots {
      display: inline-flex;
      gap: 0.25rem;
      min-height: 1em;
      align-items: center;
    }

    .bubble__pending-dots span {
      animation: bubble-pending-dot 1.2s ease-in-out infinite;
      background: var(--color-text-muted);
      border-radius: 999px;
      height: 0.375rem;
      width: 0.375rem;
    }

    .bubble__pending-dots span:nth-child(2) {
      animation-delay: 0.15s;
    }

    .bubble__pending-dots span:nth-child(3) {
      animation-delay: 0.3s;
    }

    @media (prefers-reduced-motion: reduce) {
      .bubble__pending-dots span {
        animation: none;
      }
    }

    @keyframes bubble-pending-dot {
      0%, 60%, 100% { opacity: 0.35; transform: translateY(0); }
      30% { opacity: 1; transform: translateY(-0.125rem); }
    }

    /* Applied by message-list.scrollToMessage when jumping to a message (e.g. a bookmark). */
    :host(.message-flash) .bubble__body {
      animation: bubble-flash 1.8s ease;
    }

    @keyframes bubble-flash {
      0%, 100% { box-shadow: 0 0 0 0 transparent; }
      15% { box-shadow: 0 0 0 3px color-mix(in srgb, var(--bookmark-gold) 60%, transparent); }
    }

    @media (prefers-reduced-motion: reduce) {
      :host(.message-flash) .bubble__body { animation: none; }
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MessageBubbleComponent {
  readonly message = input.required<ChatMessage>();
  readonly displayContent = input('');
  readonly copy = output<ChatMessage>();
  readonly toggleBookmark = output<ChatMessage>();
  readonly showContext = output<ChatMessage>();

  /** True for assistant replies that captured a Context X-ray snapshot. */
  readonly hasContextBreakdown = computed(() => {
    const m = this.message();
    return m.origin === 'Assistant' && (m.context_breakdown?.segments?.length ?? 0) > 0;
  });

  readonly isPendingPlaceholder = computed(() => this.message().id === CHAT_PENDING_ASSISTANT_MESSAGE_ID);
  readonly showPendingDots = computed(
    () => this.isPendingPlaceholder() && !this.displayContent().trim(),
  );
  readonly modelMoodHint = computed((): string | null => {
    const m = this.message();
    if (m.origin !== 'Assistant') {
      return null;
    }
    const model = m.generation_model?.trim();
    const mood = m.generation_mood_name?.trim();
    if (!model && !mood) {
      return null;
    }
    return `[${model || '—'}:${mood || '—'}]`;
  });

  readonly userSkillNames = computed((): string[] => {
    if (this.message().origin !== 'User') {
      return [];
    }
    const names = (this.message().rituals ?? [])
      .map(r => r.name?.trim())
      .filter((n): n is string => !!n);
    return [...new Set(names)];
  });

  readonly ariaLabel = computed(() =>
    this.showPendingDots() ? 'Assistant is composing a reply' : `${this.message().origin} message`,
  );

  shortTime(value: string): string {
    return new Date(value).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  }

  shortDate(value: string): string {
    return new Date(value).toLocaleDateString([], { month: 'short', day: 'numeric' }).toUpperCase();
  }

  formatTimestamp(value: string): string {
    return new Date(value).toLocaleString();
  }
}
