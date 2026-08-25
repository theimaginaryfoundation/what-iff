import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { of } from 'rxjs';

import { Chat } from '../../../../core/models/chat.model';
import { ChatService } from '../../../../core/services/chat.service';
import { ConfirmationService } from '../../../../core/services/confirmation.service';
import { ThreadListService } from '../../../../core/services/thread-list.service';
import { ThreadListPanelComponent } from './thread-list-panel.component';

function makeChat(overrides: Partial<Chat> = {}): Chat {
    return {
        id: 'chat-id',
        user_id: 'user-1',
        name: 'Thread',
        created_at: '2026-04-01T15:40:00Z',
        updated_at: '2026-05-05T20:02:00Z',
        ...overrides,
    };
}

type ChatServiceMock = Pick<MockedObject<ChatService>, 'listChats' | 'listAllChats' | 'patchChat' | 'deleteChat'>;
type ConfirmationServiceMock = Pick<MockedObject<ConfirmationService>, 'confirm'>;

describe('ThreadListPanelComponent', () => {
    let fixture: ComponentFixture<ThreadListPanelComponent>;
    let component: ThreadListPanelComponent;
    let service: ThreadListService;
    let chatService: ChatServiceMock;
    let confirmationSpy: ConfirmationServiceMock;

    beforeEach(async () => {
        chatService = {
            listChats: vi.fn().mockName("ChatService.listChats"),
            listAllChats: vi.fn().mockName("ChatService.listAllChats"),
            patchChat: vi.fn().mockName("ChatService.patchChat"),
            deleteChat: vi.fn().mockName("ChatService.deleteChat")
        } as unknown as ChatServiceMock;
        confirmationSpy = {
            confirm: vi.fn().mockName("ConfirmationService.confirm")
        } as unknown as ConfirmationServiceMock;
        confirmationSpy.confirm.mockResolvedValue(true);
        const seedChats = [
            makeChat({
                id: 'phantom',
                name: 'The Lighthouse Draft',
                is_favorite: true,
                personality_id: 'phantom',
                personality_name: 'Phantom',
                tags: ['horror', 'scene'],
            }),
            makeChat({
                id: 'aster',
                name: 'Equity Resilience Readout',
                personality_id: 'aster',
                personality_name: 'Aster C.',
                tags: ['finance'],
            }),
        ];
        chatService.listChats.mockReturnValue(of({ results: seedChats, page: 1, total_count: 2 }));
        chatService.listAllChats.mockReturnValue(of({ chats: seedChats, truncated: false }));
        chatService.patchChat.mockImplementation((id: string, patch: Partial<Chat>) => of(makeChat({ id, ...patch })));
        chatService.deleteChat.mockReturnValue(of(void 0));

        await TestBed.configureTestingModule({
            imports: [ThreadListPanelComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                ThreadListService,
                { provide: ChatService, useValue: chatService },
                { provide: ConfirmationService, useValue: confirmationSpy },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(ThreadListPanelComponent);
        component = fixture.componentInstance;
        service = TestBed.inject(ThreadListService);
        await service.refresh();
        fixture.detectChanges();
    });

    it('renders Concept D manager tabs and table headers', () => {
        const text = fixture.nativeElement.textContent;

        expect(text).toContain('Thread Manager');
        expect(text).toContain('Active');
        expect(text).toContain('Archived');
        expect(text).toContain('STARRED?');
        expect(text).toContain('PERSONALITY');
        expect(text).toContain('ARCHIVE?');
    });

    it('loads archived threads when archived tab is selected', async () => {
        chatService.listAllChats.mockClear();

        const archivedTab = fixture.nativeElement.querySelectorAll('.panel__tabs button')[1] as HTMLButtonElement;
        archivedTab.click();
        fixture.detectChanges();

        await fixture.whenStable();
        fixture.detectChanges();
        await fixture.whenStable();

        expect(component.threadListTab()).toBe('archived');
        expect(chatService.listAllChats).toHaveBeenCalled();
        const lastArgs = vi.mocked(chatService.listAllChats).mock.lastCall;
        expect(lastArgs![1]).toEqual(expect.objectContaining({ archived: true }));
        expect(fixture.nativeElement.textContent).toContain('RESTORE?');
    });

    it('adds and removes tag chips from the search dropdown', () => {
        const input = fixture.nativeElement.querySelector('.panel__search input') as HTMLInputElement;
        input.value = 'hor';
        input.dispatchEvent(new Event('input'));
        input.dispatchEvent(new Event('focus'));
        fixture.detectChanges();

        const option = fixture.nativeElement.querySelector('.panel__tag-dropdown button') as HTMLButtonElement;
        option.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
        fixture.detectChanges();

        expect(component.selectedTagFilters()).toEqual(['horror']);
        expect(fixture.nativeElement.textContent).toContain('The Lighthouse Draft');
        expect(fixture.nativeElement.textContent).not.toContain('Equity Resilience Readout');

        const remove = fixture.nativeElement.querySelector('.panel__tag-chip button') as HTMLButtonElement;
        remove.click();
        fixture.detectChanges();

        expect(component.selectedTagFilters()).toEqual([]);
    });

    it('calls setThreadArchived when Archive is clicked', async () => {
        vi.spyOn(service, 'setThreadArchived').mockResolvedValue(true);

        const archiveButton = fixture.nativeElement.querySelector('.thread-row__archive') as HTMLButtonElement;
        archiveButton.click();
        await fixture.whenStable();

        expect(service.setThreadArchived).toHaveBeenCalledWith(expect.any(Object), true);
    });

    it('confirms and deletes a thread when delete is clicked', async () => {
        vi.spyOn(service, 'deleteThread').mockResolvedValue(true);

        const deleteButton = fixture.nativeElement.querySelector('.thread-row__delete') as HTMLButtonElement;
        expect(deleteButton, 'delete button').toBeTruthy();
        deleteButton.click();
        await fixture.whenStable();

        expect(confirmationSpy.confirm).toHaveBeenCalledWith(expect.objectContaining({
            title: 'Delete thread?',
            confirmText: 'Delete',
            type: 'danger',
        }));
        expect(service.deleteThread).toHaveBeenCalled();
    });

    it('does not delete when confirmation is cancelled', async () => {
        confirmationSpy.confirm.mockResolvedValue(false);
        vi.spyOn(service, 'deleteThread').mockResolvedValue(true);

        const deleteButton = fixture.nativeElement.querySelector('.thread-row__delete') as HTMLButtonElement;
        deleteButton.click();
        await fixture.whenStable();

        expect(service.deleteThread).not.toHaveBeenCalled();
    });

    it('keeps star toggling wired to the thread service', () => {
        vi.spyOn(service, 'togglePinned').mockResolvedValue();

        const starButton = fixture.nativeElement.querySelector('.thread-row__star') as HTMLButtonElement;
        expect(starButton, 'star toggle button').toBeTruthy();
        starButton.click();

        expect(service.togglePinned).toHaveBeenCalled();
    });

    it('commits pending tag text when Save is clicked', async () => {
        const thread = makeChat({ id: 'phantom', tags: ['horror'] });
        vi.spyOn(service, 'setTags').mockResolvedValue(true);

        component.editTags(thread);
        component.tagInputValue.set('scene');
        fixture.detectChanges();

        await component.saveTagEdit();
        await fixture.whenStable();

        expect(service.setTags).toHaveBeenCalledWith(thread, expect.arrayContaining(['horror', 'scene']));
        expect(component.tagEditThread()).toBeNull();
    });

    it('does not save when pending tag text exceeds the length limit', async () => {
        const thread = makeChat({ id: 'phantom', tags: [] });
        vi.spyOn(service, 'setTags').mockResolvedValue(true);

        component.editTags(thread);
        component.tagInputValue.set('waytoolongtag');
        fixture.detectChanges();

        await component.saveTagEdit();
        await fixture.whenStable();

        expect(service.setTags).not.toHaveBeenCalled();
        expect(component.tagEditError()).toContain('10 characters');
        expect(component.tagEditThread()).not.toBeNull();
    });

    describe('bulk selection', () => {
        it('shows the bulk bar with a running count once rows are checked, and hides it when cleared', () => {
            expect(fixture.nativeElement.querySelector('.panel__bulk-bar')).toBeNull();

            const checkboxes = fixture.nativeElement.querySelectorAll('.thread-row__select') as NodeListOf<HTMLInputElement>;
            checkboxes[0].click();
            fixture.detectChanges();

            const bar = fixture.nativeElement.querySelector('.panel__bulk-bar') as HTMLElement;
            expect(bar, 'bulk bar').toBeTruthy();
            expect(bar.textContent).toContain('1 selected');

            const clearButton = fixture.nativeElement.querySelector('.panel__bulk-clear') as HTMLButtonElement;
            clearButton.click();
            fixture.detectChanges();

            expect(fixture.nativeElement.querySelector('.panel__bulk-bar')).toBeNull();
        });

        it('select-all header checkbox selects all visible rows and reflects indeterminate/all state', () => {
            const selectAll = fixture.nativeElement.querySelector('.panel__select-all') as HTMLInputElement;
            const rowCheckboxes = () => fixture.nativeElement.querySelectorAll('.thread-row__select') as NodeListOf<HTMLInputElement>;

            expect(selectAll.checked).toBe(false);
            expect(selectAll.indeterminate).toBe(false);

            rowCheckboxes()[0].click();
            fixture.detectChanges();
            expect(selectAll.indeterminate).toBe(true);

            selectAll.click();
            fixture.detectChanges();
            expect(selectAll.checked).toBe(true);
            expect(selectAll.indeterminate).toBe(false);
            rowCheckboxes().forEach(cb => expect(cb.checked).toBe(true));

            selectAll.click();
            fixture.detectChanges();
            expect(selectAll.checked).toBe(false);
            rowCheckboxes().forEach(cb => expect(cb.checked).toBe(false));
        });

        it('runs bulk archive on the selected threads via Choose action', async () => {
            vi.spyOn(service, 'bulkSetArchived').mockResolvedValue();

            const checkboxes = fixture.nativeElement.querySelectorAll('.thread-row__select') as NodeListOf<HTMLInputElement>;
            checkboxes[0].click();
            fixture.detectChanges();

            (fixture.nativeElement.querySelector('.panel__bulk-action-btn') as HTMLButtonElement).click();
            fixture.detectChanges();

            const archiveOption = Array.from(fixture.nativeElement.querySelectorAll('.panel__bulk-action-menu [role="option"]')).find(el => (el as HTMLElement).textContent?.trim() === 'Archive') as HTMLButtonElement;
            expect(archiveOption, 'Archive option').toBeTruthy();
            archiveOption.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
            await fixture.whenStable();

            expect(service.bulkSetArchived).toHaveBeenCalledWith(['phantom'], true);
        });

        it('confirms and runs bulk delete on the selected threads', async () => {
            vi.spyOn(service, 'bulkDelete').mockResolvedValue();

            const checkboxes = fixture.nativeElement.querySelectorAll('.thread-row__select') as NodeListOf<HTMLInputElement>;
            checkboxes[0].click();
            checkboxes[1].click();
            fixture.detectChanges();

            (fixture.nativeElement.querySelector('.panel__bulk-action-btn') as HTMLButtonElement).click();
            fixture.detectChanges();

            const deleteOption = Array.from(fixture.nativeElement.querySelectorAll('.panel__bulk-action-menu [role="option"]')).find(el => (el as HTMLElement).textContent?.trim() === 'Delete') as HTMLButtonElement;
            deleteOption.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
            await fixture.whenStable();

            expect(confirmationSpy.confirm).toHaveBeenCalledWith(expect.objectContaining({
                title: 'Delete threads?',
                confirmText: 'Delete',
                type: 'danger',
            }));
            expect(service.bulkDelete).toHaveBeenCalledWith(['phantom', 'aster']);
        });

        it('skips bulk delete when the confirm dialog is dismissed', async () => {
            confirmationSpy.confirm.mockResolvedValue(false);
            vi.spyOn(service, 'bulkDelete').mockResolvedValue();

            const checkboxes = fixture.nativeElement.querySelectorAll('.thread-row__select') as NodeListOf<HTMLInputElement>;
            checkboxes[0].click();
            fixture.detectChanges();

            (fixture.nativeElement.querySelector('.panel__bulk-action-btn') as HTMLButtonElement).click();
            fixture.detectChanges();

            const deleteOption = Array.from(fixture.nativeElement.querySelectorAll('.panel__bulk-action-menu [role="option"]')).find(el => (el as HTMLElement).textContent?.trim() === 'Delete') as HTMLButtonElement;
            deleteOption.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
            await fixture.whenStable();

            expect(service.bulkDelete).not.toHaveBeenCalled();
        });

        it('assigns the selected threads to a personality via the picker modal', async () => {
            vi.spyOn(service, 'bulkAssignPersonality').mockResolvedValue();
            fixture.componentRef.setInput('personalities', [
                { id: 'nova', name: 'Nova' },
                { id: 'phantom', name: 'Phantom' },
            ]);
            fixture.detectChanges();

            const checkboxes = fixture.nativeElement.querySelectorAll('.thread-row__select') as NodeListOf<HTMLInputElement>;
            checkboxes[0].click();
            fixture.detectChanges();

            (fixture.nativeElement.querySelector('.panel__bulk-action-btn') as HTMLButtonElement).click();
            fixture.detectChanges();

            const assignOption = Array.from(fixture.nativeElement.querySelectorAll('.panel__bulk-action-menu [role="option"]')).find(el => (el as HTMLElement).textContent?.trim() === 'Assign to personality…') as HTMLButtonElement;
            assignOption.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
            fixture.detectChanges();

            expect(component.bulkPersonalityPickerOpen()).toBe(true);

            const novaOption = Array.from(fixture.nativeElement.querySelectorAll('.bulk-personality-picker [role="option"]')).find(el => (el as HTMLElement).textContent?.trim() === 'Nova') as HTMLButtonElement;
            expect(novaOption, 'Nova option').toBeTruthy();
            novaOption.click();
            await fixture.whenStable();

            expect(service.bulkAssignPersonality).toHaveBeenCalledWith(['phantom'], 'nova');
            expect(component.bulkPersonalityPickerOpen()).toBe(false);
        });

        it('clears selection when switching tabs', async () => {
            const checkboxes = fixture.nativeElement.querySelectorAll('.thread-row__select') as NodeListOf<HTMLInputElement>;
            checkboxes[0].click();
            fixture.detectChanges();
            expect(service.selectedCount()).toBe(1);

            const archivedTab = fixture.nativeElement.querySelectorAll('.panel__tabs button')[1] as HTMLButtonElement;
            archivedTab.click();
            fixture.detectChanges();
            await fixture.whenStable();

            expect(service.selectedCount()).toBe(0);
        });
    });
});
