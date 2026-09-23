
import { ChangeDetectionStrategy, Component, ElementRef, Injector, afterNextRender, computed, effect, inject, input, output, signal, viewChild } from '@angular/core';

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
  private readonly injector = inject(Injector);
  // True while an explicit jump (e.g. to a bookmark) is loading + scrolling. Suppresses the
  // auto-scroll-to-bottom: a jump prepends older batches, which grows the group count and would
  // otherwise trip the tail digest and snap the reader back to the bottom mid-jump.
  private isJumping = false;

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
      // An in-progress jump prepends older batches (growing the group count / digest) while the
      // reader is anchored at the bottom; don't let that yank them back down before we land on
      // the target. The signal deps above are still read, so normal tail auto-scroll resumes after.
      if (this.isJumping) return;
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
   * Scroll a message into view and briefly flash it, resolving to whether it succeeded. Works
   * whether the target is already rendered or was just loaded — a jump to a far-back bookmark can
   * prepend hundreds of bubbles, and their render is a separate async step from the fetch that the
   * caller already awaited. Rather than blindly polling, wait on Angular's render lifecycle:
   * `afterNextRender` fires once the just-loaded rows are in the DOM. A bounded frame poll backs it
   * up so a pathological case (no render actually scheduled) resolves false instead of hanging.
   */
  scrollToMessage(messageId: string, timeoutMs = 8000): Promise<boolean> {
    // Stop following the tail up front so a large prepend's render can't bounce the view back to
    // the bottom while we wait for the target element to appear.
    this.stickyToBottom.set(false);
    return new Promise<boolean>(resolve => {
      let settled = false;
      const finish = (ok: boolean): void => {
        if (settled) return;
        settled = true;
        resolve(ok);
      };
      const tryScroll = (): boolean => {
        const container = this.scrollContainer()?.nativeElement;
        const target = container?.querySelector<HTMLElement>(`[data-message-id="${messageId}"]`);
        if (!container || !target) return false;
        target.scrollIntoView({ behavior: 'smooth', block: 'center' });
        target.classList.add('message-flash');
        setTimeout(() => target.classList.remove('message-flash'), 1800);
        finish(true);
        return true;
      };

      // Fast path: already on screen.
      if (tryScroll()) return;

      // Primary: the prepend that just landed is pending a render; scroll the moment it commits.
      afterNextRender(() => tryScroll(), { injector: this.injector });

      // Safety net: if that render never produces the target, don't spin forever.
      const deadline = performance.now() + timeoutMs;
      const poll = (): void => {
        if (settled) return;
        if (tryScroll()) return;
        if (performance.now() < deadline) {
          requestAnimationFrame(poll);
        } else {
          finish(false);
        }
      };
      requestAnimationFrame(poll);
    });
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
    // While jumping, keep sticky off: a prepend that lands the reader momentarily near the bottom
    // must not re-arm the auto-scroll and fight the jump.
    this.stickyToBottom.set(!this.isJumping && distanceFromBottom <= AUTO_SCROLL_BOTTOM_EPSILON_PX);
    // Infinite scroll-up: pull older history as the user nears the top so scrolling back
    // through a long thread is continuous instead of a button-click-per-page grind. Only when
    // actively scrolling *up* — otherwise the initial auto-scroll-to-bottom (and the anchor
    // re-pin after a prepend), which both increase scrollTop through the top region, would
    // trigger spurious loads.
    const scrollingUp = scrollTop < this.lastScrollTop;
    this.lastScrollTop = scrollTop;
    if (!this.isJumping && scrollingUp && scrollTop <= OLDER_LOAD_THRESHOLD_PX) {
      this.requestLoadOlder();
    }
  }

  /**
   * Bracket an explicit jump (e.g. to a bookmark). `beginJump` stops following the tail so the
   * older batches the jump loads can't snap the view back to the bottom; `endJump` re-enables
   * normal tail-following once we've landed (or the jump failed). Always pair them.
   */
  beginJump(): void {
    this.isJumping = true;
    this.stickyToBottom.set(false);
  }

  endJump(): void {
    this.isJumping = false;
    // Critical: while jumping, the auto-scroll effect bailed at the sticky check every time, so it
    // never advanced lastAppliedScrollDigest past the pre-jump (small group-count) value. The
    // batches the jump prepended grew the group count, so the digest now differs — and the moment
    // the smooth scroll's first onScroll re-arms stickyToBottom near the bottom, the effect would
    // see that difference and snap to the bottom. We've deliberately parked away from the tail, so
    // mark the current tail state as already-applied (and hold sticky off) to defuse that snap.
    this.lastAppliedScrollDigest = this.tailScrollDigest();
    this.stickyToBottom.set(false);
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
    // A DOM scroll with `behavior:'auto'` does NOT mean "instant" — it defers to
    // the CSS `scroll-behavior`, which is `smooth` on this container (wanted for
    // explicit nav like the jump button / bookmarks). The stream-follow and
    // new-message snap pass 'auto' meaning instant, so force it here: otherwise
    // every ~220ms streaming tick starts a fresh smooth animation that interrupts
    // the previous one, and while the mobile soft keyboard is resizing the
    // viewport those compounding animations read as a jitter that fights the
    // resize (the chat "keeps snapping to the bottom"). Mirrors pinToAnchor,
    // which suppresses smooth for the same visible-wobble reason.
    const priorBehavior = container.style.scrollBehavior;
    if (behavior === 'auto') {
      container.style.scrollBehavior = 'auto';
    }
    container.scrollTo({ top: container.scrollHeight, behavior });
    if (behavior === 'auto') {
      container.style.scrollBehavior = priorBehavior;
    }
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

/** Digest when tail message id/length, live reasoning length, or group count changes (covers send, reply, streaming). */
function digestForTailScroll(
  groups: readonly GroupedItem[],
  displayResolver: (message: ChatMessage) => string,
  displayRevision: number,
): string {
  void displayRevision;
  const tail = lastMessageInGroups(groups);
  if (!tail) return '';
  const displayLength = (displayResolver(tail) ?? '').length;
  // Streamed reasoning grows the pending bubble before any reply text exists.
  const reasoningLength = tail.model_reasoning?.length ?? 0;
  return `${groups.length}:${tail.id}:${displayLength}:${reasoningLength}`;
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
