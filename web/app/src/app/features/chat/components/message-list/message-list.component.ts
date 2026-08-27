
import { ChangeDetectionStrategy, Component, ElementRef, computed, effect, input, output, signal, viewChild } from '@angular/core';

import { ChatMessage } from '../../../../core/models/message.model';
import { ToolCall } from '../../../../core/models/toolcall.model';
import { GroupedItem } from '../../helpers/message-grouping.helpers';
import { MessageGroupComponent } from '../message-group/message-group.component';
import { ToolCallGroupComponent } from '../tool-call-group/tool-call-group.component';

const AUTO_SCROLL_BOTTOM_EPSILON_PX = 8;
const STREAM_FOLLOW_SCROLL_MIN_INTERVAL_MS = 220;
// Start loading the next older page this far from the top so it lands before the user hits it.
const OLDER_LOAD_THRESHOLD_PX = 600;

interface AssistantVisual {
  avatarUrl: string | null;
  accentColor: string | null;
  accentSurface: string | null;
  expressionsEnabled: boolean;
}

@Component({
  selector: 'app-message-list',
  standalone: true,
  imports: [MessageGroupComponent, ToolCallGroupComponent],
  template: `
    <section
      #scrollContainer
      class="message-list"
      role="log"
      aria-live="polite"
      aria-label="Conversation messages"
      (scroll)="onScroll($event)"
    >
      <div class="message-list__lane">
        @if (hasMoreOlder()) {
          <button
            type="button"
            class="message-list__history"
            [disabled]="loadingOlder()"
            (click)="requestLoadOlder()"
          >
            {{ loadingOlder() ? 'Loading…' : 'Load more history' }}
          </button>
        }
        @for (item of groups(); track trackGroup(item, $index)) {
          @switch (item.kind) {
            @case ('message-group') {
              <app-message-group
                [origin]="item.origin"
                [messages]="item.messages"
                [assistantVisual]="assistantVisualFor(item)"
                [displayResolver]="displayResolver()"
                (copy)="copy.emit($event)"
                (showContext)="showContext.emit($event)"
                (toggleBookmark)="toggleBookmark.emit($event)"
                (retryUserMessage)="retryUserMessage.emit($event)"
              />
            }
            @case ('tool-call-group') {
              <app-tool-call-group [toolCalls]="item.toolCalls" (openDetail)="openToolCallDetail.emit($event)" />
            }
            @case ('model-change-divider') {
              <div class="message-list__divider" role="separator" aria-label="Model changed">
                <span class="message-list__divider-line" aria-hidden="true"></span>
                <span class="message-list__divider-pill">
                  {{ item.previousModel ? 'Model changed from ' + item.previousModel + ' to ' + item.model : 'Model changed to ' + item.model }}
                </span>
                <span class="message-list__divider-line" aria-hidden="true"></span>
              </div>
            }
            @case ('system-message') {
              <div class="message-list__system">{{ item.message.message }}</div>
            }
          }
        }
      </div>
    </section>
    @if (!isNearBottom()) {
      <button type="button" class="message-list__scroll" (click)="scrollToBottom()">Jump to latest</button>
    }
  `,
  styles: [`
    :host {
      display: grid;
      min-height: 0;
      position: relative;
    }

    .message-list {
      background: var(--color-surface-base);
      overflow-x: hidden;
      overflow-y: auto;
      padding: 1rem 2.75rem 1rem 1rem;
      scroll-behavior: smooth;
    }

    @media (max-width: 640px) {
      .message-list {
        padding: 0.75rem 0.75rem 0.75rem 0.625rem;
      }
    }

    .message-list__lane {
      display: grid;
      gap: 1rem;
      margin-inline: auto;
      max-width: min(100%, 120rem);
      min-width: 0;
      width: 100%;
    }

    .message-list__system {
      color: var(--color-text-muted);
      font-size: 0.75rem;
      justify-self: center;
    }

    .message-list__system {
      border-radius: 999px;
      padding: 0.25rem 0.75rem;
      background: var(--color-surface-muted);
    }

    .message-list__divider {
      align-items: center;
      display: flex;
      gap: 0.5rem;
      width: min(100%, 76rem);
      justify-self: center;
    }

    .message-list__divider-line {
      background: var(--color-border-base);
      flex: 1;
      height: 1px;
      min-width: 1rem;
    }

    .message-list__divider-pill {
      background: var(--color-surface-muted);
      border: 1px solid var(--color-border-base);
      border-radius: 999px;
      color: var(--color-text-muted);
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
      font-size: 0.6875rem;
      padding: 0.25rem 0.625rem;
      white-space: nowrap;
    }

    .message-list__scroll,
    .message-list__history {
      border-radius: 999px;
      background: var(--color-surface-base);
      border: 1px solid var(--color-border-base);
      color: var(--color-text-primary);
      padding: 0.5rem 0.875rem;
      box-shadow: 0 8px 24px rgb(0 0 0 / 0.12);
    }

    .message-list__history {
      justify-self: center;
      margin-bottom: 0.25rem;
    }

    .message-list__history:disabled {
      cursor: wait;
      opacity: 0.7;
    }

    .message-list__scroll {
      bottom: 1rem;
      justify-self: center;
      position: absolute;
    }

    @media (max-width: 1023px) {
      .message-list {
        padding-right: 1rem;
      }

      .message-list__divider-pill {
        white-space: normal;
      }
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MessageListComponent {
  readonly conversationId = input<string | null>(null);
  readonly groups = input.required<readonly GroupedItem[]>();
  readonly assistantAvatarUrl = input<string | null>(null);
  readonly assistantAccentColor = input<string | null>(null);
  readonly assistantAccentSurface = input<string | null>(null);
  readonly assistantVisualResolver = input<(message: ChatMessage) => AssistantVisual | null>(() => null);
  /** Thread-level setting: when false, hide assistant image frames for all messages. */
  readonly expressionsEnabled = input(true);
  readonly displayResolver = input<(message: ChatMessage) => string>((message) => message.message);
  readonly displayRevision = input(0);
  readonly hasMoreOlder = input(false);
  readonly loadingOlder = input(false);
  readonly checkpointMessageId = input<string | null>(null);
  readonly loadOlder = output<void>();
  readonly copy = output<ChatMessage>();
  readonly showContext = output<ChatMessage>();
  readonly toggleBookmark = output<ChatMessage>();
  readonly openToolCallDetail = output<ToolCall>();
  readonly retryUserMessage = output<ChatMessage>();
  readonly scrollToBottomRequested = output<void>();
  readonly scrollContainer = viewChild<ElementRef<HTMLElement>>('scrollContainer');

  readonly isNearBottom = signal(true);
  /** When true, new tail content (send, reply, stream) triggers auto-scroll. */
  private readonly stickyToBottom = signal(true);
  private readonly tailScrollDigest = computed(() => {
    const revision = this.displayRevision();
    return digestForTailScroll(this.groups(), this.displayResolver(), revision);
  });

  private lastConversationId: string | null = null;
  private lastAppliedScrollDigest = '';
  private lastAutoScrollTailId: string | null = null;
  private lastAutoScrollAtMs = 0;
  // Anchor for restoring scroll after an older page prepends (element id + its viewport top).
  private pendingPrependAnchor: { id: string; top: number } | null = null;
  // Guards against firing multiple older-page loads for one scroll gesture.
  private prependInFlight = false;
  // Last observed scrollTop, to detect scroll direction (only auto-load older when going up).
  private lastScrollTop = 0;
  private lastCheckpointMessageId: string | null = null;
  private readonly groupsLengthForScrollRestore = computed(() => this.groups().length);

  constructor() {
    effect(() => {
      const conversationId = this.conversationId();
      if (conversationId !== this.lastConversationId) {
        this.lastConversationId = conversationId;
        this.lastAppliedScrollDigest = '';
        this.lastAutoScrollTailId = null;
        this.lastAutoScrollAtMs = 0;
        this.stickyToBottom.set(true);
      }

      const digest = this.tailScrollDigest();
      if (!digest) return;
      if (!this.stickyToBottom()) return;
      if (digest === this.lastAppliedScrollDigest) return;
      const tail = lastMessageInGroups(this.groups());
      const tailId = tail?.id ?? null;
      const now = Date.now();
      const isStreamingTailUpdate = tailId !== null && tailId === this.lastAutoScrollTailId;
      if (isStreamingTailUpdate && now-this.lastAutoScrollAtMs < STREAM_FOLLOW_SCROLL_MIN_INTERVAL_MS) {
        return;
      }

      this.lastAppliedScrollDigest = digest;
      this.lastAutoScrollTailId = tailId;
      this.lastAutoScrollAtMs = now;
      this.scheduleScrollToBottom();
    });

    // Preserve the reading position when older messages prepend. Anchoring to a specific
    // element (not a raw scrollHeight delta) survives asynchronously-sized content above the
    // fold (avatars, expression portraits) that would otherwise shove the view as it loads.
    effect(() => {
      const length = this.groupsLengthForScrollRestore();
      void length;
      const anchor = this.pendingPrependAnchor;
      if (!anchor) return;
      this.pendingPrependAnchor = null;
      this.restoreToAnchor(anchor);
    });

    // Release the in-flight guard once the parent finishes loading an older page.
    effect(() => {
      if (!this.loadingOlder()) {
        this.prependInFlight = false;
      }
    });

    effect(() => {
      const checkpointMessageId = this.checkpointMessageId();
      const groups = this.groups();
      if (!checkpointMessageId) {
        this.lastCheckpointMessageId = null;
        return;
      }
      // Only mark success after the target is in the DOM. If the checkpoint is on an older
      // page, leave lastCheckpointMessageId unset so a later groups() update can retry.
      if (checkpointMessageId === this.lastCheckpointMessageId || groups.length === 0) {
        return;
      }
      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          const container = this.scrollContainer()?.nativeElement;
          const target = container?.querySelector<HTMLElement>(`[data-message-id="${checkpointMessageId}"]`);
          if (!container || !target) {
            return;
          }
          this.lastCheckpointMessageId = checkpointMessageId;
          this.stickyToBottom.set(false);
          target.scrollIntoView({ behavior: 'smooth', block: 'center' });
        });
      });
    });
  }

  /**
   * Scroll a message into view and briefly flash it. Retries across a few animation frames so
   * it works whether the target is already rendered or was just loaded (e.g. jumping to a
   * bookmark on an older page). No-op if the message never renders within the window.
   */
  scrollToMessage(messageId: string): void {
    let attempts = 0;
    const tryScroll = (): void => {
      const container = this.scrollContainer()?.nativeElement;
      const target = container?.querySelector<HTMLElement>(`[data-message-id="${messageId}"]`);
      if (container && target) {
        this.stickyToBottom.set(false);
        target.scrollIntoView({ behavior: 'smooth', block: 'center' });
        target.classList.add('message-flash');
        setTimeout(() => target.classList.remove('message-flash'), 1800);
        return;
      }
      if (attempts++ < 30) {
        requestAnimationFrame(tryScroll);
      }
    };
    requestAnimationFrame(tryScroll);
  }

  requestLoadOlder(): void {
    if (!this.hasMoreOlder() || this.loadingOlder() || this.prependInFlight) {
      return;
    }
    this.prependInFlight = true;
    this.captureAnchor();
    this.loadOlder.emit();
  }

  onScroll(event: Event): void {
    const target = event.target as HTMLElement;
    const scrollTop = target.scrollTop;
    const distanceFromBottom = target.scrollHeight - scrollTop - target.clientHeight;
    this.isNearBottom.set(distanceFromBottom < 96);
    this.stickyToBottom.set(distanceFromBottom <= AUTO_SCROLL_BOTTOM_EPSILON_PX);
    // Infinite scroll-up: pull older history as the user nears the top so scrolling back
    // through a long thread is continuous instead of a button-click-per-page grind. Only when
    // actively scrolling *up* — otherwise the initial auto-scroll-to-bottom (and the anchor
    // re-pin after a prepend), which both increase scrollTop through the top region, would
    // trigger spurious loads.
    const scrollingUp = scrollTop < this.lastScrollTop;
    this.lastScrollTop = scrollTop;
    if (scrollingUp && scrollTop <= OLDER_LOAD_THRESHOLD_PX) {
      this.requestLoadOlder();
    }
  }

  /** Record the first message currently in view and its viewport position, to re-pin after prepend. */
  private captureAnchor(): void {
    const container = this.scrollContainer()?.nativeElement;
    if (!container) return;
    const containerTop = container.getBoundingClientRect().top;
    const elements = container.querySelectorAll<HTMLElement>('[data-message-id]');
    for (const el of Array.from(elements)) {
      const top = el.getBoundingClientRect().top;
      if (top - containerTop >= 0) {
        this.pendingPrependAnchor = { id: el.getAttribute('data-message-id') ?? '', top };
        return;
      }
    }
    const first = elements[0];
    if (first) {
      this.pendingPrependAnchor = { id: first.getAttribute('data-message-id') ?? '', top: first.getBoundingClientRect().top };
    }
  }

  /**
   * Keep the anchored message pinned to its previous viewport position after a prepend. The first
   * correction must happen in this render turn: delaying it until requestAnimationFrame lets the
   * reader see the newly-prepended page shove their viewport. Follow-up frames only absorb
   * late-sizing content (avatars and portraits) above the anchor.
   */
  private restoreToAnchor(anchor: { id: string; top: number }): void {
    const container = this.scrollContainer()?.nativeElement;
    if (!container || !anchor.id) return;
    // Effects observing groups() run after Angular has rendered the updated list, so pin now
    // before the browser can paint the prepended page at the wrong visual position.
    this.pinToAnchor(container, anchor);

    let frames = 0;
    const settleLateLayout = (): void => {
      this.pinToAnchor(container, anchor);
      if (frames++ < 6) {
        requestAnimationFrame(settleLateLayout);
      }
    };
    requestAnimationFrame(settleLateLayout);
  }

  /** Apply one instant anchor correction; scheduling late layout passes stays in restoreToAnchor. */
  private pinToAnchor(container: HTMLElement, anchor: { id: string; top: number }): void {
    const el = container.querySelector<HTMLElement>(`[data-message-id="${anchor.id}"]`);
    if (!el) return;
    const delta = el.getBoundingClientRect().top - anchor.top;
    if (Math.abs(delta) <= 0.5) return;

    // The list defaults to smooth scrolling for explicit navigation. An anchor correction is
    // layout preservation, not navigation — smooth behavior here creates a visible wobble.
    const priorBehavior = container.style.scrollBehavior;
    container.style.scrollBehavior = 'auto';
    container.scrollTop += delta;
    container.style.scrollBehavior = priorBehavior;
  }

  scrollToBottom(behavior: ScrollBehavior = 'smooth', emit = true): void {
    const container = this.scrollContainer()?.nativeElement;
    if (!container) {
      if (emit) this.scrollToBottomRequested.emit();
      return;
    }
    container.scrollTo({ top: container.scrollHeight, behavior });
    this.isNearBottom.set(true);
    this.stickyToBottom.set(true);
    if (emit) this.scrollToBottomRequested.emit();
  }

  /** Run after layout so scrollHeight reflects new messages (send / assistant / stream). */
  private scheduleScrollToBottom(): void {
    requestAnimationFrame(() => {
      requestAnimationFrame(() => this.scrollToBottom('auto', false));
    });
  }

  trackGroup(item: GroupedItem, index: number): string {
    if (item.kind === 'message-group') return `messages-${item.messages[0]?.id ?? index}`;
    if (item.kind === 'tool-call-group') return `tools-${item.message.id}`;
    if (item.kind === 'model-change-divider') return `model-${item.messageId}`;
    return `system-${item.message.id}`;
  }

  assistantVisualFor(item: GroupedItem): AssistantVisual | null {
    if (item.kind !== 'message-group' || item.origin !== 'Assistant') return null;
    const seedMessage = item.messages.find(message => message.origin === 'Assistant') ?? item.messages[0];
    if (!seedMessage) {
      return {
        avatarUrl: this.assistantAvatarUrl(),
        accentColor: this.assistantAccentColor(),
        accentSurface: this.assistantAccentSurface(),
        expressionsEnabled: this.expressionsEnabled(),
      };
    }
    const resolved = this.assistantVisualResolver()(seedMessage);
    return {
      avatarUrl: resolved?.avatarUrl ?? this.assistantAvatarUrl(),
      accentColor: resolved?.accentColor ?? this.assistantAccentColor(),
      accentSurface: resolved?.accentSurface ?? this.assistantAccentSurface(),
      expressionsEnabled: this.expressionsEnabled(),
    };
  }
}

/** Digest when tail message id/length or group count changes (covers send, reply, streaming). */
function digestForTailScroll(
  groups: readonly GroupedItem[],
  displayResolver: (message: ChatMessage) => string,
  displayRevision: number,
): string {
  void displayRevision;
  const tail = lastMessageInGroups(groups);
  if (!tail) return '';
  const displayLength = (displayResolver(tail) ?? '').length;
  return `${groups.length}:${tail.id}:${displayLength}`;
}

function lastMessageInGroups(groups: readonly GroupedItem[]): ChatMessage | null {
  for (let i = groups.length - 1; i >= 0; i--) {
    const item = groups[i];
    if (item.kind === 'message-group' && item.messages.length > 0) {
      return item.messages[item.messages.length - 1]!;
    }
    if (item.kind === 'tool-call-group') {
      return item.message;
    }
    if (item.kind === 'system-message') {
      return item.message;
    }
  }
  return null;
}
