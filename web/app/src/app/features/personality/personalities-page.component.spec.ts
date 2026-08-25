import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { ActivatedRoute, convertToParamMap, Router } from '@angular/router';
import { BehaviorSubject, of } from 'rxjs';

import { PersonalitiesPageComponent } from './personalities-page.component';
import { PersonalityService } from '../../core/services/personality.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { UserPreferencesService } from '../../core/services/user-preferences.service';
import { GeneratePersonalityModalService } from '../../core/services/generate-personality-modal.service';
import { Personality, PaginatedPersonalityResponse } from '../../core/models/personality.model';
import { UserPreferences } from '../../core/models/user.model';
import { TEXT_LIMIT_HARD_MAX, TEXT_LIMIT_WARNING_THRESHOLD, } from '../../core/constants/text-limits.constants';

function makePersonality(overrides: Partial<Personality>): Personality {
    return {
        id: 'p-1',
        name: 'Vera',
        system_prompt: 'sp',
        auto_pin_memories: false,
        expressions_enabled: true,
        image_style: 'auto', cover_image_id: null,
        cover_image_url: null,
        created_at: '2026-04-26T00:00:00Z',
        updated_at: '2026-04-26T00:00:00Z',
        stats: { chat_count: 0, last_used_at: null },
        ...overrides,
    };
}

describe('PersonalitiesPageComponent', () => {
    let fixture: ComponentFixture<PersonalitiesPageComponent>;
    let component: PersonalitiesPageComponent;
    let personalityService: Pick<MockedObject<PersonalityService>, 'listPersonalities' | 'createPersonality' | 'deletePersonality'>;
    let userPrefsService: Pick<MockedObject<UserPreferencesService>, 'getUserPreferences' | 'updateUserPreferences'>;
    let confirmation: Pick<MockedObject<ConfirmationService>, 'confirm' | 'alert'>;
    let router: Pick<MockedObject<Router>, 'navigate' | 'navigateByUrl'>;
    let generateModal: GeneratePersonalityModalService;
    let queryParamMap$: BehaviorSubject<ReturnType<typeof convertToParamMap>>;

    const personalities: Personality[] = [
        makePersonality({ id: 'p-1', name: 'Vera Calder', stats: { chat_count: 5, last_used_at: '2026-04-25T00:00:00Z' } }),
        makePersonality({ id: 'p-2', name: 'Filbolt Pottsworth', stats: { chat_count: 0, last_used_at: null } }),
        makePersonality({ id: 'p-3', name: 'Quinn Hawthorne', stats: { chat_count: 12, last_used_at: '2026-04-27T00:00:00Z' } }),
    ];

    const preferences: UserPreferences = {
        id: 'pref-1',
        user_id: 'u-1',
        default_model_id: 'm-1',
        default_personality_id: 'p-1',
        theme: 'dark',
    } as UserPreferences;

    beforeEach(async () => {
        personalityService = {
            listPersonalities: vi.fn().mockName("PersonalityService.listPersonalities"),
            createPersonality: vi.fn().mockName("PersonalityService.createPersonality"),
            deletePersonality: vi.fn().mockName("PersonalityService.deletePersonality")
        } as unknown as Pick<MockedObject<PersonalityService>, 'listPersonalities' | 'createPersonality' | 'deletePersonality'>;
        userPrefsService = {
            getUserPreferences: vi.fn().mockName("UserPreferencesService.getUserPreferences"),
            updateUserPreferences: vi.fn().mockName("UserPreferencesService.updateUserPreferences")
        } as unknown as Pick<MockedObject<UserPreferencesService>, 'getUserPreferences' | 'updateUserPreferences'>;
        confirmation = {
            confirm: vi.fn().mockName("ConfirmationService.confirm"),
            alert: vi.fn().mockName("ConfirmationService.alert")
        } as unknown as Pick<MockedObject<ConfirmationService>, 'confirm' | 'alert'>;
        router = {
            navigate: vi.fn().mockName("Router.navigate"),
            navigateByUrl: vi.fn().mockName("Router.navigateByUrl")
        } as unknown as Pick<MockedObject<Router>, 'navigate' | 'navigateByUrl'>;
        queryParamMap$ = new BehaviorSubject(convertToParamMap({}));

        const response: PaginatedPersonalityResponse = {
            results: personalities,
            total_count: personalities.length,
            page: 1,
        };
        personalityService.listPersonalities.mockReturnValue(of(response));
        userPrefsService.getUserPreferences.mockReturnValue(of(preferences));

        await TestBed.configureTestingModule({
            imports: [PersonalitiesPageComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                { provide: PersonalityService, useValue: personalityService },
                { provide: UserPreferencesService, useValue: userPrefsService },
                { provide: ConfirmationService, useValue: confirmation },
                { provide: Router, useValue: router },
                {
                    provide: ActivatedRoute,
                    useValue: {
                        snapshot: { queryParamMap: convertToParamMap({}) },
                        queryParamMap: queryParamMap$.asObservable(),
                    },
                },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(PersonalitiesPageComponent);
        component = fixture.componentInstance;
        generateModal = TestBed.inject(GeneratePersonalityModalService);
        fixture.detectChanges();
    });

    it('loads personalities on init', () => {
        expect(personalityService.listPersonalities).toHaveBeenCalled();
        expect(component.personalities().length).toBe(3);
        expect(component.totalCount()).toBe(3);
    });

    it('marks the user preferences default as default', () => {
        expect(component.defaultPersonalityId()).toBe('p-1');
    });

    it('sorts by recent (descending last_used_at) by default', () => {
        const visible = component.visiblePersonalities();
        expect(visible[0].id).toBe('p-3');
        expect(visible[1].id).toBe('p-1');
        expect(visible[2].id).toBe('p-2');
    });

    it('sorts alphabetically when filter changes to alpha', () => {
        component.onFiltersChanged({ query: '', sort: 'alpha', defaultOnly: false });
        const visible = component.visiblePersonalities();
        expect(visible.map(p => p.name)).toEqual(['Filbolt Pottsworth', 'Quinn Hawthorne', 'Vera Calder']);
    });

    it('sorts by most-used when filter changes', () => {
        component.onFiltersChanged({ query: '', sort: 'most-used', defaultOnly: false });
        const visible = component.visiblePersonalities();
        expect(visible.map(p => p.id)).toEqual(['p-3', 'p-1', 'p-2']);
    });

    it('default-only filter narrows to default personality', () => {
        component.onFiltersChanged({ query: '', sort: 'recent', defaultOnly: true });
        const visible = component.visiblePersonalities();
        expect(visible.length).toBe(1);
        expect(visible[0].id).toBe('p-1');
    });

    it('changing query reloads personalities', () => {
        personalityService.listPersonalities.mockClear();
        component.onFiltersChanged({ query: 'quinn', sort: 'recent', defaultOnly: false });
        expect(personalityService.listPersonalities).toHaveBeenCalledTimes(1);
        expect(vi.mocked(personalityService.listPersonalities).mock.lastCall![2]).toEqual({ name: 'quinn' });
    });

    it('open action navigates to detail', () => {
        component.onAction({ action: 'open', personality: personalities[1] });
        expect(router.navigate).toHaveBeenCalledWith(['/personality', 'p-2']);
    });

    it('edit action opens the edit modal', () => {
        component.onAction({ action: 'edit', personality: personalities[1] });
        expect(component.isEditOpen()).toBe(true);
        expect(component.editingPersonality()?.id).toBe('p-2');
    });

    it('set-default action updates preferences', async () => {
        userPrefsService.updateUserPreferences.mockReturnValue(of({ ...preferences, default_personality_id: 'p-2' } as UserPreferences));
        await component.makeDefault(personalities[1]);
        expect(userPrefsService.updateUserPreferences).toHaveBeenCalled();
        expect(component.defaultPersonalityId()).toBe('p-2');
    });

    it('delete confirms before deleting', async () => {
        confirmation.confirm.mockResolvedValue(false);
        await component.deletePersonality(personalities[0]);
        expect(personalityService.deletePersonality).not.toHaveBeenCalled();
    });

    it('opens generate modal instead of navigating', () => {
        component.navigateGenerate();
        expect(generateModal.open()).toBe(true);
        expect(router.navigate).not.toHaveBeenCalledWith(['/personality/generate']);
    });

    it('opens create modal when create=1 query param is set after init', () => {
        expect(component.isCreateOpen()).toBe(false);
        queryParamMap$.next(convertToParamMap({ create: '1' }));
        fixture.detectChanges();
        expect(component.isCreateOpen()).toBe(true);
        expect(router.navigate).toHaveBeenCalledWith([], expect.objectContaining({
            queryParams: { create: null },
            queryParamsHandling: 'merge',
            replaceUrl: true,
        }));
    });

    it('blocks create when system prompt exceeds hard limit', () => {
        component.openCreate();
        component.setCreateName('Too Long');
        component.setCreateSystemPrompt('x'.repeat(TEXT_LIMIT_HARD_MAX + 1));

        component.submitCreate();

        expect(personalityService.createPersonality).not.toHaveBeenCalled();
        expect(component.createErrorMessage()).toContain('cannot exceed');
    });

    it('shows near-limit warning and creates after confirmation', () => {
        personalityService.createPersonality.mockReturnValue(of(makePersonality({ id: 'new-id', name: 'Near', system_prompt: 'ok' })));
        component.openCreate();
        component.setCreateName('Near Limit');
        component.setCreateSystemPrompt('x'.repeat(TEXT_LIMIT_WARNING_THRESHOLD));

        component.submitCreate();
        expect(component.createLimitWarningOpen()).toBe(true);
        expect(personalityService.createPersonality).not.toHaveBeenCalled();

        component.confirmCreateDespiteWarning();

        expect(personalityService.createPersonality).toHaveBeenCalled();
    });
});
