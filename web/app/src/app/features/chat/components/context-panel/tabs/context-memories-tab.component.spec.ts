import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { Router } from '@angular/router';
import { of } from 'rxjs';

import { Memory, MemoryFilters } from '../../../../../core/models/memory.model';
import { ConfirmationService } from '../../../../../core/services/confirmation.service';
import { MemoryService } from '../../../../../core/services/memory.service';
import { ContextMemoriesTabComponent } from './context-memories-tab.component';

describe('ContextMemoriesTabComponent', () => {
    type MemoryServiceMock = Pick<MockedObject<MemoryService>, 'getMemories' | 'createMemory' | 'patchMemory' | 'deleteMemory'>;

    let fixture: ComponentFixture<ContextMemoriesTabComponent>;
    let memoryService: MemoryServiceMock;
    let confirmationService: Pick<MockedObject<ConfirmationService>, 'confirm'>;

    beforeEach(async () => {
        confirmationService = { confirm: vi.fn().mockName("ConfirmationService.confirm") } as unknown as Pick<MockedObject<ConfirmationService>, 'confirm'>;
        memoryService = {
            getMemories: vi.fn().mockName("MemoryService.getMemories"),
            createMemory: vi.fn().mockName("MemoryService.createMemory"),
            patchMemory: vi.fn().mockName("MemoryService.patchMemory"),
            deleteMemory: vi.fn().mockName("MemoryService.deleteMemory")
        } as unknown as MemoryServiceMock;
        memoryService.getMemories.mockImplementation((_page?: number, _limit?: number, filters?: MemoryFilters) => {
            let results: Memory[] = [];
            if (filters?.level === 'thread')
                results = [memory({ id: 'thread-1', content: 'thread memory', level: 'thread' })];
            if (filters?.level === 'global')
                results = [memory({ id: 'global-1', content: 'global memory', level: 'global' })];
            if (filters?.pinned_personality_id)
                results = [memory({ id: 'persona-1', content: 'persona memory', pinned_personality_id: 'p1' })];
            return of({ results, total_count: results.length, page: 1 });
        });
        memoryService.createMemory.mockReturnValue(of({
            id: 'mem-2',
            chat_id: 'chat-1',
            content: 'created',
            level: 'thread',
            type: 'Context',
            status: 'active',
            confidence: 0.6,
            starred: false,
            created_at: '',
            updated_at: '',
        }));
        memoryService.deleteMemory.mockReturnValue(of(void 0));
        memoryService.patchMemory.mockReturnValue(of({
            id: 'mem-1',
            chat_id: 'chat-1',
            content: 'updated',
            level: 'thread',
            type: 'Context',
            status: 'active',
            confidence: 0.6,
            starred: false,
            created_at: '',
            updated_at: '',
        }));

        await TestBed.configureTestingModule({
            imports: [ContextMemoriesTabComponent],
            providers: [
                provideZonelessChangeDetection(),
                { provide: MemoryService, useValue: memoryService },
                { provide: ConfirmationService, useValue: confirmationService },
                { provide: Router, useValue: {
                        navigate: vi.fn().mockName("Router.navigate")
                    } },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(ContextMemoriesTabComponent);
        fixture.componentRef.setInput('chatId', 'chat-1');
        fixture.componentRef.setInput('personalityId', 'p1');
        fixture.detectChanges();
        await fixture.whenStable();
        await new Promise(resolve => setTimeout(resolve));
        fixture.detectChanges();
    });

    it('renders memory scope tabs and defaults to thread memories', () => {
        expect(memoryService.getMemories).toHaveBeenCalled();
        expect(fixture.nativeElement.textContent).toContain('This thread');
        expect(fixture.nativeElement.textContent).toContain('Global');
        expect(fixture.nativeElement.textContent).toContain('thread memory');
        expect(fixture.nativeElement.textContent).not.toContain('global memory');
    });

    it('switches to global and personality memories', () => {
        const globalTab = fixture.nativeElement.querySelectorAll('.context-tabs button')[1] as HTMLButtonElement;
        globalTab.click();
        fixture.detectChanges();

        expect(fixture.nativeElement.textContent).toContain('global memory');
        expect(fixture.nativeElement.textContent).toContain('persona memory');
        expect(fixture.nativeElement.textContent).not.toContain('thread memory');
    });

    it('renders memory actions in the bottom action section', () => {
        const actions = fixture.nativeElement.querySelector('.memory-actions') as HTMLElement;

        expect(actions).toBeTruthy();
        expect(actions.textContent).toContain('Add memory');
        expect(actions.textContent).toContain('Manage all memories');
    });

    const clickDelete = async () => {
        const buttons = fixture.nativeElement.querySelectorAll('.memory-meta button') as NodeListOf<HTMLButtonElement>;
        const deleteButton = Array.from(buttons)
            .find((button): button is HTMLButtonElement => button.textContent?.trim() === 'delete');
        deleteButton?.click();
        await fixture.whenStable();
    };

    it('deletes memories through the memory service after ConfirmationService agrees', async () => {
        const nativeConfirm = vi.spyOn(window, 'confirm');
        confirmationService.confirm.mockResolvedValue(true);

        await clickDelete();

        expect(confirmationService.confirm).toHaveBeenCalledWith(expect.objectContaining({ type: 'danger' }));
        expect(nativeConfirm).not.toHaveBeenCalled();
        expect(memoryService.deleteMemory).toHaveBeenCalledWith('thread-1');
    });

    it('does not delete when the confirmation is declined', async () => {
        confirmationService.confirm.mockResolvedValue(false);

        await clickDelete();

        expect(memoryService.deleteMemory).not.toHaveBeenCalled();
    });
});

function memory(overrides: Partial<Memory>): Memory {
    return {
        id: 'mem-1',
        chat_id: 'chat-1',
        content: 'remember this',
        level: 'thread',
        type: 'Context',
        status: 'active',
        confidence: 0.6,
        starred: false,
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
        ...overrides,
    };
}
