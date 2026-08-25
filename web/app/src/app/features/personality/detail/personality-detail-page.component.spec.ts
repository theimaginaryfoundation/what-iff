import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { ActivatedRoute, Router } from '@angular/router';
import { BehaviorSubject, of } from 'rxjs';

import { ChatService } from '../../../core/services/chat.service';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { FileAttachmentService } from '../../../core/services/file-attachment.service';
import { PersonalityService } from '../../../core/services/personality.service';
import { UserPreferencesService } from '../../../core/services/user-preferences.service';
import { Chat } from '../../../core/models/chat.model';
import { Personality } from '../../../core/models/personality.model';
import { UserPreferences } from '../../../core/models/user.model';
import { PersonalityDetailPageComponent } from './personality-detail-page.component';

function makePersonality(overrides: Partial<Personality> = {}): Personality {
    return {
        id: 'p-1',
        name: 'Vera Calder',
        system_prompt: 'A spectral cartographer.',
        scratchpad: 'Scratchpad body',
        scratchpad_update_prompt: 'Update scratchpad carefully',
        archival_model: 'gpt-legacy',
        memory_search_prompt: 'Search memories like this',
        memory_write_prompt: 'Write memories like this',
        auto_pin_memories: false,
        expressions_enabled: true,
        image_style: 'auto',
        cover_image_id: 'img-cover-1',
        cover_image_url: null,
        accent_color: '#7A5AF8',
        thumbnail_circle: { cx: 0.5, cy: 0.42, r: 0.34 },
        created_at: '2026-04-28T00:00:00Z',
        updated_at: '2026-04-28T00:00:00Z',
        stats: { chat_count: 2, last_used_at: null },
        ...overrides,
    };
}

describe('PersonalityDetailPageComponent', () => {
    let fixture: ComponentFixture<PersonalityDetailPageComponent>;
    let chatService: Pick<MockedObject<ChatService>, 'createChat' | 'setLastChatId'>;
    let personalityService: Pick<MockedObject<PersonalityService>, 'getPersonality' | 'listExpressions' | 'updatePersonality' | 'deletePersonality'>;
    let router: Pick<MockedObject<Router>, 'navigate'>;

    const preferences: UserPreferences = {
        id: 'pref-1',
        user_id: 'u-1',
        default_model_id: 'm-1',
        default_personality_id: undefined,
        theme: 'dark',
    };

    beforeEach(async () => {
        personalityService = {
            getPersonality: vi.fn().mockName("PersonalityService.getPersonality"),
            listExpressions: vi.fn().mockName("PersonalityService.listExpressions"),
            updatePersonality: vi.fn().mockName("PersonalityService.updatePersonality"),
            deletePersonality: vi.fn().mockName("PersonalityService.deletePersonality")
        } as unknown as Pick<MockedObject<PersonalityService>, 'getPersonality' | 'listExpressions' | 'updatePersonality' | 'deletePersonality'>;
        personalityService.getPersonality.mockReturnValue(of(makePersonality()));
        personalityService.listExpressions.mockReturnValue(of([]));
        personalityService.updatePersonality.mockReturnValue(of(makePersonality()));

        chatService = {
            createChat: vi.fn().mockName("ChatService.createChat"),
            setLastChatId: vi.fn().mockName("ChatService.setLastChatId")
        } as unknown as Pick<MockedObject<ChatService>, 'createChat' | 'setLastChatId'>;
        chatService.createChat.mockReturnValue(of({
            id: 'chat-1',
            user_id: 'u-1',
            name: 'New Chat',
            personality_id: 'p-1',
            created_at: '',
            updated_at: '',
        } as Chat));

        const preferences$ = new BehaviorSubject<UserPreferences | null>(preferences);
        const userPreferencesService = {
            getUserPreferences: vi.fn().mockName("UserPreferencesService.getUserPreferences"),
            updateUserPreferences: vi.fn().mockName("UserPreferencesService.updateUserPreferences"),
            preferences$: preferences$.asObservable()
        };
        userPreferencesService.getUserPreferences.mockReturnValue(of(preferences));

        const fileAttachmentService = {
            listFileAttachments: vi.fn().mockName("FileAttachmentService.listFileAttachments"),
            uploadPersonalityFileAttachment: vi.fn().mockName("FileAttachmentService.uploadPersonalityFileAttachment"),
            deleteFileAttachment: vi.fn().mockName("FileAttachmentService.deleteFileAttachment")
        };
        fileAttachmentService.listFileAttachments.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));

        const confirmation = {
            confirm: vi.fn().mockName("ConfirmationService.confirm"),
            alert: vi.fn().mockName("ConfirmationService.alert")
        };
        router = {
            navigate: vi.fn().mockName("Router.navigate")
        } as unknown as Pick<MockedObject<Router>, 'navigate'>;

        await TestBed.configureTestingModule({
            imports: [PersonalityDetailPageComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                {
                    provide: ActivatedRoute,
                    useValue: { snapshot: { paramMap: { get: () => 'p-1' } } },
                },
                { provide: ChatService, useValue: chatService },
                { provide: ConfirmationService, useValue: confirmation },
                { provide: FileAttachmentService, useValue: fileAttachmentService },
                { provide: PersonalityService, useValue: personalityService },
                { provide: Router, useValue: router },
                { provide: UserPreferencesService, useValue: userPreferencesService },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(PersonalityDetailPageComponent);
    });

    it('instantiates and loads a personality from the route id', () => {
        fixture.detectChanges();
        expect(fixture.nativeElement.textContent).toContain('Vera Calder');
    });

    it('starts a new chat with the personality without requiring ChatSessionService', async () => {
        fixture.detectChanges();
        await fixture.componentInstance.onUseInNewChat();

        expect(chatService.createChat).toHaveBeenCalledWith({
            name: 'New Chat',
            personality_id: 'p-1',
        });
        expect(chatService.setLastChatId).toHaveBeenCalledWith('chat-1');
        expect(router.navigate).toHaveBeenCalledWith(['/chat', 'chat-1']);
    });

    it('preserves non-edited fields when saving prompt edits', async () => {
        fixture.detectChanges();

        await fixture.componentInstance.onSavePrompt({
            name: 'Vera Calder',
            systemPrompt: 'Updated prompt',
        });

        const request = vi.mocked(personalityService.updatePersonality).mock.lastCall![1];
        expect(request.cover_image_id).toBe('img-cover-1');
        expect(request.accent_color).toBe('#7A5AF8');
        expect(request.thumbnail_circle).toEqual({ cx: 0.5, cy: 0.42, r: 0.34 });
        expect(request.archival_model).toBe('gpt-legacy');
        expect(request.memory_search_prompt).toBe('Search memories like this');
        expect(request.memory_write_prompt).toBe('Write memories like this');
        expect(request.image_style).toBe('auto');
        expect(request.expressions_enabled).toBe(true);
    });
});
