import { NULL_PERSONALITY_ID } from '../../../core/constants/app.constants';
import { Chat } from '../../../core/models/chat.model';

export type ThreadSort = 'recent' | 'newest' | 'alphabetical';

export interface ThreadGroup {
  id: string;
  label: string;
  threads: Chat[];
}

export interface PersonalityOption {
  id: string;
  label: string;
}

export interface ThreadFilterState {
  query: string;
  pinnedOnly: boolean;
  selectedTag: string | null;
  selectedPersonalityId: string | null;
  /** Sidebar multi-select; when non-empty, threads must match one of these personality IDs. */
  sidebarPersonalityIds: readonly string[];
  sort: ThreadSort;
  activeThreadId: string | null;
}

const DAY_MS = 24 * 60 * 60 * 1000;

export function applyThreadFilters(chats: readonly Chat[], state: ThreadFilterState): Chat[] {
  const normalizedQuery = state.query.trim().toLowerCase();
  const filtered = chats.filter(chat => {
    if (state.pinnedOnly && !chat.is_favorite) return false;
    if (state.selectedTag && !(chat.tags ?? []).includes(state.selectedTag)) return false;
    if (state.selectedPersonalityId && chat.personality_id !== state.selectedPersonalityId) return false;
    if (state.sidebarPersonalityIds.length > 0) {
      if (!chat.personality_id || !state.sidebarPersonalityIds.includes(chat.personality_id)) {
        return false;
      }
    }
    if (!normalizedQuery) return true;
    const fields = [chat.name, ...(chat.tags ?? [])].map(value => value.toLowerCase());
    return fields.some(value => value.includes(normalizedQuery));
  });

  const sorted = [...filtered].sort((a, b) => compareThreads(a, b, state.sort));
  if (!state.activeThreadId) return sorted;

  const active = chats.find(chat => chat.id === state.activeThreadId);
  if (!active) return sorted;
  if (sorted.some(chat => chat.id === active.id)) return sorted;
  return [active, ...sorted];
}

export function buildThreadGroups(chats: readonly Chat[], now = new Date()): ThreadGroup[] {
  const pinned = chats.filter(chat => chat.is_favorite);
  const unpinned = chats.filter(chat => !chat.is_favorite);
  const groups: ThreadGroup[] = [];
  if (pinned.length > 0) {
    groups.push({
      id: 'pinned',
      label: 'Starred',
      threads: pinned,
    });
  }

  const byBucket = new Map<string, Chat[]>();
  for (const chat of unpinned) {
    const bucket = bucketForDate(chat.last_message_time ?? chat.updated_at, now);
    const existing = byBucket.get(bucket.id) ?? [];
    existing.push(chat);
    byBucket.set(bucket.id, existing);
  }

  for (const bucket of BUCKETS) {
    const threads = byBucket.get(bucket.id);
    if (!threads || threads.length === 0) continue;
    groups.push({
      id: bucket.id,
      label: bucket.label,
      threads,
    });
  }
  return groups;
}

export function uniqueTags(chats: readonly Chat[]): string[] {
  const values = new Set<string>();
  chats.forEach(chat => (chat.tags ?? []).forEach(tag => values.add(tag)));
  return [...values].sort((a, b) => a.localeCompare(b));
}

export function uniquePersonalityOptions(chats: readonly Chat[]): PersonalityOption[] {
  const values = new Map<string, string>();
  chats.forEach(chat => {
    const pid = chat.personality_id;
    if (!pid || pid === NULL_PERSONALITY_ID) return;
    const existing = values.get(pid);
    if (existing) return;
    values.set(pid, chat.personality_name || pid);
  });
  return [...values.entries()]
    .map(([id, label]) => ({ id, label }))
    .sort((a, b) => a.label.localeCompare(b.label));
}

function compareThreads(a: Chat, b: Chat, sort: ThreadSort): number {
  if (sort === 'alphabetical') {
    return a.name.localeCompare(b.name);
  }
  if (sort === 'newest') {
    return toMillis(b.created_at) - toMillis(a.created_at);
  }
  return toMillis(b.last_message_time ?? b.updated_at) - toMillis(a.last_message_time ?? a.updated_at);
}

function toMillis(value: string | undefined): number {
  if (!value) return 0;
  const millis = Date.parse(value);
  return Number.isNaN(millis) ? 0 : millis;
}

const BUCKETS = [
  { id: 'today', label: 'Today' },
  { id: 'yesterday', label: 'Yesterday' },
  { id: 'this-week', label: 'Earlier this week' },
  { id: 'this-month', label: 'Earlier this month' },
  { id: 'older', label: 'Older' },
] as const;

function bucketForDate(value: string, now: Date): { id: string; label: string } {
  const timestamp = Date.parse(value);
  if (Number.isNaN(timestamp)) return BUCKETS[BUCKETS.length - 1];

  const eventDate = new Date(timestamp);
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const eventStart = new Date(eventDate.getFullYear(), eventDate.getMonth(), eventDate.getDate()).getTime();
  const ageDays = Math.floor((startOfToday - eventStart) / DAY_MS);

  if (ageDays <= 0) return BUCKETS[0];
  if (ageDays === 1) return BUCKETS[1];
  if (ageDays < 7) return BUCKETS[2];
  if (eventDate.getMonth() === now.getMonth() && eventDate.getFullYear() === now.getFullYear()) return BUCKETS[3];
  return BUCKETS[4];
}

export const RECENT_OPENED_THREAD_IDS_KEY = 'recentOpenedThreadIds';
const RECENT_OPENED_THREAD_IDS_CAP = 20;
export const SIDEBAR_RECENT_THREAD_LIMIT = 5;

function recentOpenedThreadStorage(): Storage | null {
  if (typeof localStorage === 'undefined') {
    return null;
  }
  return localStorage;
}

export function loadRecentOpenedThreadIds(): string[] {
  const storage = recentOpenedThreadStorage();
  if (!storage) return [];
  try {
    const raw = storage.getItem(RECENT_OPENED_THREAD_IDS_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((id): id is string => typeof id === 'string' && id.length > 0);
  } catch {
    return [];
  }
}

export function recordRecentOpenedThreadId(threadId: string): string[] {
  const normalized = threadId.trim();
  if (!normalized) return loadRecentOpenedThreadIds();
  const next = [normalized, ...loadRecentOpenedThreadIds().filter(id => id !== normalized)].slice(
    0,
    RECENT_OPENED_THREAD_IDS_CAP,
  );
  const storage = recentOpenedThreadStorage();
  if (!storage) return next;
  try {
    storage.setItem(RECENT_OPENED_THREAD_IDS_KEY, JSON.stringify(next));
  } catch {
    // ignore quota / privacy mode
  }
  return next;
}

export function clearRecentOpenedThreadIds(): void {
  const storage = recentOpenedThreadStorage();
  if (!storage) return;
  try {
    storage.removeItem(RECENT_OPENED_THREAD_IDS_KEY);
  } catch {
    // ignore
  }
}

/** Sidebar recent list: opened threads first, then last message, then created time. */
export function pickSidebarRecentThreads(
  threads: readonly Chat[],
  openedIds: readonly string[],
  limit = SIDEBAR_RECENT_THREAD_LIMIT,
): Chat[] {
  const candidates = threads.filter(thread => !!thread.id && !thread.is_favorite && !thread.archived);
  const openedRank = new Map(openedIds.map((id, index) => [id, index] as const));
  return [...candidates]
    .sort((a, b) => compareRecentSidebarThreads(a, b, openedRank))
    .slice(0, limit);
}

function compareRecentSidebarThreads(
  a: Chat,
  b: Chat,
  openedRank: ReadonlyMap<string, number>,
): number {
  const rankA = recentSidebarThreadRank(a, openedRank);
  const rankB = recentSidebarThreadRank(b, openedRank);
  if (rankA[0] !== rankB[0]) return rankA[0] - rankB[0];
  return rankA[1] - rankB[1];
}

function recentSidebarThreadRank(
  thread: Chat,
  openedRank: ReadonlyMap<string, number>,
): readonly [tier: number, tieBreaker: number] {
  const opened = openedRank.get(thread.id);
  if (opened !== undefined) {
    return [0, opened];
  }
  const messageMillis = toMillis(thread.last_message_time);
  if (messageMillis > 0) {
    return [1, -messageMillis];
  }
  return [2, -toMillis(thread.created_at)];
}
