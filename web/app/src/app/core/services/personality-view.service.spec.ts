import type { MockedObject } from "vitest";
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { Subject, of, throwError } from 'rxjs';

import { Personality, PersonalityExpression } from '../models/personality.model';
import { PersonalityService } from './personality.service';
import { PersonalityViewService } from './personality-view.service';

function makePersonality(id: string, overrides: Partial<Personality> = {}): Personality {
    return {
        id,
        name: 'Persona ' + id,
        system_prompt: 'sp',
        auto_pin_memories: false,
        expressions_enabled: true,
        image_style: 'auto', cover_image_id: null,
        cover_image_url: null,
        created_at: '2026-04-28T00:00:00Z',
        updated_at: '2026-04-28T00:00:00Z',
        stats: { chat_count: 0, last_used_at: null },
        ...overrides,
    };
}

function makeExpression(key: string): PersonalityExpression {
    return {
        expression_key: key,
        label: null,
        image_id: null,
        image_url: null,
        created_at: '2026-04-28T00:00:00Z',
        updated_at: '2026-04-28T00:00:00Z',
    };
}

describe('PersonalityViewService', () => {
    let service: PersonalityViewService;
    let personalityService: Pick<MockedObject<PersonalityService>, 'getPersonality' | 'listExpressions'>;

    beforeEach(() => {
        personalityService = {
            getPersonality: vi.fn().mockName("PersonalityService.getPersonality"),
            listExpressions: vi.fn().mockName("PersonalityService.listExpressions")
        } as unknown as Pick<MockedObject<PersonalityService>, 'getPersonality' | 'listExpressions'>;
        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                PersonalityViewService,
                { provide: PersonalityService, useValue: personalityService },
            ],
        });
        service = TestBed.inject(PersonalityViewService);
    });

    it('starts with empty state', () => {
        expect(service.personality()).toBeNull();
        expect(service.expressions()).toEqual([]);
        expect(service.loading()).toBe(false);
        expect(service.error()).toBeNull();
    });

    it('loads personality + expressions on setActive', () => {
        const personality = makePersonality('p-1');
        const expressions = [makeExpression('happy')];
        personalityService.getPersonality.mockReturnValue(of(personality));
        personalityService.listExpressions.mockReturnValue(of(expressions));

        service.setActive('p-1');

        expect(personalityService.getPersonality).toHaveBeenCalledWith('p-1');
        expect(personalityService.listExpressions).toHaveBeenCalledWith('p-1');
        expect(service.personality()).toEqual(personality);
        expect(service.expressions()).toEqual(expressions);
        expect(service.loading()).toBe(false);
    });

    it('exposes errors when the personality fetch fails', () => {
        personalityService.getPersonality.mockReturnValue(throwError(() => new Error('boom')));
        personalityService.listExpressions.mockReturnValue(of([]));

        service.setActive('p-broken');

        expect(service.error()).toBe('boom');
        expect(service.personality()).toBeNull();
        expect(service.loading()).toBe(false);
    });

    it('treats a missing expressions endpoint as an empty list', () => {
        personalityService.getPersonality.mockReturnValue(of(makePersonality('p-1')));
        personalityService.listExpressions.mockReturnValue(throwError(() => new Error('no endpoint')));

        service.setActive('p-1');

        expect(service.expressions()).toEqual([]);
        expect(service.error()).toBeNull();
    });

    it('does not overwrite expressions when setExpressions runs during an in-flight load', () => {
        const persSub = new Subject<Personality>();
        const exprSub = new Subject<PersonalityExpression[]>();
        personalityService.getPersonality.mockReturnValue(persSub.asObservable());
        personalityService.listExpressions.mockReturnValue(exprSub.asObservable());

        service.setActive('p-1');
        persSub.next(makePersonality('p-1'));
        persSub.complete();

        const afterDelete: PersonalityExpression[] = [];
        service.setExpressions(afterDelete);

        exprSub.next([makeExpression('happy')]);
        exprSub.complete();

        expect(service.expressions()).toEqual(afterDelete);
        expect(service.personality()?.id).toBe('p-1');
    });

    it('ignores stale responses from a prior setActive', () => {
        const subjectOne = new Subject<Personality>();
        const subjectTwo = new Subject<Personality>();
        personalityService.getPersonality.mockReturnValueOnce(subjectOne.asObservable()).mockReturnValueOnce(subjectTwo.asObservable());
        personalityService.listExpressions.mockReturnValueOnce(of([])).mockReturnValueOnce(of([]));

        service.setActive('p-1');
        service.setActive('p-2');
        subjectOne.next(makePersonality('p-1'));
        subjectOne.complete();
        subjectTwo.next(makePersonality('p-2'));
        subjectTwo.complete();

        expect(service.personality()?.id).toBe('p-2');
    });

    it('refresh re-runs requests for the active personality', () => {
        personalityService.getPersonality.mockReturnValue(of(makePersonality('p-1')));
        personalityService.listExpressions.mockReturnValue(of([]));
        service.setActive('p-1');
        expect(personalityService.getPersonality).toHaveBeenCalledTimes(1);

        service.refresh();
        expect(personalityService.getPersonality).toHaveBeenCalledTimes(2);
    });

    it('clearActive resets state', () => {
        personalityService.getPersonality.mockReturnValue(of(makePersonality('p-1')));
        personalityService.listExpressions.mockReturnValue(of([]));
        service.setActive('p-1');

        service.clearActive();
        expect(service.personality()).toBeNull();
        expect(service.expressions()).toEqual([]);
        expect(service.loading()).toBe(false);
    });

    it('setPersonality only applies when the active id matches', () => {
        personalityService.getPersonality.mockReturnValue(of(makePersonality('p-1')));
        personalityService.listExpressions.mockReturnValue(of([]));
        service.setActive('p-1');

        const updated = makePersonality('p-1', { name: 'Updated' });
        service.setPersonality(updated);
        expect(service.personality()?.name).toBe('Updated');

        service.setPersonality(makePersonality('p-other'));
        expect(service.personality()?.id).toBe('p-1');
    });
});
