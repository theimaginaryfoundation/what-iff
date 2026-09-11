import { DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { associationLabel, isUserScopedMemoryLevel, MemoryCardVm } from '../helpers/memory-vm.helpers';
import { MemoryPersonalityOption } from './memory-form.component';
import { GlobeIconComponent, StarIconComponent } from '../../../shared/ui/icons/icons';
import { PersonaAccentScopeComponent } from '../../personality/picker/persona-accent-scope.component';
import { PersonaCoverComponent } from '../../personality/picker/persona-cover.component';

@Component({
  selector: 'app-memory-card',
  standalone: true,
  imports: [
    DatePipe,
    FormsModule,
    GlobeIconComponent,
    StarIconComponent,
    PersonaAccentScopeComponent,
    PersonaCoverComponent,
  ],
  templateUrl: './memory-card.component.html',
  styleUrl: './memory-card.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MemoryCardComponent {
  readonly memory = input.required<MemoryCardVm>();
  readonly personalities = input<MemoryPersonalityOption[]>([]);
  readonly pinUpdating = input(false);
  readonly selected = input(false);
  readonly focused = input(false);
  readonly isArchivedView = input(false);
  readonly readOnly = input(false);

  readonly toggleSelect = output<string>();
  readonly focus = output<string>();
  readonly save = output<{ id: string; content: string }>();
  readonly pinChange = output<{ id: string; pinnedPersonalityId: string | null }>();
  readonly starChange = output<{ id: string; starred: boolean }>();
  readonly move = output<string>();
  readonly archive = output<string>();
  readonly delete = output<string>();

  readonly editing = signal(false);
  readonly draft = signal('');
  readonly pinDraft = signal<string | null>(null);
  readonly menuOpen = signal(false);
  readonly canSave = computed(() => this.draft().trim().length > 0);
  readonly showPinControl = computed(() => isUserScopedMemoryLevel(this.memory().level));
  readonly association = computed(() => associationLabel(this.memory()));
  readonly pinnedPersonality = computed(() => {
    const id = this.memory().pinnedPersonalityId;
    if (!id) return null;
    return this.personalities().find(p => p.id === id) ?? null;
  });

  startEdit(event?: Event): void {
    if (this.readOnly()) return;
    event?.stopPropagation();
    this.menuOpen.set(false);
    this.draft.set(this.memory().content);
    this.pinDraft.set(this.memory().pinnedPersonalityId);
    this.editing.set(true);
  }

  cancelEdit(event?: Event): void {
    event?.stopPropagation();
    this.draft.set(this.memory().content);
    this.pinDraft.set(this.memory().pinnedPersonalityId);
    this.editing.set(false);
  }

  onPinChange(personalityId: string | null, event?: Event): void {
    event?.stopPropagation();
    this.pinDraft.set(personalityId);
    this.pinChange.emit({ id: this.memory().id, pinnedPersonalityId: personalityId });
  }

  submitEdit(event?: Event): void {
    event?.stopPropagation();
    const content = this.draft().trim();
    if (!content) {
      return;
    }
    this.save.emit({ id: this.memory().id, content });
    this.editing.set(false);
  }

  toggleMenu(event?: Event): void {
    event?.stopPropagation();
    this.menuOpen.update(open => !open);
  }

  closeMenu(): void {
    this.menuOpen.set(false);
  }

  onToggleSelect(event: Event): void {
    event.stopPropagation();
    this.toggleSelect.emit(this.memory().id);
  }

  onCardActivate(event?: Event): void {
    if (this.editing()) return;
    const target = event?.target as HTMLElement | undefined;
    if (target?.closest('button, a, label, input, textarea, select')) return;
    this.focus.emit(this.memory().id);
  }

  onStar(event?: Event): void {
    event?.stopPropagation();
    this.menuOpen.set(false);
    this.starChange.emit({ id: this.memory().id, starred: !this.memory().starred });
  }

  onMove(event?: Event): void {
    event?.stopPropagation();
    this.menuOpen.set(false);
    this.move.emit(this.memory().id);
  }

  onArchive(event?: Event): void {
    event?.stopPropagation();
    this.menuOpen.set(false);
    this.archive.emit(this.memory().id);
  }

  onDelete(event?: Event): void {
    event?.stopPropagation();
    this.menuOpen.set(false);
    this.delete.emit(this.memory().id);
  }
}
