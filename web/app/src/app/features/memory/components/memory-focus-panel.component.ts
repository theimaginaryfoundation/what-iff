import { DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, input, output } from '@angular/core';
import { RouterLink } from '@angular/router';

import { MemoryMergeEvent } from '../../../core/models/memory.model';
import { associationLabel, MemoryCardVm } from '../helpers/memory-vm.helpers';
import { MemoryPersonalityOption } from './memory-form.component';
import { StarIconComponent } from '../../../shared/ui/icons/icons';
import { TooltipDirective } from '../../../shared/ui/tooltip/tooltip.directive';
import { PersonaAccentScopeComponent } from '../../personality/picker/persona-accent-scope.component';
import { PersonaCoverComponent } from '../../personality/picker/persona-cover.component';

@Component({
  selector: 'app-memory-focus-panel',
  standalone: true,
  imports: [
    DatePipe,
    RouterLink,
    StarIconComponent,
    TooltipDirective,
    PersonaAccentScopeComponent,
    PersonaCoverComponent,
  ],
  templateUrl: './memory-focus-panel.component.html',
  styleUrl: './memory-focus-panel.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MemoryFocusPanelComponent {
  readonly memory = input.required<MemoryCardVm>();
  readonly personalities = input<MemoryPersonalityOption[]>([]);
  readonly mergeEvents = input<MemoryMergeEvent[]>([]);
  readonly mergeEventsLoading = input(false);
  readonly isArchivedView = input(false);
  readonly readOnly = input(false);
  /** When false, hide the panel's own close control (e.g. modal chrome provides one). */
  readonly showCloseButton = input(true);

  readonly close = output<void>();
  readonly edit = output<string>();
  readonly move = output<string>();
  readonly archive = output<string>();
  readonly delete = output<string>();
  readonly starChange = output<{ id: string; starred: boolean }>();

  readonly association = computed(() => associationLabel(this.memory()));
  readonly pinnedPersonality = computed(() => {
    const id = this.memory().pinnedPersonalityId;
    if (!id) return null;
    return this.personalities().find(p => p.id === id) ?? null;
  });
  readonly relatedMergeEvents = computed(() => {
    const id = this.memory().id;
    return this.mergeEvents().filter(event => event.survivor_memory_id === id && !event.reverted_at);
  });
  readonly mergedFromIds = computed(() => this.memory().mergedFromIds);

  mergeTypeLabel(event: MemoryMergeEvent): string {
    switch (event.merge_type) {
      case 'link':
        return 'Linked related memories';
      case 'fold_live':
        return 'Memories merged';
      default:
        return 'Created from batch';
    }
  }

  sourceCount(event: MemoryMergeEvent): number {
    const fromMembers = event.source_members?.length ?? 0;
    if (fromMembers > 0) return fromMembers;
    return Math.max(event.duplicates_folded + 1, 0);
  }
}
