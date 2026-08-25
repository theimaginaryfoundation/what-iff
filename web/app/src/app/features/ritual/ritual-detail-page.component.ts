import { CommonModule, DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, OnInit, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { forkJoin, of } from 'rxjs';
import { catchError, map } from 'rxjs/operators';

import { Ritual } from '../../core/models/ritual.model';
import { ChatService } from '../../core/services/chat.service';
import { PersonalityService } from '../../core/services/personality.service';
import { RitualService } from '../../core/services/ritual.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { RitualFormComponent, RitualFormSave } from './components/ritual-form.component';
import { RitualSelectOption } from './components/ritual-filter-bar.component';
import { RitualAvailablePreviewComponent } from './components/ritual-available-preview.component';

@Component({
  selector: 'app-ritual-detail-page',
  standalone: true,
  imports: [CommonModule, DatePipe, FormsModule, RitualFormComponent, RitualAvailablePreviewComponent],
  templateUrl: './ritual-detail-page.component.html',
  styleUrl: './ritual-detail-page.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RitualDetailPageComponent implements OnInit {
  private readonly ritualService = inject(RitualService);
  private readonly personalityService = inject(PersonalityService);
  private readonly chatService = inject(ChatService);
  private readonly confirmation = inject(ConfirmationService);
  private readonly route = inject(ActivatedRoute);
  private readonly router = inject(Router);

  readonly ritual = signal<Ritual | null>(null);
  readonly loading = signal(true);
  readonly saving = signal(false);
  readonly deleting = signal(false);
  readonly error = signal<string | null>(null);
  readonly isSystem = signal(false);
  readonly personalities = signal<RitualSelectOption[]>([]);
  readonly chats = signal<RitualSelectOption[]>([]);
  readonly selectedChatId = signal('');

  readonly ritualId = computed(() => this.route.snapshot.paramMap.get('id') ?? '');
  readonly creating = computed(() => this.ritualId() === 'new');

  ngOnInit(): void {
    this.loadSelectOptions();
    if (this.creating()) {
      const copyFrom = this.route.snapshot.queryParamMap.get('copyFrom');
      if (copyFrom) {
        this.loadRitual(copyFrom, true);
      } else {
        this.loading.set(false);
      }
      return;
    }
    this.loadRitual(this.ritualId(), false);
  }

  private loadSelectOptions(): void {
    forkJoin({
      personalities: this.personalityService.listPersonalities(1, 200).pipe(catchError(() => of({ results: [] } as any))),
      chats: this.chatService.listAllChats(200).pipe(
        catchError(() => of({ chats: [], truncated: false })),
        map(result => ({ results: result.chats })),
      ),
    }).subscribe(result => {
      this.personalities.set(
        (result.personalities.results ?? []).map((personality: any) => ({ id: personality.id, label: personality.name })),
      );
      this.chats.set((result.chats.results ?? []).map((chat: any) => ({ id: chat.id, label: chat.name || 'Untitled chat' })));
      if (!this.selectedChatId() && this.chats().length > 0) {
        this.selectedChatId.set(this.chats()[0].id);
      }
    });
  }

  private loadRitual(id: string, fromCopy: boolean): void {
    this.loading.set(true);
    this.error.set(null);
    forkJoin({
      ritual: this.ritualService.getRitual(id).pipe(catchError(() => of(null))),
      system: this.ritualService.listSystemRituals().pipe(catchError(() => of([]))),
    }).subscribe({
      next: result => {
        const fallback = (result.system ?? []).find(item => item.id === id) ?? null;
        const found = result.ritual ?? fallback;
        if (!found) {
          this.error.set('Skill not found.');
          this.loading.set(false);
          return;
        }
        this.isSystem.set((result.system ?? []).some(item => item.id === found.id) && !fromCopy);
        this.ritual.set(fromCopy ? { ...found, id: '' } : found);
        this.loading.set(false);
      },
      error: error => {
        this.loading.set(false);
        this.error.set(error instanceof Error ? error.message : 'Failed to load skill');
      },
    });
  }

  goBack(): void {
    void this.router.navigate(['/skills'], { queryParamsHandling: 'merge' });
  }

  save(payload: RitualFormSave): void {
    this.saving.set(true);
    this.error.set(null);
    if (this.creating()) {
      this.ritualService.createRitual(payload).subscribe({
        next: ritual => {
          this.saving.set(false);
          void this.router.navigate(['/skills', ritual.id]);
        },
        error: error => {
          this.saving.set(false);
          this.error.set(error instanceof Error ? error.message : 'Failed to create skill');
        },
      });
      return;
    }

    const ritual = this.ritual();
    if (!ritual) {
      this.saving.set(false);
      return;
    }
    this.ritualService.updateRitual(ritual.id, payload).subscribe({
      next: updated => {
        this.ritual.set(updated);
        this.saving.set(false);
      },
      error: error => {
        this.saving.set(false);
        this.error.set(error instanceof Error ? error.message : 'Failed to save skill');
      },
    });
  }

  async remove(): Promise<void> {
    const ritual = this.ritual();
    if (!ritual) return;
    const confirmed = await this.confirmation.confirm({
      title: 'Delete skill',
      message: `Delete "${ritual.name}"?`,
      type: 'danger',
      confirmText: 'Delete',
      cancelText: 'Cancel',
    });
    if (!confirmed) return;

    this.deleting.set(true);
    this.ritualService.deleteRitual(ritual.id).subscribe({
      next: () => {
        this.deleting.set(false);
        void this.router.navigate(['/skills']);
      },
      error: error => {
        this.deleting.set(false);
        this.error.set(error instanceof Error ? error.message : 'Failed to delete skill');
      },
    });
  }

  assignSystemHotkey(hotkeys: string): void {
    const ritual = this.ritual();
    if (!ritual) return;
    this.saving.set(true);
    this.ritualService.assignSystemRitualHotkey(ritual.id, hotkeys).subscribe({
      next: () => {
        this.saving.set(false);
        this.loadRitual(ritual.id, false);
      },
      error: error => {
        this.saving.set(false);
        this.error.set(error instanceof Error ? error.message : 'Failed to assign hotkey');
      },
    });
  }

  copyToPersonal(): void {
    const ritual = this.ritual();
    if (!ritual) return;
    this.saving.set(true);
    this.ritualService
      .createRitual({
        name: ritual.name,
        description: ritual.description,
        content: ritual.content,
        hotkeys: ritual.hotkeys || undefined,
        personality_id: ritual.personality_id,
      })
      .subscribe({
        next: created => {
          this.saving.set(false);
          void this.router.navigate(['/skills', created.id]);
        },
        error: error => {
          this.saving.set(false);
          this.error.set(error instanceof Error ? error.message : 'Failed to copy skill');
        },
      });
  }
}
