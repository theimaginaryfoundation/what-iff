import { CommonModule } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, input, output } from '@angular/core';

import { ChatMessage } from '../../../../core/models/message.model';
import { AuthImagePipe } from '../../../../core/pipes/auth-image.pipe';
import { MessageBubbleComponent } from '../message-bubble/message-bubble.component';

interface AssistantVisual {
  avatarUrl: string | null;
  accentColor: string | null;
  accentSurface: string | null;
  expressionsEnabled: boolean;
}

@Component({
  selector: 'app-message-group',
  standalone: true,
  imports: [CommonModule, AuthImagePipe, MessageBubbleComponent],
  template: `
    <section
      class="message-group"
      [class.message-group--user]="origin() === 'User'"
      [class.message-group--assistant]="origin() === 'Assistant'"
      [class.message-group--no-avatar]="origin() === 'Assistant' && !showAssistantAvatar()"
      [style.--assistant-accent]="assistantAccentColor() || 'var(--color-accent)'"
      [style.--assistant-accent-surface]="assistantAccentSurface() || 'color-mix(in oklab, var(--assistant-accent, var(--color-accent)) 14%, transparent)'"
      [attr.aria-label]="origin() + ' message group'"
    >
      @if (origin() === 'Assistant' && showAssistantAvatar()) {
        <div class="message-group__avatar-column">
          @if (expressionKeySnippet(); as exprKey) {
            <div class="message-group__expression-key" [attr.title]="exprKey">{{ exprKey }}</div>
          }
          <div class="message-group__avatar" aria-hidden="true">
            <span class="message-group__avatar-main">
              @if (expressionImageUrl(); as exprUrl) {
                @if (exprUrl | authImage | async; as resolvedExpr) {
                  <img [src]="resolvedExpr" alt="" />
                } @else {
                  {{ assistantLabel().charAt(0) }}
                }
              } @else if (assistantThumbnail(); as thumbnail) {
                <img [src]="'data:image/jpeg;base64,' + thumbnail" alt="" />
              } @else if (assistantAvatarUrl(); as avatarUrl) {
                @if (avatarUrl | authImage | async; as resolvedAvatarUrl) {
                  <img [src]="resolvedAvatarUrl" alt="" />
                } @else {
                  {{ assistantLabel().charAt(0) }}
                }
              } @else {
                {{ assistantLabel().charAt(0) }}
              }
            </span>
            <span class="message-group__avatar-popout">
              @if (expressionImageUrl(); as exprUrl) {
                @if (exprUrl | authImage | async; as resolvedExpr) {
                  <img [src]="resolvedExpr" alt="" />
                  <span class="message-group__avatar-popout-label">{{ assistantLabel() }}</span>
                } @else {
                  <span class="message-group__avatar-popout-fallback">
                    <span class="message-group__avatar-popout-initials">{{ assistantInitials() }}</span>
                    <span class="message-group__avatar-popout-fallback-name">{{ assistantLabel() }}</span>
                  </span>
                }
              } @else if (assistantThumbnail(); as thumbnail) {
                <img [src]="'data:image/jpeg;base64,' + thumbnail" alt="" />
                <span class="message-group__avatar-popout-label">{{ assistantLabel() }}</span>
              } @else if (assistantAvatarUrl(); as avatarUrl) {
                @if (avatarUrl | authImage | async; as resolvedAvatarUrl) {
                  <img [src]="resolvedAvatarUrl" alt="" />
                  <span class="message-group__avatar-popout-label">{{ assistantLabel() }}</span>
                } @else {
                  <span class="message-group__avatar-popout-fallback">
                    <span class="message-group__avatar-popout-initials">{{ assistantInitials() }}</span>
                    <span class="message-group__avatar-popout-fallback-name">{{ assistantLabel() }}</span>
                  </span>
                }
              } @else {
                <span class="message-group__avatar-popout-fallback">
                  <span class="message-group__avatar-popout-initials">{{ assistantInitials() }}</span>
                  <span class="message-group__avatar-popout-fallback-name">{{ assistantLabel() }}</span>
                </span>
              }
            </span>
          </div>
        </div>
      }
      <div class="message-group__items">
        @for (message of messages(); track message.id) {
          <app-message-bubble
            [attr.data-message-id]="message.id"
            [message]="message"
            [displayContent]="displayMessage(message)"
            (copy)="copy.emit($event)"
            (showContext)="showContext.emit($event)"
            (toggleBookmark)="toggleBookmark.emit($event)"
          />
        }
        @if (userGenerationError(); as errInfo) {
          <div class="message-group__gen-error message-group__gen-error--user" role="alert">
            <span class="message-group__gen-error-text">{{ errInfo.text }}</span>
            <button
              type="button"
              class="message-group__gen-error-retry"
              (click)="retryUserMessage.emit(errInfo.message)"
            >
              Retry
            </button>
          </div>
        }
        @if (assistantCheckpointLine(); as ck) {
          <div class="message-group__checkpoint" role="status" [attr.aria-label]="ck">
            <span class="message-group__checkpoint-line" aria-hidden="true"></span>
            <span class="message-group__checkpoint-pill">{{ ck }}</span>
            <span class="message-group__checkpoint-line" aria-hidden="true"></span>
          </div>
        }
      </div>
    </section>
  `,
  styles: [`
    .message-group {
      align-items: flex-end;
      display: flex;
      gap: 0.75rem;
      max-width: 100%;
      min-width: 0;
      padding: 0.125rem 0;
    }

    .message-group--user {
      justify-content: flex-end;
    }

    .message-group--assistant.message-group--no-avatar .message-group__items {
      max-width: min(96%, min(72rem, 100%));
    }

    .message-group--assistant:not(.message-group--no-avatar) .message-group__items {
      flex: 1 1 0;
      max-width: none;
      min-width: 0;
    }

    .message-group__header {
      align-items: center;
      color: var(--color-text-muted);
      display: flex;
      font-size: 0.625rem;
      font-weight: 600;
      gap: 0.3125rem;
      letter-spacing: 0.04em;
      margin-bottom: 0.125rem;
      text-transform: uppercase;
    }

    .message-group__avatar-column {
      align-items: center;
      display: flex;
      flex: 0 0 6.875rem;
      flex-direction: column;
      gap: 0.25rem;
      min-width: 0;
    }

    .message-group__expression-key {
      color: var(--color-text-muted);
      font-family: ui-monospace, monospace;
      font-size: 0.5625rem;
      font-weight: 600;
      line-height: 1.2;
      max-width: 6.875rem;
      overflow: hidden;
      text-align: center;
      text-overflow: ellipsis;
      white-space: nowrap;
      width: 100%;
    }

    .message-group__avatar {
      display: inline-flex;
      flex: 0 0 6.875rem;
      position: relative;
    }

    .message-group__avatar-main,
    .message-group__avatar-popout {
      align-items: center;
      background: var(--assistant-accent-surface);
      border-radius: 0.75rem;
      color: var(--assistant-accent, var(--color-accent));
      display: inline-flex;
      font-size: 1.875rem;
      font-weight: 800;
      height: 9.125rem;
      justify-content: center;
      overflow: hidden;
      width: 6.875rem;
    }

    .message-group__avatar-main img,
    .message-group__avatar-popout img {
      height: 100%;
      object-fit: cover;
      object-position: center top;
      width: 100%;
    }

    .message-group__avatar-popout {
      border: 2px solid var(--assistant-accent, var(--color-accent));
      border-radius: 0.625rem;
      box-shadow: 0 12px 40px rgb(0 0 0 / 0.55);
      height: 12.5rem;
      left: calc(100% + 0.5rem);
      opacity: 0;
      pointer-events: none;
      position: absolute;
      top: 50%;
      transform: translateY(-50%) scale(0.96);
      transform-origin: left center;
      transition: opacity 140ms ease, transform 140ms ease;
      visibility: hidden;
      width: 9.375rem;
      z-index: 8;
    }

    .message-group__avatar:hover .message-group__avatar-popout,
    .message-group__avatar:focus-within .message-group__avatar-popout {
      opacity: 1;
      transform: translateY(-50%) scale(1);
      visibility: visible;
    }

    .message-group__avatar-popout-label {
      align-items: end;
      background: linear-gradient(to top, rgb(0 0 0 / 0.75) 0%, rgb(0 0 0 / 0) 100%);
      color: #fff;
      display: flex;
      font-size: 0.75rem;
      font-weight: 700;
      inset: auto 0 0 0;
      justify-content: center;
      min-height: 3.125rem;
      padding: 0.75rem 0.625rem 0.5rem;
      position: absolute;
      text-align: center;
    }

    .message-group__avatar-popout-fallback {
      align-items: center;
      background: color-mix(in oklab, var(--assistant-accent, var(--color-accent)) 14%, var(--color-surface-elevated));
      color: var(--assistant-accent, var(--color-accent));
      display: flex;
      flex-direction: column;
      gap: 0.5rem;
      height: 100%;
      justify-content: center;
      width: 100%;
    }

    .message-group__avatar-popout-initials {
      font-size: 3.25rem;
      font-weight: 800;
      letter-spacing: -0.05em;
      line-height: 1;
    }

    .message-group__avatar-popout-fallback-name {
      color: color-mix(in srgb, var(--assistant-accent, var(--color-accent)) 72%, var(--color-text-secondary));
      font-size: 0.75rem;
      font-weight: 600;
      text-align: center;
    }

    .message-group__items {
      display: grid;
      gap: 0.75rem;
      max-width: min(92%, min(72rem, 100%));
      min-width: 0;
    }

    .message-group--user .message-group__items {
      justify-items: end;
    }

    .message-group__gen-error {
      align-items: center;
      border: 1px solid var(--color-border-base);
      border-radius: 0.5rem;
      color: var(--color-text-secondary);
      display: flex;
      flex-wrap: wrap;
      font-size: 0.6875rem;
      gap: 0.5rem;
      justify-content: space-between;
      line-height: 1.35;
      max-width: min(92%, min(72rem, 100%));
      padding: 0.4375rem 0.625rem;
      width: 100%;
    }

    .message-group__gen-error--user {
      background: color-mix(in srgb, var(--color-danger) 10%, var(--color-surface-muted));
      border-color: color-mix(in srgb, var(--color-danger) 35%, var(--color-border-base));
      color: color-mix(in srgb, var(--color-danger) 55%, var(--color-text-primary));
    }

    .message-group__gen-error-text {
      flex: 1;
      min-width: 8rem;
      white-space: pre-wrap;
      word-break: break-word;
    }

    .message-group__gen-error-retry {
      border-radius: 999px;
      border: 1px solid var(--color-border-base);
      color: var(--color-text-primary);
      flex-shrink: 0;
      font-size: 0.6875rem;
      font-weight: 600;
      padding: 0.25rem 0.625rem;
    }

    .message-group__gen-error-retry:hover,
    .message-group__gen-error-retry:focus-visible {
      border-color: var(--color-accent);
      color: var(--color-accent);
    }

    .message-group__checkpoint {
      align-items: center;
      display: flex;
      gap: 0.5rem;
      justify-self: center;
      margin-top: 0.25rem;
      width: 100%;
    }

    .message-group__checkpoint-line {
      background: var(--color-border-base);
      flex: 1;
      height: 1px;
      min-width: 0.75rem;
    }

    .message-group__checkpoint-pill {
      background: var(--color-surface-muted);
      border: 1px solid var(--color-border-base);
      border-radius: 999px;
      color: var(--color-text-muted);
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
      font-size: 0.6875rem;
      padding: 0.25rem 0.625rem;
      text-align: center;
      white-space: nowrap;
    }

    @media (max-width: 640px) {
      .message-group {
        gap: 0.5rem;
      }

      .message-group__avatar-column {
        flex: 0 0 clamp(2.5rem, 30vw, 4.25rem);
      }

      .message-group__avatar {
        flex: 0 0 clamp(2.5rem, 30vw, 4.25rem);
      }

      .message-group__avatar-main {
        border-radius: 0.5rem;
        font-size: clamp(0.875rem, 3.5vw, 1.25rem);
        height: clamp(3.25rem, 38vw, 5.75rem);
        width: clamp(2.5rem, 30vw, 4.25rem);
      }

      .message-group__expression-key {
        font-size: 0.5rem;
        max-width: clamp(2.5rem, 30vw, 4.25rem);
      }

      .message-group__items {
        max-width: 100%;
      }

      .message-group--user .message-group__items {
        max-width: min(94%, 100%);
      }
    }

    @media (max-width: 420px) {
      .message-group__avatar-column {
        flex: 0 0 clamp(2.125rem, 24vw, 3.25rem);
      }

      .message-group__avatar {
        flex: 0 0 clamp(2.125rem, 24vw, 3.25rem);
      }

      .message-group__avatar-main {
        font-size: 0.75rem;
        height: clamp(2.75rem, 32vw, 4.25rem);
        width: clamp(2.125rem, 24vw, 3.25rem);
      }

      .message-group__expression-key {
        max-width: clamp(2.125rem, 24vw, 3.25rem);
      }
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MessageGroupComponent {
  readonly origin = input.required<ChatMessage['origin']>();
  readonly messages = input.required<readonly ChatMessage[]>();
  readonly assistantVisual = input<AssistantVisual | null>(null);
  readonly displayResolver = input<(message: ChatMessage) => string>((message) => message.message);
  readonly copy = output<ChatMessage>();
  readonly showContext = output<ChatMessage>();
  readonly toggleBookmark = output<ChatMessage>();
  readonly retryUserMessage = output<ChatMessage>();

  /** When false, hide the entire assistant image column (expression, portrait, thinking). */
  readonly showAssistantAvatar = computed(() => this.assistantVisual()?.expressionsEnabled !== false);

  readonly userGenerationError = computed((): { text: string; message: ChatMessage } | null => {
    if (this.origin() !== 'User') {
      return null;
    }
    const target = this.messages().find(m => m.last_error_message?.trim());
    if (!target?.last_error_message?.trim()) {
      return null;
    }
    return { text: target.last_error_message.trim(), message: target };
  });

  /** Shown after assistant bubbles when a post-turn checkpoint (scratchpad + summary) completed. */
  readonly assistantCheckpointLine = computed((): string | null => {
    if (this.origin() !== 'Assistant') return null;
    const msgs = this.messages();
    if (!msgs.length) return null;
    const last = msgs[msgs.length - 1];
    if (!last.checkpoint_completed_at?.trim()) return null;
    return 'Scratchpad & summary updated';
  });

  displayMessage(message: ChatMessage): string {
    return this.displayResolver()(message);
  }

  assistantLabel(): string {
    return this.messages().find(message => message.generation_personality)?.generation_personality ?? 'Assistant';
  }

  assistantInitials(): string {
    return this.assistantLabel()
      .split(/\s+/)
      .map(part => part.charAt(0))
      .join('')
      .toUpperCase()
      .slice(0, 2);
  }

  assistantThumbnail(): string | null {
    return this.messages().find(message => message.generation_mood_thumbnail)?.generation_mood_thumbnail ?? null;
  }

  expressionImageUrl(): string | null {
    if (!this.showAssistantAvatar()) return null;
    return (
      this.messages().find(message => message.generation_expression_image_url)?.generation_expression_image_url ??
      null
    );
  }

  expressionKeySnippet(): string | null {
    if (!this.showAssistantAvatar()) return null;
    const key = this.messages().find(m => m.generation_expression_key?.trim())?.generation_expression_key?.trim();
    return key || null;
  }

  assistantAvatarUrl(): string | null {
    return this.assistantVisual()?.avatarUrl ?? null;
  }

  assistantAccentColor(): string | null {
    return this.assistantVisual()?.accentColor ?? null;
  }

  assistantAccentSurface(): string | null {
    return this.assistantVisual()?.accentSurface ?? null;
  }
}
