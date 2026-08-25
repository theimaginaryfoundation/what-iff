import { CommonModule } from '@angular/common';
import { ChangeDetectionStrategy, Component, OnChanges, SimpleChanges, computed, inject, input, signal } from '@angular/core';
import { Router } from '@angular/router';
import { firstValueFrom } from 'rxjs';

import { Memory } from '../../../../../core/models/memory.model';
import { MemoryService } from '../../../../../core/services/memory.service';
import { apiErrorMessage } from '../../../../../core/utils/api-error.helpers';
import { ButtonComponent } from '../../../../../shared/ui/button/button.component';
import { ModalComponent } from '../../../../../shared/ui/modal/modal.component';

type MemoryContextTab = 'thread' | 'global';

@Component({
  selector: 'app-context-memories-tab',
  standalone: true,
  imports: [CommonModule, ButtonComponent, ModalComponent],
  template: `
    <section class="tab-body">
      <div class="context-tabs" role="tablist" aria-label="Memory scope">
        <button
          type="button"
          role="tab"
          [attr.aria-selected]="activeMemoryTab() === 'thread'"
          [class.context-tabs__button--active]="activeMemoryTab() === 'thread'"
          (click)="activeMemoryTab.set('thread')"
        >
          This Thread
        </button>
        <button
          type="button"
          role="tab"
          [attr.aria-selected]="activeMemoryTab() === 'global'"
          [class.context-tabs__button--active]="activeMemoryTab() === 'global'"
          (click)="activeMemoryTab.set('global')"
        >
          Global
        </button>
      </div>

      <div class="memory-content">
        @if (loading()) {
          <p class="state">Loading memories…</p>
        } @else if (error(); as error) {
          <p class="error" role="alert">{{ error }}</p>
        } @else {
          <ng-container *ngTemplateOutlet="memoryList; context: { $implicit: visibleMemories() }" />
        }
      </div>

      <div class="memory-actions">
        <button type="button" class="add-memory" (click)="openCreateModal()">Add Memory</button>
        <button type="button" (click)="goToMemoryList()">Manage all memories</button>
      </div>
    </section>

    <ng-template #memoryList let-items>
      @if (items.length === 0) {
        <p class="state">No memories yet.</p>
      } @else {
        <ul class="list">
          @for (memory of items; track memory.id) {
            <li class="memory-item">
              <p class="memory-item__content">{{ memory.content }}</p>
              <div class="memory-meta">
                <time [attr.datetime]="memory.updated_at">{{ memory.updated_at | date: 'MMM d h:mm a' }}</time>
                <span class="memory-meta__actions">
                  <button type="button" (click)="openEditModal(memory)">edit</button>
                  <button type="button" class="memory-meta__delete" (click)="deleteMemory(memory)">delete</button>
                </span>
              </div>
            </li>
          }
        </ul>
      }
    </ng-template>

    <ui-modal
      [open]="editorOpen()"
      [labelledBy]="'memory-editor-title'"
      (dismiss)="closeEditor()"
    >
      <h4 modal-header id="memory-editor-title">{{ editingId() ? 'Edit memory' : 'Create memory' }}</h4>
      <label class="label" for="memory-content">Memory content</label>
      <textarea id="memory-content" [value]="draft()" (input)="draft.set($any($event.target).value)"></textarea>
      <div modal-footer>
        <ui-button size="sm" variant="secondary" (activate)="closeEditor()">Cancel</ui-button>
        <ui-button size="sm" variant="primary" (activate)="saveEditor()">Save</ui-button>
      </div>
    </ui-modal>
  `,
  styles: [`
    .tab-body {
      display: flex;
      flex: 1;
      flex-direction: column;
      gap: 0.625rem;
      min-height: 0;
    }

    :host {
      display: flex;
      flex: 1;
      min-height: 0;
    }

    .context-tabs {
      border-radius: 0.375rem;
      display: flex;
      overflow: hidden;
    }

    .context-tabs button {
      background: transparent;
      border: 0;
      color: var(--color-text-muted);
      cursor: pointer;
      flex: 1;
      font-size: 0.625rem;
      font-weight: 700;
      padding: 0.3125rem 0;
    }

    .context-tabs .context-tabs__button--active {
      background: color-mix(in srgb, var(--color-accent) 14%, transparent);
      color: var(--color-accent);
    }

    .memory-content {
      flex: 1;
      min-height: 0;
      overflow-y: auto;
    }

    .list {
      display: grid;
      gap: 0.375rem;
      list-style: none;
      margin: 0;
      padding: 0;
    }

    .memory-item {
      background: var(--bg-elevated, var(--color-surface-elevated));
      border: 1px solid var(--border, var(--color-border-base));
      border-radius: 0.375rem;
      color: var(--color-text-primary);
      display: grid;
      gap: 0.375rem;
      padding: 0.5rem 0.625rem;
    }

    .memory-item__content {
      font-size: 0.75rem;
      line-height: 1.35;
      margin: 0;
    }

    .memory-meta {
      align-items: center;
      color: var(--text-muted, var(--color-text-muted));
      display: flex;
      flex-wrap: wrap;
      font-size: 0.625rem;
      gap: 0.4rem;
      justify-content: space-between;
      line-height: 1;
    }

    .memory-meta time {
      text-transform: uppercase;
    }

    .memory-meta__actions {
      display: inline-flex;
      gap: 0.4rem;
      margin-left: auto;
    }

    .memory-meta button {
      background: transparent;
      border: 0;
      color: inherit;
      cursor: pointer;
      font-size: inherit;
      padding: 0;
    }

    .memory-meta button:hover {
      color: var(--accent, var(--color-accent));
    }

    .memory-meta .memory-meta__delete {
      color: var(--danger, var(--color-danger));
    }

    .memory-actions {
      border-top: 1px solid var(--color-border-base);
      display: grid;
      flex-shrink: 0;
      gap: 0.5rem;
      margin-top: 0.375rem;
      padding-top: 0.625rem;
    }

    .memory-actions button {
      background: var(--color-surface-base);
      border: 1px solid var(--color-border-base);
      border-radius: 0.45rem;
      color: var(--color-text-primary);
      cursor: pointer;
      font-size: 0.72rem;
      font-weight: 600;
      line-height: 1;
      padding: 0.45rem 0.55rem;
      width: 100%;
      white-space: nowrap;
    }

    .memory-actions .add-memory {
      background: color-mix(in srgb, var(--color-accent) 14%, var(--color-surface-base));
      border-color: color-mix(in srgb, var(--color-accent) 36%, var(--color-border-base));
      color: var(--color-accent);
    }

    .state,
    .error {
      margin: 0;
      font-size: 0.85rem;
    }

    .error {
      color: var(--color-danger);
    }

    .label {
      display: block;
      font-size: 0.8rem;
      margin-bottom: 0.25rem;
    }

    textarea {
      width: 100%;
      min-height: 8rem;
      border: 1px solid var(--color-border-base);
      border-radius: 0.625rem;
      padding: 0.6rem;
      color: var(--color-text-primary);
      background: var(--color-surface-base);
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ContextMemoriesTabComponent implements OnChanges {
  readonly chatId = input<string | null>(null);
  readonly personalityId = input<string | null>(null);
  readonly memoryService = inject(MemoryService);
  private readonly router = inject(Router);

  readonly loading = signal(false);
  readonly error = signal<string | null>(null);
  readonly activeMemoryTab = signal<MemoryContextTab>('thread');
  readonly threadMemories = signal<Memory[]>([]);
  readonly globalMemories = signal<Memory[]>([]);
  readonly personalityMemories = signal<Memory[]>([]);
  readonly visibleMemories = computed(() =>
    this.activeMemoryTab() === 'thread'
      ? this.threadMemories()
      : [...this.globalMemories(), ...this.personalityMemories()],
  );

  readonly editorOpen = signal(false);
  readonly draft = signal('');
  readonly editingId = signal<string | null>(null);

  ngOnChanges(changes: SimpleChanges): void {
    if (changes['chatId'] || changes['personalityId']) {
      void this.refresh();
    }
  }

  async refresh(): Promise<void> {
    const chatId = this.chatId();
    this.error.set(null);
    this.loading.set(true);
    try {
      const [threadPage, globalPage, personalityPage] = await Promise.all([
        chatId
          ? firstValueFrom(this.memoryService.getMemories(1, 20, { chat_id: chatId, level: 'thread' }))
          : Promise.resolve({ results: [] }),
        firstValueFrom(this.memoryService.getMemories(1, 20, { level: 'global' })),
        this.personalityId()
          ? firstValueFrom(this.memoryService.getMemories(1, 20, { pinned_personality_id: this.personalityId()! }))
          : Promise.resolve({ results: [] }),
      ]);
      this.threadMemories.set(threadPage.results ?? []);
      this.globalMemories.set(globalPage.results ?? []);
      this.personalityMemories.set(personalityPage.results ?? []);
    } catch (error) {
      this.error.set(apiErrorMessage(error, 'Failed to load memories'));
    } finally {
      this.loading.set(false);
    }
  }

  openCreateModal(): void {
    this.editingId.set(null);
    this.draft.set('');
    this.editorOpen.set(true);
  }

  openEditModal(memory: Memory): void {
    this.editingId.set(memory.id);
    this.draft.set(memory.content);
    this.editorOpen.set(true);
  }

  closeEditor(): void {
    this.editorOpen.set(false);
  }

  async saveEditor(): Promise<void> {
    const chatId = this.chatId();
    const content = this.draft().trim();
    if (!chatId || !content) return;

    try {
      if (this.editingId()) {
        await firstValueFrom(this.memoryService.patchMemory(this.editingId()!, { content }));
      } else {
        await firstValueFrom(this.memoryService.createMemory({
          chat_id: chatId,
          content,
          level: 'thread',
          type: 'Context',
        }));
      }
      this.closeEditor();
      await this.refresh();
    } catch (error) {
      this.error.set(apiErrorMessage(error, 'Failed to save memory'));
    }
  }

  async deleteMemory(memory: Memory): Promise<void> {
    if (!confirm('Delete this memory?')) return;
    try {
      await firstValueFrom(this.memoryService.deleteMemory(memory.id));
      await this.refresh();
    } catch (error) {
      this.error.set(apiErrorMessage(error, 'Failed to delete memory'));
    }
  }

  goToMemoryList(): void {
    const queryParams: Record<string, string> = {};
    if (this.chatId()) {
      queryParams['chat_id'] = this.chatId()!;
      queryParams['level'] = 'thread';
    }
    if (this.personalityId()) {
      queryParams['personality_id'] = this.personalityId()!;
    }
    void this.router.navigate(['/memories'], {
      queryParams: Object.keys(queryParams).length > 0 ? queryParams : undefined,
    });
  }
}
