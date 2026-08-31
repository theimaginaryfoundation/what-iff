import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideRouter, Router } from '@angular/router';
import { of } from 'rxjs';

import { ChatService } from '../../core/services/chat.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { MemoryService } from '../../core/services/memory.service';
import { PersonalityService } from '../../core/services/personality.service';
import { CompactionEvent } from '../../core/models/memory.model';
import { Personality, PersonalityPromptChange } from '../../core/models/personality.model';
import { CompactionLogPageComponent } from './compaction-log-page.component';

describe('CompactionLogPageComponent', () => {
    let component: CompactionLogPageComponent;
    let fixture: ComponentFixture<CompactionLogPageComponent>;
    let personalityService: {
        listPersonalities: ReturnType<typeof vi.fn>;
        listPromptChanges: ReturnType<typeof vi.fn>;
        revertPromptChange: ReturnType<typeof vi.fn>;
    };
    let confirmation: { confirm: ReturnType<typeof vi.fn> };

    const event: CompactionEvent = {
        id: 'checkpoint-1',
        chat_id: 'chat-1',
        chat_name: 'A long-running project conversation',
        reason: 'assistant_messages_since_checkpoint (20) >= 20',
        loaded_memories: [{ memory_id: 'memory-1', content: 'User prefers concise updates.', scope: 'global' }],
        created_at: '2026-08-11T12:00:00Z',
        updated_at: '2026-08-11T12:00:00Z',
    };

    const personality: Personality = {
        id: 'personality-9',
        name: 'Vera',
        system_prompt: 'Current prompt',
        auto_pin_memories: false,
        cover_image_id: null,
        cover_image_url: null,
        expressions_enabled: true,
        image_style: 'auto',
        created_at: '2026-08-11T00:00:00Z',
        updated_at: '2026-08-11T00:00:00Z',
        stats: { chat_count: 1, last_used_at: null },
    };

    const promptChange: PersonalityPromptChange = {
        id: 'change-1',
        user_id: 'user-1',
        personality_id: 'personality-9',
        old_prompt: 'Old prompt text',
        new_prompt: 'New prompt text',
        action: 'edit',
        created_at: '2026-08-12T12:00:00Z',
    };

    beforeEach(async () => {
        const memoryService = {
            listCompactionEvents: vi.fn().mockName("MemoryService.listCompactionEvents"),
            revertSnapshot: vi.fn().mockName("MemoryService.revertSnapshot")
        };
        memoryService.listCompactionEvents.mockReturnValue(of({ results: [event], total_count: 1, page: 1 }));

        const chatService = {
            listChats: vi.fn().mockName("ChatService.listChats")
        };
        chatService.listChats.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));

        personalityService = {
            listPersonalities: vi.fn().mockName("PersonalityService.listPersonalities"),
            listPromptChanges: vi.fn().mockName("PersonalityService.listPromptChanges"),
            revertPromptChange: vi.fn().mockName("PersonalityService.revertPromptChange"),
        };
        personalityService.listPersonalities.mockReturnValue(of({ results: [personality], total_count: 1, page: 1 }));
        personalityService.listPromptChanges.mockReturnValue(of([promptChange]));
        personalityService.revertPromptChange.mockReturnValue(of({ ...promptChange, id: 'change-2', action: 'revert' }));

        confirmation = { confirm: vi.fn().mockName("ConfirmationService.confirm") };
        confirmation.confirm.mockResolvedValue(true);

        await TestBed.configureTestingModule({
            imports: [CompactionLogPageComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideRouter([]),
                { provide: MemoryService, useValue: memoryService },
                { provide: ChatService, useValue: chatService },
                { provide: PersonalityService, useValue: personalityService },
                { provide: ConfirmationService, useValue: confirmation },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(CompactionLogPageComponent);
        component = fixture.componentInstance;
        fixture.detectChanges();
        component.toggleExpanded(event);
        fixture.detectChanges();
    });

    it('shows labeled, human-readable status chips', () => {
        const text = fixture.nativeElement.textContent;
        expect(text).toContain('Thread');
        expect(text).toContain('A long-running project conversation');
        expect(text).toContain('Checkpoint');
        expect(text).toContain('Turn limit reached');
        expect(text).toContain('Memory changes');
        expect(text).not.toContain('(20) >= 20');
    });

    it('keeps personality prompt audit details collapsed by default so the checkpoint feed stays visible', () => {
        const toggle = fixture.nativeElement.querySelector('[data-testid="prompt-change-toggle"]') as HTMLButtonElement | null;
        expect(personalityService.listPromptChanges).toHaveBeenCalledWith('personality-9');
        expect(toggle).not.toBeNull();
        expect(toggle?.getAttribute('aria-expanded')).toBe('false');
        expect(fixture.nativeElement.textContent).toContain('Personality prompt changes');
        expect(fixture.nativeElement.textContent).toContain('A long-running project conversation');
        expect(fixture.nativeElement.textContent).not.toContain('Old prompt text');
        expect(fixture.nativeElement.textContent).not.toContain('New prompt text');

        component.togglePromptChanges();
        fixture.detectChanges();

        expect(toggle?.getAttribute('aria-expanded')).toBe('true');
        expect(fixture.nativeElement.textContent).toContain('Vera');
        expect(fixture.nativeElement.textContent).toContain('Old prompt text');
        expect(fixture.nativeElement.textContent).toContain('New prompt text');
        expect(fixture.nativeElement.textContent).toContain('Restore previous');
    });

    it('restores a historical personality prompt through the audit entry', async () => {
        personalityService.listPromptChanges.mockClear();
        await component.revertPromptChange({ ...promptChange, personality_name: 'Vera' });

        expect(confirmation.confirm).toHaveBeenCalled();
        expect(personalityService.revertPromptChange).toHaveBeenCalledWith('personality-9', 'change-1');
        expect(personalityService.listPromptChanges).toHaveBeenCalledWith('personality-9');
        expect(component.notice()).toBe('Prompt restored for Vera.');
    });

    it('collapses and expands the loaded memories segment', () => {
        const toggle = fixture.nativeElement.querySelector('.loaded-memories-toggle') as HTMLButtonElement | null;
        expect(toggle, 'loaded memories toggle').not.toBeNull();
        expect(toggle?.getAttribute('aria-expanded')).toBe('true');
        expect(toggle?.querySelector('.compaction-toggle__chevron')).not.toBeNull();
        expect(component.isLoadedMemoriesCollapsed(event)).toBe(false);
        expect(fixture.nativeElement.textContent).toContain('User prefers concise updates.');

        component.toggleLoadedMemories(event);
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('.loaded-memories-toggle')?.getAttribute('aria-expanded')).toBe('false');
        expect(component.isLoadedMemoriesCollapsed(event)).toBe(true);
        expect(fixture.nativeElement.textContent).not.toContain('User prefers concise updates.');

        component.toggleLoadedMemories(event);
        expect(component.isLoadedMemoriesCollapsed(event)).toBe(false);
    });

    it('renders created memories from a checkpoint', () => {
        const withCreated: CompactionEvent = {
            ...event,
            id: 'checkpoint-2',
            created_memories: [{ memory_id: 'memory-2', content: 'User is allergic to shellfish.', scope: 'global' }],
        };
        component.events.set([withCreated]);
        fixture.detectChanges();
        component.toggleExpanded(withCreated);
        fixture.detectChanges();

        const text = fixture.nativeElement.textContent;
        expect(text).toContain('New memories (1)');
        expect(text).toContain('User is allergic to shellfish.');
    });

    it('maps checkpoint reason codes to human-readable labels', () => {
        expect(component.checkpointReasonLabel('last_input_tokens (12000) >= 12000')).toBe('Input size limit reached');
        expect(component.checkpointReasonLabel('estimated_context_tokens (900000) >= 900000')).toBe('Conversation size limit reached');
        expect(component.checkpointReasonLabel(null)).toBe('Checkpoint created');
    });

    it('narrows results by thread and personality filters', () => {
        const memoryService = TestBed.inject(MemoryService) as MockedObject<MemoryService>;
        memoryService.listCompactionEvents.mockClear();
        personalityService.listPromptChanges.mockClear();

        component.updateFilters('chat-42', 'personality-9');

        expect(component.selectedChatID()).toBe('chat-42');
        expect(component.selectedPersonalityID()).toBe('personality-9');
        expect(memoryService.listCompactionEvents).toHaveBeenCalledWith(1, 20, {
            chat_id: 'chat-42',
            personality_id: 'personality-9',
        });
        expect(personalityService.listPromptChanges).toHaveBeenCalledWith('personality-9');
    });

    it('collapses and expands the summaries section', () => {
        expect(component.isSectionCollapsed(event, 'summary')).toBe(false);
        expect(fixture.nativeElement.textContent).toContain('Conversation summary');

        component.toggleSection(event, 'summary');
        fixture.detectChanges();
        expect(component.isSectionCollapsed(event, 'summary')).toBe(true);
        expect(fixture.nativeElement.textContent).not.toContain('Conversation summary');

        component.toggleSection(event, 'summary');
        fixture.detectChanges();
        expect(component.isSectionCollapsed(event, 'summary')).toBe(false);
        expect(fixture.nativeElement.textContent).toContain('Conversation summary');
    });

    it('opens the originating thread at the checkpoint message', () => {
        const router = TestBed.inject(Router);
        const navigateSpy = vi.spyOn(router, 'navigate').mockResolvedValue(true);

        const withMessage: CompactionEvent = { ...event, assistant_message_id: 'msg-77' };
        component.openThread(withMessage);
        expect(navigateSpy).toHaveBeenCalledWith(['/chat', 'chat-1'], { queryParams: { checkpoint: 'msg-77' } });

        navigateSpy.mockClear();
        component.openThread(event);
        expect(navigateSpy).toHaveBeenCalledWith(['/chat', 'chat-1'], { queryParams: undefined });
    });
});
