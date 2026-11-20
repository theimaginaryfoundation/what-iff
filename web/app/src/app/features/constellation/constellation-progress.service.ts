import { DOCUMENT } from '@angular/common';
import { Injectable, computed, effect, inject, signal } from '@angular/core';
import { AuthService } from '../../core/services/auth.service';
import { LEARNING_OBJECTIVES } from './constellation.data';
import type { LearningProgress } from './constellation.model';
import { completedBranchCount, completedObjectives, learningRank, objectiveReady, parseLearningProgress } from './constellation-progress';

/** Browser-only learning ledger, isolated by signed-in user. Never an entitlement or verification service. */
@Injectable({ providedIn: 'root' })
export class ConstellationProgressService {
  private readonly auth = inject(AuthService);
  private readonly document = inject(DOCUMENT);
  private readonly owner = signal<string | null>(null);
  readonly progress = signal<LearningProgress>({});
  readonly persisted = signal(true);
  readonly completed = computed(() => (this.owner() === this.userId() ? completedObjectives(this.progress()) : new Set<string>()));
  readonly rank = computed(() => learningRank(this.completed()));
  readonly branches = computed(() => completedBranchCount(this.completed()));
  readonly xp = computed(() => LEARNING_OBJECTIVES.reduce((total, node) => total + (this.completed().has(node.id) ? node.xp : 0), 0));

  constructor() {
    effect(() => {
      const id = this.userId();
      this.owner.set(id);
      this.persisted.set(true);
      try {
        this.progress.set(id ? parseLearningProgress(this.document.defaultView?.localStorage.getItem(this.key(id)) ?? null) : {});
      } catch {
        this.progress.set({});
        this.persisted.set(false);
      }
    });
  }

  complete(id: string, note: string): boolean {
    const node = LEARNING_OBJECTIVES.find(node => node.id === id);
    const userId = this.userId();
    if (
      !userId ||
      userId !== this.owner() ||
      !node ||
      this.completed().has(id) ||
      !objectiveReady(node, this.completed()) ||
      note.trim().length < 12 ||
      note.length > 2000
    )
      return false;
    const progress = { ...this.progress(), [id]: { note: note.trim(), completedAt: new Date().toISOString() } };
    this.progress.set(progress);
    try {
      this.document.defaultView?.localStorage.setItem(this.key(userId), JSON.stringify(progress));
    } catch {
      this.persisted.set(false);
    }
    return true;
  }

  private userId(): string | null {
    return this.auth.currentUser()?.id ?? null;
  }
  private key(id: string): string {
    return `whatiff.constellation.v1:${id}`;
  }
}
