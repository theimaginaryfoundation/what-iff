import { Injectable, OnDestroy, Signal, computed, inject, signal } from '@angular/core';
import { Subscription, forkJoin, of } from 'rxjs';
import { catchError } from 'rxjs/operators';

import { Personality, PersonalityExpression } from '../models/personality.model';
import { apiErrorMessage } from '../utils/api-error.helpers';
import { PersonalityService } from './personality.service';

/**
 * Component-scoped service that loads a personality alongside its expression
 * catalog and exposes them as signals for the personality detail page.
 *
 * Mirrors the stale-request guard pattern from `ChatSessionService.setActive()`
 * so out-of-order responses from the network never override a newer selection.
 *
 * Provided in the route component (not root) so each navigation gets a fresh
 * instance — preventing leaks between personalities.
 */
@Injectable()
export class PersonalityViewService implements OnDestroy {
  private readonly personalityService = inject(PersonalityService);

  private readonly _personality = signal<Personality | null>(null);
  private readonly _expressions = signal<readonly PersonalityExpression[]>([]);
  private readonly _loading = signal(false);
  private readonly _error = signal<string | null>(null);

  private activePersonalityId: string | null = null;
  private readonly subscriptions = new Subscription();

  /**
   * Incremented on every {@link setExpressions} (optimistic edits). Each
   * `requestActive` snapshot compares against this so a slow initial forkJoin
   * cannot overwrite expressions after the user has already removed a row
   * (DELETE + optimistic list) — which previously looked like "delete only
   * cleared the image" when the stale list still contained the slot.
   */
  private expressionsMutationEpoch = 0;

  readonly personality: Signal<Personality | null> = this._personality.asReadonly();
  readonly expressions: Signal<readonly PersonalityExpression[]> = this._expressions.asReadonly();
  readonly loading: Signal<boolean> = this._loading.asReadonly();
  readonly error: Signal<string | null> = this._error.asReadonly();
  readonly hasError = computed(() => this._error() !== null);

  /**
   * Loads (or reloads) a personality and its expressions. Subsequent calls
   * with a different ID cancel any in-flight responses.
   */
  setActive(personalityId: string): void {
    if (!personalityId) {
      this.clearActive();
      return;
    }
    if (this.activePersonalityId === personalityId && (this._personality() || this._loading())) {
      return;
    }
    this.activePersonalityId = personalityId;
    this._loading.set(true);
    this._error.set(null);
    this._personality.set(null);
    this._expressions.set([]);
    this.requestActive(personalityId);
  }

  /** Reloads the active personality without changing its ID. */
  refresh(): void {
    if (!this.activePersonalityId) return;
    this._loading.set(true);
    this._error.set(null);
    this.requestActive(this.activePersonalityId);
  }

  /** Updates the cached personality directly without re-fetching. */
  setPersonality(personality: Personality | null): void {
    if (personality && this.activePersonalityId !== personality.id) return;
    this._personality.set(personality);
  }

  /** Updates the cached expressions directly without re-fetching. */
  setExpressions(expressions: readonly PersonalityExpression[]): void {
    this.expressionsMutationEpoch++;
    this._expressions.set(expressions);
  }

  /** Clears all cached state. */
  clearActive(): void {
    this.activePersonalityId = null;
    this.expressionsMutationEpoch = 0;
    this._personality.set(null);
    this._expressions.set([]);
    this._loading.set(false);
    this._error.set(null);
  }

  ngOnDestroy(): void {
    this.subscriptions.unsubscribe();
  }

  private requestActive(personalityId: string): void {
    const expressionsEpochAtStart = this.expressionsMutationEpoch;
    const personality$ = this.personalityService.getPersonality(personalityId);
    const expressions$ = this.personalityService
      .listExpressions(personalityId)
      .pipe(catchError(() => of([] as PersonalityExpression[])));

    this.subscriptions.add(
      forkJoin({ personality: personality$, expressions: expressions$ }).subscribe({
        next: ({ personality, expressions }) => {
          if (this.activePersonalityId !== personalityId) return;
          this._personality.set(personality);
          if (this.expressionsMutationEpoch === expressionsEpochAtStart) {
            this._expressions.set(expressions);
          }
          this._loading.set(false);
        },
        error: error => {
          if (this.activePersonalityId !== personalityId) return;
          this._loading.set(false);
          this._error.set(apiErrorMessage(error, 'Failed to load personality'));
        },
      }),
    );
  }
}
