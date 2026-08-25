import type { MockedObject } from "vitest";
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { of, throwError } from 'rxjs';

import { PersonalityExpression } from '../models/personality.model';
import { ExpressionAssignmentService, OptimisticContext } from './expression-assignment.service';
import { PersonalityService } from './personality.service';

function makeServerExpression(overrides: Partial<PersonalityExpression> = {}): PersonalityExpression {
    return {
        expression_key: 'happy',
        label: null,
        image_id: 'img-from-server',
        image_url: '/api/image-gallery/img-from-server?size=full',
        created_at: '2026-04-28T00:00:00Z',
        updated_at: '2026-04-28T00:00:00Z',
        ...overrides,
    };
}

function makeContext(initial: {
    imageId: string | null;
    imageUrl: string | null;
    label: string | null;
    exists: boolean;
}): {
    ctx: OptimisticContext;
    state: {
        imageId: string | null;
        imageUrl: string | null;
        label: string | null;
        exists: boolean;
    };
    history: string[];
} {
    const state = { ...initial };
    const history: string[] = [];
    const ctx: OptimisticContext = {
        snapshot: () => ({ expressionKey: 'happy', ...state }),
        apply: (_snap, change, optimisticImageUrl) => {
            history.push('apply');
            if ('image_id' in change) {
                state.imageId = change.image_id ?? null;
                state.imageUrl = change.image_id == null ? null : (optimisticImageUrl ?? state.imageUrl);
            }
            if ('label' in change) {
                state.label = change.label ?? null;
            }
        },
        applyDelete: () => {
            history.push('applyDelete');
            state.exists = false;
            state.imageId = null;
            state.imageUrl = null;
        },
        commit: server => {
            history.push('commit');
            state.imageId = server.image_id;
            state.imageUrl = server.image_url;
            state.label = server.label;
            state.exists = true;
        },
        rollback: snap => {
            history.push('rollback');
            state.imageId = snap.imageId;
            state.imageUrl = snap.imageUrl;
            state.label = snap.label;
            state.exists = snap.exists;
        },
    };
    return { ctx, state, history };
}

describe('ExpressionAssignmentService', () => {
    let service: ExpressionAssignmentService;
    let personalityService: Pick<MockedObject<PersonalityService>, 'upsertExpression' | 'deleteExpression'>;

    beforeEach(() => {
        personalityService = {
            upsertExpression: vi.fn().mockName("PersonalityService.upsertExpression"),
            deleteExpression: vi.fn().mockName("PersonalityService.deleteExpression")
        } as unknown as Pick<MockedObject<PersonalityService>, 'upsertExpression' | 'deleteExpression'>;
        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                ExpressionAssignmentService,
                { provide: PersonalityService, useValue: personalityService },
            ],
        });
        service = TestBed.inject(ExpressionAssignmentService);
    });

    it('forwards calls without an optimistic context', async () => {
        personalityService.upsertExpression.mockReturnValue(of(makeServerExpression()));
        service.assignFromGallery('p-1', 'happy', 'img-1', '/optimistic.png').subscribe(server => {
            expect(server.image_id).toBe('img-from-server');
            expect(personalityService.upsertExpression).toHaveBeenCalledWith('p-1', 'happy', { image_id: 'img-1' });
            ;
        });
    });

    it('applies an optimistic update before the network commit', async () => {
        const { ctx, state, history } = makeContext({ imageId: null, imageUrl: null, label: null, exists: false });
        personalityService.upsertExpression.mockReturnValue(of(makeServerExpression({ image_id: 'img-1', image_url: '/server.png' })));

        service.assignFromGallery('p-1', 'happy', 'img-1', '/optimistic.png', ctx).subscribe(() => {
            expect(history).toEqual(['apply', 'commit']);
            expect(state.imageId).toBe('img-1');
            expect(state.imageUrl).toBe('/server.png');
            ;
        });
    });

    it('rolls back the optimistic update on failure', async () => {
        const { ctx, state, history } = makeContext({ imageId: 'old-img', imageUrl: '/old.png', label: 'Old', exists: true });
        personalityService.upsertExpression.mockReturnValue(throwError(() => new Error('500')));

        service.assignFromGallery('p-1', 'happy', 'img-new', '/new.png', ctx).subscribe({
            next: () => expect.fail('should not emit'),
            error: () => {
                expect(history).toEqual(['apply', 'rollback']);
                expect(state.imageId).toBe('old-img');
                expect(state.imageUrl).toBe('/old.png');
                expect(state.label).toBe('Old');
                ;
            },
        });
    });

    it('clear sets the slot to null with optimistic update', async () => {
        const { ctx, state } = makeContext({ imageId: 'img-1', imageUrl: '/x.png', label: 'Hi', exists: true });
        personalityService.upsertExpression.mockReturnValue(of(makeServerExpression({ image_id: null, image_url: null, label: 'Hi' })));

        service.clear('p-1', 'happy', ctx).subscribe(() => {
            expect(state.imageId).toBeNull();
            expect(state.imageUrl).toBeNull();
            ;
        });
    });

    it('remove rolls back when delete fails', async () => {
        const { ctx, state, history } = makeContext({ imageId: 'img-1', imageUrl: '/x.png', label: 'Hi', exists: true });
        personalityService.deleteExpression.mockReturnValue(throwError(() => new Error('500')));

        service.remove('p-1', 'happy', ctx).subscribe({
            next: () => expect.fail('should not emit'),
            error: () => {
                expect(history).toEqual(['applyDelete', 'rollback']);
                expect(state.imageId).toBe('img-1');
                expect(state.label).toBe('Hi');
                ;
            },
        });
    });

    it('remove calls applyDelete then deleteExpression on success', async () => {
        const { ctx, history } = makeContext({ imageId: 'img-1', imageUrl: '/x.png', label: 'Hi', exists: true });
        personalityService.deleteExpression.mockReturnValue(of(void 0));

        service.remove('p-1', 'happy', ctx).subscribe(() => {
            expect(history).toEqual(['applyDelete']);
            expect(personalityService.deleteExpression).toHaveBeenCalledWith('p-1', 'happy');
            ;
        });
    });
});
