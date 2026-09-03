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
  private readonly _toolCalls = signal<readonly ToolCall[]>([]);
  private readonly _latestBreakdown = signal<ContextBreakdown | null>(null);
  private readonly _latestBreakdownId = signal<string | null>(null);
  // A specific past turn the user pinned via "Show context"; cleared when a new turn lands.
  private readonly _pinnedBreakdown = signal<ContextBreakdown | null>(null);

  readonly activeChat = this._activeChat.asReadonly();
  readonly mobileOpen = this._mobileOpen.asReadonly();
  readonly composerInsert = this._composerInsert.asReadonly();
  readonly pendingThreadReferences = this._pendingThreadReferences.asReadonly();
  readonly toolCalls = this._toolCalls.asReadonly();
  /** Most recent assistant turn's Context X-ray for the active chat, or null. */
  readonly latestBreakdown = this._latestBreakdown.asReadonly();
  /** The breakdown the Context tab should render: a pinned past turn, else the latest. */
  readonly shownBreakdown = computed(() => this._pinnedBreakdown() ?? this._latestBreakdown());
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
    }
    this._latestBreakdown.set(breakdown);
    this._latestBreakdownId.set(messageId);
  }

  /** Pin a specific past turn's breakdown in the Context tab (from a message's "Show context"). */
  selectBreakdown(breakdown: ContextBreakdown | null): void {
    this._pinnedBreakdown.set(breakdown);
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
