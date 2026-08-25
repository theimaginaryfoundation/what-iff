import { Injectable, signal } from '@angular/core';

export type ModeAssociationFilterMode = 'all' | 'personality';

@Injectable({ providedIn: 'root' })
export class ModeViewService {
  readonly associationFilterMode = signal<ModeAssociationFilterMode>('all');
  readonly selectedPersonalityIds = signal<string[]>([]);
  readonly createRequestTick = signal(0);

  setSelectedPersonalityIds(ids: readonly string[]): void {
    const next = [...ids];
    this.selectedPersonalityIds.set(next);
    this.associationFilterMode.set(next.length > 0 ? 'personality' : 'all');
  }

  selectAllAssociations(): void {
    this.selectedPersonalityIds.set([]);
    this.associationFilterMode.set('all');
  }

  requestCreateModalOpen(): void {
    this.createRequestTick.update(value => value + 1);
  }
}
