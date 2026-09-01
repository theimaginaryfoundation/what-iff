import { CommonModule, DatePipe, UpperCasePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, inject, OnInit, signal } from '@angular/core';
import { Router, RouterLink } from '@angular/router';
import { forkJoin, map, of } from 'rxjs';

import {
  CheckpointSnapshot,
  CompactionEvent,
  CompactionLoadedMemory,
  MemoryMergeEvent,
} from '../../core/models/memory.model';
import { Chat } from '../../core/models/chat.model';
import { Personality, PersonalityPromptChange } from '../../core/models/personality.model';
import { ChatService } from '../../core/services/chat.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { MemoryService } from '../../core/services/memory.service';
import { PersonalityService } from '../../core/services/personality.service';

type PromptAuditEntry = PersonalityPromptChange & { personality_name: string };

@Component({
  selector: 'app-compaction-log-page',
  standalone: true,
  imports: [CommonModule, DatePipe, RouterLink, UpperCasePipe],
  templateUrl: './compaction-log-page.component.html',
  styleUrl: './compaction-log-page.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class CompactionLogPageComponent implements OnInit {
  private readonly memoryService = inject(MemoryService);
  private readonly confirmation = inject(ConfirmationService);
  private readonly router = inject(Router);
  private readonly chatService = inject(ChatService);
  private readonly personalityService = inject(PersonalityService);

  readonly events = signal<CompactionEvent[]>([]);
  readonly promptChanges = signal<PromptAuditEntry[]>([]);
  readonly promptChangesLoading = signal(false);
  readonly promptChangesError = signal<string | null>(null);
  readonly promptChangesExpanded = signal(false);
  readonly chats = signal<readonly Chat[]>([]);
  readonly personalities = signal<readonly Personality[]>([]);
  readonly selectedChatID = signal('');
  readonly selectedPersonalityID = signal('');
  readonly loading = signal(true);
  readonly error = signal<string | null>(null);
  readonly expandedIds = signal<Set<string>>(new Set());
  readonly collapsedSummaryIds = signal<Set<string>>(new Set());
  readonly collapsedMemoryIds = signal<Set<string>>(new Set());
  readonly collapsedLoadedMemoryIds = signal<Set<string>>(new Set());
  readonly revertingId = signal<string | null>(null);
  readonly revertingPromptChangeId = signal<string | null>(null);
  readonly revertedIds = signal<Set<string>>(new Set());
  readonly notice = signal<string | null>(null);
  readonly page = signal(1);
  readonly totalCount = signal(0);
  readonly totalPages = signal(1);

  private static readonly PAGE_SIZE = 20;

  ngOnInit(): void {
    this.chatService.listChats(1, 100).subscribe({ next: response => this.chats.set(response.results ?? []) });
    this.personalityService.listPersonalities(1, 100).subscribe({
      next: response => {
        this.personalities.set(response.results ?? []);
        this.loadPromptChanges();
      },
    });
    this.load(1);
  }

  load(page: number): void {
    this.loading.set(true);
    this.error.set(null);
    this.memoryService.listCompactionEvents(page, CompactionLogPageComponent.PAGE_SIZE, {
      chat_id: this.selectedChatID() || undefined,
      personality_id: this.selectedPersonalityID() || undefined,
    }).subscribe({
      next: response => {
        this.events.set(response.results ?? []);
        this.page.set(response.page ?? page);
        this.totalCount.set(response.total_count ?? 0);
        this.totalPages.set(Math.max(1, Math.ceil((response.total_count ?? 0) / CompactionLogPageComponent.PAGE_SIZE)));
        this.loading.set(false);
      },
      error: err => {
        this.error.set(err instanceof Error ? err.message : 'Failed to load compaction log');
        this.loading.set(false);
      },
    });
  }

  private loadPromptChanges(): void {
    const personalities = this.personalities();
    const selected = this.selectedPersonalityID();
    const targets = selected ? personalities.filter(item => item.id === selected) : [...personalities];

    if (targets.length === 0) {
      this.promptChanges.set([]);
      this.promptChangesLoading.set(false);
      return;
    }

    this.promptChangesLoading.set(true);
    this.promptChangesError.set(null);
    const requests = targets.map(personality =>
      this.personalityService.listPromptChanges(personality.id).pipe(
        map(changes => changes.map(change => ({ ...change, personality_name: personality.name }))),
      ),
    );

    (requests.length ? forkJoin(requests) : of([] as PromptAuditEntry[][])).subscribe({
      next: groups => {
        const changes = groups.flat().sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at));
        this.promptChanges.set(changes);
        this.promptChangesLoading.set(false);
      },
      error: err => {
        this.promptChangesError.set(err instanceof Error ? err.message : 'Failed to load personality prompt changes');
        this.promptChangesLoading.set(false);
      },
    });
  }

  togglePromptChanges(): void {
    this.promptChangesExpanded.update(expanded => !expanded);
  }

  updateFilters(chatID: string, personalityID: string): void {
    this.selectedChatID.set(chatID);
    this.selectedPersonalityID.set(personalityID);
    this.load(1);
    this.loadPromptChanges();
  }

  async revertPromptChange(change: PromptAuditEntry): Promise<void> {
    if (this.revertingPromptChangeId()) return;
    const confirmed = await this.confirmation.confirm({
      title: 'Restore personality prompt?',
      message: `Restore ${change.personality_name} to the prompt from before this change? The restore will be recorded as a new audit entry.`,
      type: 'warning',
      confirmText: 'Restore',
      cancelText: 'Cancel',
    });
    if (!confirmed) return;

    this.revertingPromptChangeId.set(change.id);
    this.notice.set(null);
    this.personalityService.revertPromptChange(change.personality_id, change.id).subscribe({
      next: () => {
        this.revertingPromptChangeId.set(null);
        this.notice.set(`Prompt restored for ${change.personality_name}.`);
        this.loadPromptChanges();
      },
      error: err => {
        this.revertingPromptChangeId.set(null);
        this.promptChangesError.set(err instanceof Error ? err.message : 'Failed to restore personality prompt');
      },
    });
  }

  checkpointReasonLabel(reason: string | null | undefined): string {
    const raw = reason ?? '';
    if (raw.startsWith('assistant_messages_since_checkpoint')) return 'Turn limit reached';
    if (raw.startsWith('last_input_tokens')) return 'Input size limit reached';
    if (raw.startsWith('estimated_context_tokens')) return 'Conversation size limit reached';
    return raw || 'Checkpoint created';
  }

  isExpanded(event: CompactionEvent): boolean {
    return this.expandedIds().has(event.id);
  }

  toggleExpanded(event: CompactionEvent): void {
    const next = new Set(this.expandedIds());
    if (next.has(event.id)) next.delete(event.id);
    else next.add(event.id);
    this.expandedIds.set(next);
  }

  isSectionCollapsed(event: CompactionEvent, section: 'summary' | 'memory'): boolean {
    return (section === 'summary' ? this.collapsedSummaryIds() : this.collapsedMemoryIds()).has(event.id);
  }

  toggleSection(event: CompactionEvent, section: 'summary' | 'memory'): void {
    const source = section === 'summary' ? this.collapsedSummaryIds() : this.collapsedMemoryIds();
    const next = new Set(source);
    next.has(event.id) ? next.delete(event.id) : next.add(event.id);
    if (section === 'summary') this.collapsedSummaryIds.set(next);
    else this.collapsedMemoryIds.set(next);
  }

  isLoadedMemoriesCollapsed(event: CompactionEvent): boolean {
    return this.collapsedLoadedMemoryIds().has(event.id);
  }

  toggleLoadedMemories(event: CompactionEvent): void {
    const next = new Set(this.collapsedLoadedMemoryIds());
    next.has(event.id) ? next.delete(event.id) : next.add(event.id);
    this.collapsedLoadedMemoryIds.set(next);
  }

  summaryChanged(event: CompactionEvent): boolean {
    return (event.old_summary?.content ?? '') !== (event.new_summary?.content ?? '');
  }

  scratchpadChanged(event: CompactionEvent): boolean {
    return (event.old_scratchpad?.content ?? '') !== (event.new_scratchpad?.content ?? '');
  }

  mergeSummary(event: CompactionEvent): string {
    const created = event.created_memories?.length ?? 0;
    const updates = this.updatedMemoryEvents(event);
    if (created === 0 && updates.length === 0) return 'No memory changes';
    const counts = { fold_live: 0, link: 0 } as Record<string, number>;
    for (const me of updates) counts[me.merge_type] = (counts[me.merge_type] ?? 0) + 1;
    const parts: string[] = [];
    if (created) parts.push(`${created} created`);
    if (counts['fold_live']) parts.push(`${counts['fold_live']} merged`);
    if (counts['link']) parts.push(`${counts['link']} linked`);
    return parts.join(' · ');
  }

  mergeTypeLabel(event: MemoryMergeEvent): string {
    switch (event.merge_type) {
      case 'link': return 'Linked';
      case 'fold_live': return 'Memories Merged';
      default: return 'Updated';
    }
  }

  mergedSourceCount(event: MemoryMergeEvent): number {
    const fromMembers = event.source_members?.length ?? 0;
    if (fromMembers > 0) return fromMembers;
    return Math.max(event.duplicates_folded + 1, 0);
  }

  createdMemories(event: CompactionEvent): CompactionLoadedMemory[] {
    return event.created_memories ?? [];
  }

  updatedMemoryEvents(event: CompactionEvent): MemoryMergeEvent[] {
    return (event.merge_events ?? []).filter(item => item.merge_type !== 'create');
  }

  isSnapshotReverted(snapshot: CheckpointSnapshot | null | undefined): boolean {
    return !!snapshot && this.revertedIds().has(snapshot.id);
  }

  async revert(snapshot: CheckpointSnapshot | null | undefined): Promise<void> {
    if (!snapshot || this.revertingId()) return;

    const isScratchpad = snapshot.kind === 'scratchpad';
    const confirmed = await this.confirmation.confirm({
      title: isScratchpad ? 'Restore personality scratchpad?' : 'Restore conversation summary?',
      message: isScratchpad
        ? 'This restores the scratchpad for every chat using this personality. Continue?'
        : 'This restores this conversation to the selected checkpoint summary. Continue?',
      type: 'warning',
      confirmText: 'Restore',
      cancelText: 'Cancel',
    });
    if (!confirmed) return;

    const label = snapshot.kind === 'scratchpad' ? 'scratchpad' : 'summary';
    this.revertingId.set(snapshot.id);
    this.notice.set(null);
    this.memoryService.revertSnapshot(snapshot.id).subscribe({
      next: () => {
        this.revertingId.set(null);
        const next = new Set(this.revertedIds());
        next.add(snapshot.id);
        this.revertedIds.set(next);
        this.notice.set(
          snapshot.kind === 'scratchpad'
            ? 'Scratchpad restored for this personality (shared across its chats).'
            : 'Conversation summary restored for this thread.',
        );
      },
      error: err => {
        this.revertingId.set(null);
        this.error.set(err instanceof Error ? err.message : `Failed to revert ${label}`);
      },
    });
  }

  openMemory(memoryId: string | null | undefined): void {
    if (memoryId) void this.router.navigate(['/memories', memoryId]);
  }

  openThread(event: CompactionEvent): void {
    void this.router.navigate(['/chat', event.chat_id], {
      queryParams: event.assistant_message_id ? { checkpoint: event.assistant_message_id } : undefined,
    });
  }

  goToPage(page: number): void {
    if (page < 1 || page > this.totalPages()) return;
    this.load(page);
  }
}
