import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { ActivatedRoute, Router } from '@angular/router';
import { of } from 'rxjs';

import { ChatService } from '../../core/services/chat.service';
import { MemoryService } from '../../core/services/memory.service';
import { MemoryViewService } from '../../core/services/memory-view.service';
import { PersonalityService } from '../../core/services/personality.service';
import { MemoriesPageComponent } from './memories-page.component';

describe('MemoriesPageComponent', () => {
    beforeEach(async () => {
        const viewService = {
            filters: () => ({
                scope: 'all',
                level: 'all',
                sort: 'created_desc',
                query: '',
                personalityId: '',
                chatId: '',
                minDate: '',
                maxDate: '',
            }),
            associationFilterMode: () => 'all',
            loading: () => false,
            error: () => null,
            selectedIds: () => [],
            totalPages: () => 1,
            currentPage: () => 1,
            totalCount: () => 0,
            memories: () => [],
            deleting: () => false,
            applyFilters: vi.fn().mockName('applyFilters'),
            setFilters: vi.fn().mockName('setFilters'),
            clearFilters: vi.fn().mockName('clearFilters'),
            load: vi.fn().mockName('load'),
            toggleSelection: vi.fn().mockName('toggleSelection'),
            setAllSelected: vi.fn().mockName('setAllSelected'),
            deleteOne: vi.fn().mockName('deleteOne').mockReturnValue(of(void 0)),
            deleteSelected: vi.fn().mockName('deleteSelected').mockReturnValue(of(void 0)),
        };

        await TestBed.configureTestingModule({
            imports: [MemoriesPageComponent],
            providers: [
                provideZonelessChangeDetection(),
                { provide: MemoryViewService, useValue: viewService },
                { provide: MemoryService, useValue: { exportMemories: () => of(new Blob()) } },
                { provide: PersonalityService, useValue: { listPersonalities: () => of({ results: [] }) } },
                { provide: ChatService, useValue: { listChats: () => of({ results: [] }) } },
                { provide: ActivatedRoute, useValue: { queryParams: of({}) } },
                { provide: Router, useValue: { navigate: vi.fn().mockName('navigate') } },
            ],
        }).compileComponents();
    });

    it('creates', () => {
        const fixture = TestBed.createComponent(MemoriesPageComponent);
        expect(fixture.componentInstance).toBeTruthy();
    });
});
