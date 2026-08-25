import { Injectable, inject } from '@angular/core';
import { Observable, throwError } from 'rxjs';
import { catchError, tap } from 'rxjs/operators';

import { PersonalityExpression, UpdatePersonalityExpressionRequest } from '../models/personality.model';
import { PersonalityService } from './personality.service';

/**
 * Snapshot of an expression slot used to roll back an optimistic update on failure.
 */
export interface ExpressionSlotSnapshot {
  expressionKey: string;
  imageId: string | null;
  imageUrl: string | null;
  label: string | null;
  exists: boolean;
}

/**
 * Hook callbacks the calling component supplies so this service can update
 * the local `expressions` list optimistically and roll back if the network
 * call fails. Both functions must be cheap, synchronous mutations.
 */
export interface OptimisticContext {
  /** Snapshot the current state of an expression slot for potential rollback. */
  snapshot(expressionKey: string): ExpressionSlotSnapshot;
  /** Apply an optimistic mutation that the user will see immediately. */
  apply(snapshot: ExpressionSlotSnapshot, change: UpdatePersonalityExpressionRequest, optimisticImageUrl: string | null): void;
  /** Replace the slot with the authoritative server response. */
  commit(server: PersonalityExpression): void;
  /** Revert the slot back to a previous snapshot when the request fails. */
  rollback(snapshot: ExpressionSlotSnapshot): void;
  /**
   * Optimistically remove the entire slot locally before DELETE /expressions/{key}.
   * Must not reuse {@link apply} with `{ image_id: null }` — that only clears the image (PUT).
   */
  applyDelete?(snapshot: ExpressionSlotSnapshot): void;
}

@Injectable({ providedIn: 'root' })
export class ExpressionAssignmentService {
  private readonly personalityService = inject(PersonalityService);

  /**
   * Assigns (or replaces) the image for an expression slot. Performs an
   * optimistic UI update and rolls back if the server rejects the change.
   *
   * @param personalityId The owning personality.
   * @param expressionKey The slot key.
   * @param imageId The gallery image ID to assign.
   * @param imageUrl Optional URL to display while the request is in flight (eg. the gallery thumb).
   * @param ctx     Optional optimistic-update hooks; when omitted, the service simply forwards the request.
   */
  assignFromGallery(
    personalityId: string,
    expressionKey: string,
    imageId: string,
    imageUrl: string | null,
    ctx?: OptimisticContext,
  ): Observable<PersonalityExpression> {
    return this.upsertWithOptimisticUpdate(
      personalityId,
      expressionKey,
      { image_id: imageId },
      imageUrl,
      ctx,
    );
  }

  /** Update only the label for an expression slot, with optimistic UI. */
  setLabel(
    personalityId: string,
    expressionKey: string,
    label: string,
    ctx?: OptimisticContext,
  ): Observable<PersonalityExpression> {
    return this.upsertWithOptimisticUpdate(
      personalityId,
      expressionKey,
      { label },
      undefined,
      ctx,
    );
  }

  /** Clears the image (and optionally the label) for an expression slot. */
  clear(
    personalityId: string,
    expressionKey: string,
    ctx?: OptimisticContext,
  ): Observable<PersonalityExpression> {
    return this.upsertWithOptimisticUpdate(
      personalityId,
      expressionKey,
      { image_id: null },
      null,
      ctx,
    );
  }

  /** Deletes the entire expression row from the server. */
  remove(
    personalityId: string,
    expressionKey: string,
    ctx?: OptimisticContext,
  ): Observable<void> {
    const snapshot = ctx?.snapshot(expressionKey);
    if (snapshot && ctx?.applyDelete) {
      ctx.applyDelete(snapshot);
    }
    return this.personalityService.deleteExpression(personalityId, expressionKey).pipe(
      catchError(error => {
        if (snapshot && ctx) {
          ctx.rollback(snapshot);
        }
        return throwError(() => error);
      }),
    );
  }

  private upsertWithOptimisticUpdate(
    personalityId: string,
    expressionKey: string,
    request: UpdatePersonalityExpressionRequest,
    optimisticImageUrl: string | null | undefined,
    ctx?: OptimisticContext,
  ): Observable<PersonalityExpression> {
    let snapshot: ExpressionSlotSnapshot | undefined;
    if (ctx) {
      snapshot = ctx.snapshot(expressionKey);
      ctx.apply(snapshot, request, optimisticImageUrl ?? snapshot.imageUrl);
    }
    return this.personalityService.upsertExpression(personalityId, expressionKey, request).pipe(
      tap(server => {
        if (ctx) ctx.commit(server);
      }),
      catchError(error => {
        if (snapshot && ctx) {
          ctx.rollback(snapshot);
        }
        return throwError(() => error);
      }),
    );
  }
}
