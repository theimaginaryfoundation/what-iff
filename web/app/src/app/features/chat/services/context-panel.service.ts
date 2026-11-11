import { DOCUMENT } from '@angular/common';
import { Injectable, computed, inject, signal } from '@angular/core';

import { Chat } from '../../../core/models/chat.model';
import { ContextBreakdown } from '../../../core/models/message.model';
import { FileAttachment } from '../../../core/models/file-attachment.model';
import { RightPanelService } from '../../../core/services/right-panel.service';
import { ToolCall } from '../../../core/models/toolcall.model';
import { formatThreadReferences } from '../helpers/thread-reference.helpers';

export type ContextPanelTab = 'scratchpad' | 'memories' | 'tools' | 'files' | 'context';

const TAB_STORAGE_KEY = 'contextPanel.tabsByChat.v1';
/**
 * Whether the docked (desktop) panel was last left open or closed (#300). Only the docked panel
 * is remembered: below the desktop breakpoint the panel is a sheet over the chat, which always
 * starts closed and whose open/close does not touch this value.
 */
export const DESKTOP_OPEN_STORAGE_KEY = 'contextPanel.desktopOpen.v1';
/** Below this the layout shows the panel as a sheet, not docked (AppLayoutComponent's 1024px). */
const MOBILE_VIEWPORT_QUERY = '(max-width: 1023px)';

@Injectable({ providedIn: 'root' })
export class ContextPanelService {
  private readonly tabsByChat = signal<Record<string, ContextPanelTab>>(readTabsFromStorage());
  private readonly _activeChat = signal<Chat | null>(null);
  private readonly _mobileOpen = signal(false);
  private readonly _composerInsert = signal<string | null>(null);
  // Files waiting to be attached to the next message (an upload from the Files tab, a gallery pick, a
  // Save As or markup from the file workspace), consumed by the chat page into the composer's
  // pending attachments. A queue: markup adds several at once (ADR 0x045).
  private readonly _composerAttachments = signal<readonly FileAttachment[]>([]);
  // The files attached to the message being composed, published by the chat page so the Files tab
  // can show them as "next message" before they are sent.
  private readonly _composerPending = signal<readonly FileAttachment[]>([]);
  // Bumped when the Files tab should reload: a reply landed, the app regained focus, a file changed.
  private readonly _filesVersion = signal(0);
  // Bumped when the Files tab asks the composer to open its gallery picker.
  private readonly _galleryPickerRequests = signal(0);
  private readonly _composerThreadReferences = signal<Chat[]>([]);
  private readonly _toolCalls = signal<readonly ToolCall[]>([]);
  private readonly _latestBreakdown = signal<ContextBreakdown | null>(null);
  private readonly _latestBreakdownId = signal<string | null>(null);
  // A specific past turn the user pinned via "Show context"; cleared when a new turn lands.
  private readonly _pinnedBreakdown = signal<ContextBreakdown | null>(null);
  // The owning message id of the pinned past turn, tracked alongside it so the shown
  // breakdown always resolves to a message id (see shownBreakdownId).
  private readonly _pinnedBreakdownId = signal<string | null>(null);

  // Latest server copy of the active chat after a panel-side change (e.g. sandbox). The
  // chat page folds it into the session's thread, which owns the chat and feeds activeChat back.
  private readonly _threadUpdate = signal<Chat | null>(null);

  readonly activeChat = this._activeChat.asReadonly();
  readonly threadUpdate = this._threadUpdate.asReadonly();
  readonly mobileOpen = this._mobileOpen.asReadonly();
  readonly composerInsert = this._composerInsert.asReadonly();
  readonly composerAttachments = this._composerAttachments.asReadonly();
  readonly filesVersion = this._filesVersion.asReadonly();
  readonly composerPending = this._composerPending.asReadonly();
  readonly galleryPickerRequests = this._galleryPickerRequests.asReadonly();
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

  private readonly document = inject(DOCUMENT);

  constructor(private readonly rightPanel: RightPanelService) {}

  setActiveChat(chat: Chat | null): void {
    const previousId = this.activeChatId();
    this._activeChat.set(chat);
    this.rebindComposerThreadReferences(previousId, chat?.id ?? null);
  }

  /** Publishes a chat the panel just PATCHed so the session adopts it instead of keeping a stale copy. */
  publishThreadUpdate(chat: Chat): void {
    this._activeChat.set(chat);
    this._threadUpdate.set(chat);
  }

  /**
   * Keeps attached threads tied to the chat they were attached for. References added while a
   * chat is open belong to it and are dropped when the user moves to another chat (or none).
   * References added with no chat open (e.g. from the Thread Manager) are unbound and follow
   * the user into the next chat they open, minus that chat itself (no self-references).
   */
  private rebindComposerThreadReferences(previousId: string | null, activeId: string | null): void {
    if (previousId === activeId) {
      return;
    }
    if (previousId !== null) {
      this._composerThreadReferences.set([]);
    } else if (activeId !== null) {
      this._composerThreadReferences.update(current => current.filter(thread => thread.id !== activeId));
    }
  }

  isComposerThreadReferenced(threadId: string): boolean {
    return this._composerThreadReferences().some(thread => thread.id === threadId);
  }

  /** Adds the thread to the composer references, or removes it if already attached. */
  toggleComposerThreadReference(thread: Chat): void {
    if (this.isComposerThreadReferenced(thread.id)) {
      this.removeComposerThreadReference(thread.id);
      return;
    }
    // A thread can't reference itself.
    if (!thread.id || thread.id === this.activeChatId()) {
      return;
    }
    this._composerThreadReferences.update(current => [...current, thread]);
  }

  removeComposerThreadReference(threadId: string): void {
    this._composerThreadReferences.update(current => current.filter(thread => thread.id !== threadId));
  }

  /** Replaces the composer references wholesale (e.g. restoring a selection snapshot). */
  setComposerThreadReferences(threads: readonly Chat[]): void {
    const activeId = this.activeChatId();
    this._composerThreadReferences.set(threads.filter(thread => !!thread.id && thread.id !== activeId));
  }

  /** Asks the chat page to add attachment to the message being composed (ADR 0x043 §4). */
  requestComposerAttachment(attachment: FileAttachment): void {
    this._composerAttachments.update(queued => [...queued, attachment]);
  }

  /** Takes every queued attachment, oldest first. */
  consumeComposerAttachments(): readonly FileAttachment[] {
    const queued = this._composerAttachments();
    this._composerAttachments.set([]);
    return queued;
  }

  setComposerPending(files: readonly FileAttachment[]): void {
    this._composerPending.set(files);
  }

  /** Tells the Files tab to reload the thread's files. */
  refreshFiles(): void {
    this._filesVersion.update(n => n + 1);
  }

  /** Asks the composer to open its "Add from gallery" picker. */
  requestGalleryPicker(): void {
    this._galleryPickerRequests.update(n => n + 1);
  }

  clearComposerThreadReferences(): void {
    this._composerThreadReferences.set([]);
  }

  /** Text block sent ahead of the user's message for the attached threads ('' when none). */
  composerThreadReferencesText(): string {
    return formatThreadReferences(this._composerThreadReferences());
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

  /**
   * Shows or hides the docked panel as the user's choice. On a desktop-width viewport the choice is
   * remembered for the next page load (see restoreDesktopVisible); on a narrower one the docked
   * panel isn't shown, so the sheet flows calling this leave the remembered value alone.
   */
  setDesktopVisible(visible: boolean): void {
    this.rightPanel.setVisible(visible);
    if (!this.isMobileViewport()) writeStored(DESKTOP_OPEN_STORAGE_KEY, visible);
  }

  /** Shows the docked panel as the user last left it (open when nothing is stored). */
  restoreDesktopVisible(): void {
    this.rightPanel.setVisible(readStored(DESKTOP_OPEN_STORAGE_KEY) !== false);
  }

  /**
   * Gets out of the way of a file that is opening (ADR 0x043 §1): the panel would cover it. This
   * is not the user's choice of open or closed, so the remembered value stays as it was.
   */
  hideForOpenFile(): void {
    this.rightPanel.setVisible(false);
    this._mobileOpen.set(false);
  }

  /** Hides the docked panel when the chat page goes away, keeping the remembered choice. */
  hideOnPageExit(): void {
    this.rightPanel.setVisible(false);
  }

  requestComposerInsert(text: string): void {
    this._composerInsert.set(text);
  }

  consumeComposerInsert(): string | null {
    const value = this._composerInsert();
    this._composerInsert.set(null);
    return value;
  }

  /**
   * Whether the layout shows the panel as a sheet rather than docked. The one place that decides
   * it, so the sheet flows (ChatPageComponent) and what is remembered agree on the breakpoint.
   */
  isMobileViewport(): boolean {
    return !!this.document.defaultView?.matchMedia?.(MOBILE_VIEWPORT_QUERY).matches;
  }
}

function readTabsFromStorage(): Record<string, ContextPanelTab> {
  const parsed = readStored(TAB_STORAGE_KEY);
  return parsed && typeof parsed === 'object' ? (parsed as Record<string, ContextPanelTab>) : {};
}

function writeTabsToStorage(tabs: Record<string, ContextPanelTab>): void {
  writeStored(TAB_STORAGE_KEY, tabs);
}

/** The parsed JSON stored under `key`; undefined when missing, unreadable or storage is unavailable. */
function readStored(key: string): unknown {
  try {
    const raw = localStorage.getItem(key);
    return raw ? JSON.parse(raw) : undefined;
  } catch {
    return undefined;
  }
}

function writeStored(key: string, value: unknown): void {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    // Ignore storage failures (private mode/quota).
  }
}
