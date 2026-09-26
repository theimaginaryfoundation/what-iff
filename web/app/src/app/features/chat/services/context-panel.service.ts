import { Injectable, computed, signal } from '@angular/core';

import { Chat } from '../../../core/models/chat.model';
import { ContextBreakdown } from '../../../core/models/message.model';
import { RightPanelService } from '../../../core/services/right-panel.service';
import { ToolCall } from '../../../core/models/toolcall.model';

export type ContextPanelTab = 'scratchpad' | 'memories' | 'tools' | 'context';

const TAB_STORAGE_KEY = 'contextPanel.tabsByChat.v1';

@Injectable({ providedIn: 'root' })
export class ContextPanelService {
  private readonly tabsByChat = signal<Record<string, ContextPanelTab>>(readTabsFromStorage());
  private readonly _activeChat = signal<Chat | null>(null);
  private readonly _mobileOpen = signal(false);
  private readonly _composerInsert = signal<string | null>(null);
  private readonly _pendingThreadReferences = signal<Chat[]>([]);
  private readonly _composerThreadReferences = signal<Chat[]>([]);
  private readonly _toolCalls = signal<readonly ToolCall[]>([]);
  private readonly _latestBreakdown = signal<ContextBreakdown | null>(null);
  private readonly _latestBreakdownId = signal<string | null>(null);
  // A specific past turn the user pinned via "Show context"; cleared when a new turn lands.
  private readonly _pinnedBreakdown = signal<ContextBreakdown | null>(null);
  // The owning message id of the pinned past turn, tracked alongside it so the shown
  // breakdown always resolves to a message id (see shownBreakdownId).
  private readonly _pinnedBreakdownId = signal<string | null>(null);

  readonly activeChat = this._activeChat.asReadonly();
  readonly mobileOpen = this._mobileOpen.asReadonly();
  readonly composerInsert = this._composerInsert.asReadonly();
  readonly pendingThreadReferences = this._pendingThreadReferences.asReadonly();
  /** Threads attached to the message being composed; rendered as chips above the composer. */
  readonly composerThreadReferences = this._composerThreadReferences.asReadonly();
  readonly toolCalls = this._toolCalls.asReadonly();
  /** Most recent assistant turn's Context X-ray for the active chat, or null. */
  readonly latestBreakdown = this._latestBreakdown.asReadonly();
  /** The breakdown the Context tab should render: a pinned past turn, else the latest. */
  readonly shownBreakdown = computed(() => this._pinnedBreakdown() ?? this._latestBreakdown());
  /** Owning message id of shownBreakdown, or null. Mirrors the pinned-else-latest choice above. */
  readonly shownBreakdownId = computed(() =>
    this._pinnedBreakdown() ? this._pinnedBreakdownId() : this._latestBreakdownId(),
  );
  readonly visible = computed(() => this.rightPanel.visible());
  readonly activeChatId = computed(() => this._activeChat()?.id ?? null);
  readonly activeTab = computed<ContextPanelTab>(() => {
    const chatId = this.activeChatId();
    if (!chatId) return 'scratchpad';
    return this.tabsByChat()[chatId] ?? 'scratchpad';
  });

  constructor(private readonly rightPanel: RightPanelService) {}

  setActiveChat(chat: Chat | null): void {
    this._activeChat.set(chat);

    // A thread can't reference itself; drop it if the user navigated into a referenced thread.
    if (chat && this._composerThreadReferences().some(thread => thread.id === chat.id)) {
      this.removeComposerThreadReference(chat.id);
    }

    // Thread references can be queued from the thread manager while there is no
    // active composer. Once the user opens the conversation they want to send
    // from, turn those queued references into explicit next-turn context text.
    if (chat && this._pendingThreadReferences().length > 0) {
      const references = this._pendingThreadReferences();
      this._composerInsert.set(formatThreadReferences(references));
      this._pendingThreadReferences.set([]);
    }
  }

  queueThreadReference(thread: Chat): void {
    this._pendingThreadReferences.update(current => {
      if (current.some(item => item.id === thread.id)) {
        return current;
      }
      return [...current, thread];
    });
  }

  /** Adds the thread to the composer references, or removes it if already attached. */
  toggleComposerThreadReference(thread: Chat): void {
    if (thread.id === this.activeChatId()) {
      return;
    }
    this._composerThreadReferences.update(current =>
      current.some(item => item.id === thread.id)
        ? current.filter(item => item.id !== thread.id)
        : [...current, thread],
    );
  }

  removeComposerThreadReference(threadId: string): void {
    this._composerThreadReferences.update(current => current.filter(thread => thread.id !== threadId));
  }

  /** Replaces the composer references wholesale (e.g. restoring a selection snapshot). */
  setComposerThreadReferences(threads: readonly Chat[]): void {
    const activeId = this.activeChatId();
    this._composerThreadReferences.set(threads.filter(thread => thread.id !== activeId));
  }

  clearComposerThreadReferences(): void {
    this._composerThreadReferences.set([]);
  }

  /** Text block sent ahead of the user's message for the attached threads ('' when none). */
  composerThreadReferencesText(): string {
    const references = this._composerThreadReferences();
    return references.length > 0 ? formatThreadReferences(references) : '';
  }

  removePendingThreadReference(threadId: string): void {
    this._pendingThreadReferences.update(current => current.filter(thread => thread.id !== threadId));
  }

  setToolCalls(toolCalls: readonly ToolCall[]): void {
    this._toolCalls.set(toolCalls);
  }

  setLatestBreakdown(breakdown: ContextBreakdown | null, messageId: string | null = null): void {
    // A genuinely new turn (new owning message id) replaces any pinned past turn, so the
    // panel follows the conversation forward after each send.
    if (messageId !== null && messageId !== this._latestBreakdownId()) {
      this._pinnedBreakdown.set(null);
      this._pinnedBreakdownId.set(null);
    }
    this._latestBreakdown.set(breakdown);
    this._latestBreakdownId.set(messageId);
  }

  /** Pin a specific past turn's breakdown in the Context tab (from a message's "Show context"). */
  selectBreakdown(breakdown: ContextBreakdown | null, messageId: string | null = null): void {
    this._pinnedBreakdown.set(breakdown);
    this._pinnedBreakdownId.set(messageId);
  }

  setActiveTab(tab: ContextPanelTab): void {
    const chatId = this.activeChatId();
    if (!chatId) return;
    const next = { ...this.tabsByChat(), [chatId]: tab };
    this.tabsByChat.set(next);
    writeTabsToStorage(next);
  }

  openMobile(): void {
    this._mobileOpen.set(true);
  }

  closeMobile(): void {
    this._mobileOpen.set(false);
  }

  setDesktopVisible(visible: boolean): void {
    this.rightPanel.setVisible(visible);
  }

  requestComposerInsert(text: string): void {
    this._composerInsert.set(text);
  }

  consumeComposerInsert(): string | null {
    const value = this._composerInsert();
    this._composerInsert.set(null);
    return value;
  }
}

function formatThreadReferences(threads: readonly Chat[]): string {
  const lines = threads.map(thread =>
    `[Thread context: ${JSON.stringify(thread.name)}; thread_id=${JSON.stringify(thread.id)}]`,
  );
  return `${lines.join('\n')}\n`;
}

function readTabsFromStorage(): Record<string, ContextPanelTab> {
  try {
    const raw = localStorage.getItem(TAB_STORAGE_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as Record<string, ContextPanelTab>;
    return parsed ?? {};
  } catch {
    return {};
  }
}

function writeTabsToStorage(tabs: Record<string, ContextPanelTab>): void {
  try {
    localStorage.setItem(TAB_STORAGE_KEY, JSON.stringify(tabs));
  } catch {
    // Ignore storage failures (private mode/quota).
  }
}
