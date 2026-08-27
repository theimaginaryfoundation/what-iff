import { Injectable, OnDestroy, computed, inject, signal } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { Chat, PatchChatRequest } from '../models/chat.model';
import { apiErrorMessage } from '../utils/api-error.helpers';
import { ChatService } from './chat.service';
import {
  PersonalityOption,
  ThreadFilterState,
  ThreadGroup,
  ThreadSort,
  applyThreadFilters,
  buildThreadGroups,
  loadRecentOpenedThreadIds,
  recordRecentOpenedThreadId,
  uniquePersonalityOptions,
  uniqueTags,
} from '../../features/chat/helpers/thread-list.helpers';

const QUERY_DEBOUNCE_MS = 220;
const LIST_PAGE_SIZE = 200;

@Injectable({ providedIn: 'root' })
export class ThreadListService implements OnDestroy {
  private readonly chatService = inject(ChatService);

  private readonly allThreads = signal<Chat[]>([]);
  /** True when {@link ChatService.listAllChats} hit client-side total/page caps. */
  readonly listTruncated = signal(false);
  readonly loading = signal(false);
  /**
   * Last user-facing failure message.
   * Single-thread ops set a specific cause; bulk ops keep that when exactly one item fails,
   * and replace it with an aggregate count when multiple items fail.
   */
  readonly error = signal<string | null>(null);

  readonly query = signal('');
  readonly pinnedOnly = signal(false);
  readonly selectedTag = signal<string | null>(null);
  readonly selectedPersonalityId = signal<string | null>(null);
  /** Mirrors app-sidebar personality filter chips (thread manager + list API). */
  readonly sidebarPersonalityFilterIds = signal<readonly string[]>([]);
  readonly sort = signal<ThreadSort>('recent');
  readonly activeThreadId = signal<string | null>(null);
  readonly recentOpenedIds = signal<string[]>(loadRecentOpenedThreadIds());
  /** When true, {@link refresh} loads archived threads only (Thread Manager archived tab). */
  readonly showArchivedOnly = signal(false);
  /** Thread Manager bulk-selection: ids checked in the table. */
  readonly selectedIds = signal<ReadonlySet<string>>(new Set());
  readonly selectedCount = computed(() => this.selectedIds().size);

  private queryTimer: ReturnType<typeof setTimeout> | null = null;
  /** Monotonically increases so a slow prior list request cannot overwrite a newer filter result. */
  private refreshGeneration = 0;

  readonly tags = computed(() => uniqueTags(this.allThreads()));
  readonly personalities = computed<PersonalityOption[]>(() => uniquePersonalityOptions(this.allThreads()));

  readonly filteredThreads = computed(() =>
    applyThreadFilters(this.allThreads(), this.filterState()),
  );

  readonly groups = computed<ThreadGroup[]>(() => buildThreadGroups(this.filteredThreads()));
  readonly pinnedThreads = computed(() =>
    this.filteredThreads().filter(thread => !!thread.is_favorite && !!thread.id),
  );

  setActiveThreadId(threadId: string | null): void {
    this.activeThreadId.set(threadId);
    if (threadId) {
      this.recentOpenedIds.set(recordRecentOpenedThreadId(threadId));
    }
  }

  /** Switches list API scope between active and archived threads and reloads. */
  setArchivedPanelOnly(archived: boolean): void {
    this.clearSelection();
    if (this.showArchivedOnly() === archived) {
      return;
    }
    this.showArchivedOnly.set(archived);
    void this.refresh();
  }

  toggleSelected(id: string): void {
    const next = new Set(this.selectedIds());
    if (next.has(id)) {
      next.delete(id);
    } else {
      next.add(id);
    }
    this.selectedIds.set(next);
  }

  setAllSelected(ids: readonly string[], selected: boolean): void {
    const next = new Set(this.selectedIds());
    for (const id of ids) {
      if (selected) {
        next.add(id);
      } else {
        next.delete(id);
      }
    }
    this.selectedIds.set(next);
  }

  clearSelection(): void {
    if (this.selectedIds().size > 0) {
      this.selectedIds.set(new Set());
    }
  }

  /** Selection state of the given (visible) ids, for the header select-all checkbox. */
  selectionState(ids: readonly string[]): 'none' | 'some' | 'all' {
    if (ids.length === 0) return 'none';
    const selected = this.selectedIds();
    const selectedCount = ids.reduce((count, id) => count + (selected.has(id) ? 1 : 0), 0);
    if (selectedCount === 0) return 'none';
    return selectedCount === ids.length ? 'all' : 'some';
  }

  /** Clears local unread badge for a thread (e.g. after mark-read); does not hit the API. */
  clearUnreadForThread(threadId: string): void {
    this.allThreads.update(threads =>
      threads.map(t => (t.id === threadId ? { ...t, unread_count: 0 } : t)),
    );
  }

  setQuery(query: string): void {
    this.query.set(query);
    if (this.queryTimer) clearTimeout(this.queryTimer);
    this.queryTimer = setTimeout(() => {
      this.queryTimer = null;
      void this.refresh();
    }, QUERY_DEBOUNCE_MS);
  }

  clearFilters(): void {
    this.query.set('');
    this.pinnedOnly.set(false);
    this.selectedTag.set(null);
    this.selectedPersonalityId.set(null);
    this.sidebarPersonalityFilterIds.set([]);
    this.sort.set('recent');
    this.clearSelection();
    void this.refresh();
  }

  /** Synced from the left sidebar personality filter while browsing threads. */
  setSidebarPersonalityFilter(ids: readonly string[]): void {
    const normalized = [...ids];
    this.sidebarPersonalityFilterIds.set(normalized);
    const tableFilter = normalized.length === 1 ? normalized[0] : null;
    const tableChanged = this.selectedPersonalityId() !== tableFilter;
    if (tableChanged) {
      this.selectedPersonalityId.set(tableFilter);
    }
    if (tableChanged || normalized.length !== 1) {
      void this.refresh();
    }
  }

  /**
   * Reloads threads for the current filters. Older in-flight requests are
   * discarded so slower responses cannot replace newer filter results.
   */
  async refresh(): Promise<void> {
    const generation = ++this.refreshGeneration;
    this.clearSelection();
    this.loading.set(true);
    this.error.set(null);
    try {
      const query = this.query().trim();
      const filters: { search?: string; tag?: string; personality_id?: string; is_favorite?: boolean; archived?: boolean } = {};
      if (query) filters.search = query;
      if (this.selectedTag()) filters.tag = this.selectedTag()!;
      if (this.selectedPersonalityId()) filters.personality_id = this.selectedPersonalityId()!;
      if (this.pinnedOnly()) filters.is_favorite = true;
      if (this.showArchivedOnly()) filters.archived = true;
      const response = await firstValueFrom(
        this.chatService.listAllChats(LIST_PAGE_SIZE, Object.keys(filters).length ? filters : undefined),
      );
      if (generation !== this.refreshGeneration) return;
      this.allThreads.set(response.chats);
      this.listTruncated.set(response.truncated);
    } catch (error) {
      if (generation !== this.refreshGeneration) return;
      this.error.set(apiErrorMessage(error, 'Failed to load threads'));
    } finally {
      if (generation === this.refreshGeneration) this.loading.set(false);
    }
  }

  async renameThread(thread: Chat, name: string): Promise<void> {
    const trimmedName = name.trim();
    if (!trimmedName || trimmedName === thread.name) return;
    await this.optimisticPatch(thread, { name: trimmedName });
  }

  async togglePinned(thread: Chat): Promise<void> {
    await this.optimisticPatch(thread, { is_favorite: !thread.is_favorite });
  }

  /** @returns whether the server accepted the patch */
  async setTags(thread: Chat, tags: string[]): Promise<boolean> {
    return this.optimisticPatch(thread, { tags });
  }

  /** @returns whether the server accepted the archive change */
  async setThreadArchived(thread: Chat, archived: boolean): Promise<boolean> {
    const snapshot = this.allThreads();
    const archivedPanel = this.showArchivedOnly();
    const removeFromList =
      (!archivedPanel && archived) || (archivedPanel && !archived);
    if (removeFromList) {
      this.allThreads.set(snapshot.filter(item => item.id !== thread.id));
    } else {
      this.allThreads.set(
        snapshot.map(item =>
          item.id === thread.id ? { ...item, archived, updated_at: new Date().toISOString() } : item,
        ),
      );
    }
    try {
      const updated = await firstValueFrom(this.chatService.patchChat(thread.id, { archived }));
      if (!removeFromList) {
        this.allThreads.set(this.allThreads().map(item => (item.id === updated.id ? updated : item)));
      }
      return true;
    } catch (error) {
      this.allThreads.set(snapshot);
      this.error.set(apiErrorMessage(error, 'Failed to update thread'));
      console.error(`[ThreadList] Failed to update archive for thread ${thread.id}`, error);
      return false;
    }
  }

  /** @returns whether the server accepted the delete */
  async deleteThread(thread: Chat): Promise<boolean> {
    const snapshot = this.allThreads();
    this.allThreads.set(snapshot.filter(item => item.id !== thread.id));
    try {
      await firstValueFrom(this.chatService.deleteChat(thread.id));
      return true;
    } catch (error) {
      this.allThreads.set(snapshot);
      this.error.set(apiErrorMessage(error, 'Failed to delete thread'));
      console.error(`[ThreadList] Failed to delete thread ${thread.id}`, error);
      return false;
    }
  }

  /**
   * Best-effort bulk delete: sequential (avoids optimistic-list races).
   * Per-thread failures are logged in {@link deleteThread}; processing continues.
   * Leaves the specific {@link error} when one item fails; sets an aggregate count when several fail.
   */
  async bulkDelete(ids: readonly string[]): Promise<void> {
    const threads = this.threadsById(ids);
    let failed = 0;
    for (const thread of threads) {
      if (!(await this.deleteThread(thread))) failed++;
    }
    if (failed > 1) {
      this.error.set(`Failed to delete ${failed} of ${threads.length} threads`);
    }
    this.setAllSelected(ids, false);
  }

  /**
   * Best-effort bulk archive/restore: sequential for optimistic-list safety.
   * Per-thread failures are logged in {@link setThreadArchived}; processing continues.
   * Leaves the specific {@link error} when one item fails; sets an aggregate count when several fail.
   */
  async bulkSetArchived(ids: readonly string[], archived: boolean): Promise<void> {
    const threads = this.threadsById(ids);
    let failed = 0;
    for (const thread of threads) {
      if (!(await this.setThreadArchived(thread, archived))) failed++;
    }
    if (failed > 1) {
      const action = archived ? 'archive' : 'restore';
      this.error.set(`Failed to ${action} ${failed} of ${threads.length} threads`);
    }
    this.setAllSelected(ids, false);
  }

  /**
   * Best-effort bulk personality assign: sequential for optimistic-list safety.
   * Per-thread failures are logged in {@link optimisticPatch}; processing continues.
   * Leaves the specific {@link error} when one item fails; sets an aggregate count when several fail.
   */
  async bulkAssignPersonality(ids: readonly string[], personalityId: string): Promise<void> {
    const threads = this.threadsById(ids);
    let failed = 0;
    for (const thread of threads) {
      if (!(await this.optimisticPatch(thread, { personality_id: personalityId }))) failed++;
    }
    if (failed > 1) {
      this.error.set(`Failed to update personality on ${failed} of ${threads.length} threads`);
    }
    this.setAllSelected(ids, false);
  }

  private threadsById(ids: readonly string[]): Chat[] {
    const byId = new Map(this.allThreads().map(thread => [thread.id, thread] as const));
    return ids.map(id => byId.get(id)).filter((thread): thread is Chat => !!thread);
  }

  async unpinAll(): Promise<void> {
    const pinned = this.pinnedThreads();
    for (const thread of pinned) {
      if (!thread.is_favorite) continue;
      await this.optimisticPatch(thread, { is_favorite: false });
    }
  }

  ngOnDestroy(): void {
    if (this.queryTimer) clearTimeout(this.queryTimer);
  }

  private filterState(): ThreadFilterState {
    return {
      query: this.query(),
      pinnedOnly: this.pinnedOnly(),
      selectedTag: this.selectedTag(),
      selectedPersonalityId: this.selectedPersonalityId(),
      sidebarPersonalityIds: this.sidebarPersonalityFilterIds(),
      sort: this.sort(),
      activeThreadId: this.activeThreadId(),
    };
  }

  private async optimisticPatch(thread: Chat, patch: PatchChatRequest): Promise<boolean> {
    const snapshot = this.allThreads();
    const optimistic = mergePatch(thread, patch);
    this.allThreads.set(snapshot.map(item => (item.id === thread.id ? optimistic : item)));
    try {
      const updated = await firstValueFrom(this.chatService.patchChat(thread.id, patch));
      this.allThreads.set(this.allThreads().map(item => (item.id === updated.id ? updated : item)));
      return true;
    } catch (error) {
      this.allThreads.set(snapshot);
      this.error.set(apiErrorMessage(error, 'Failed to update thread'));
      console.error(`[ThreadList] Failed to patch thread ${thread.id}`, error);
      return false;
    }
  }
}

function mergePatch(thread: Chat, patch: PatchChatRequest): Chat {
  return {
    ...thread,
    ...(patch.name !== undefined ? { name: patch.name } : {}),
    ...(patch.tags !== undefined ? { tags: patch.tags } : {}),
    ...(patch.is_favorite !== undefined ? { is_favorite: patch.is_favorite } : {}),
    ...(patch.archived !== undefined ? { archived: patch.archived } : {}),
    updated_at: new Date().toISOString(),
  };
}
