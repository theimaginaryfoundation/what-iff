import type { MockedObject } from "vitest";
import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { of } from 'rxjs';

import { MemoryService } from './memory.service';
import { MemoryViewService } from './memory-view.service';

describe('MemoryViewService', () => {
    let service: MemoryViewService;
    let memoryApi: Pick<MockedObject<MemoryService>, 'getMemories' | 'deleteMemory'>;

    beforeEach(() => {
        memoryApi = {
            getMemories: vi.fn().mockName("MemoryService.getMemories"),
            deleteMemory: vi.fn().mockName("MemoryService.deleteMemory")
        } as unknown as Pick<MockedObject<MemoryService>, 'getMemories' | 'deleteMemory'>;
        memoryApi.getMemories.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));
        memoryApi.deleteMemory.mockReturnValue(of(void 0));
        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                MemoryViewService,
                { provide: MemoryService, useValue: memoryApi },
            ],
        });
        service = TestBed.inject(MemoryViewService);
    });

    it('loads first page', () => {
        service.load(1);
        expect(memoryApi.getMemories).toHaveBeenCalled();
        expect(service.currentPage()).toBe(1);
    });

    it('resets page on setFilters', () => {
        service.setFilters({ query: 'abc' });
        expect(service.filters().query).toBe('abc');
        expect(service.currentPage()).toBe(1);
    });

    it('forwards sort to memory request', () => {
        service.setFilters({ sort: 'updated_desc' });
        const filters = vi.mocked(memoryApi.getMemories).mock.lastCall?.[2];
        expect(filters?.sort).toBe('updated_desc');
    });

    it('toggles selected ids', () => {
        service.toggleSelection('m-1');
        expect(service.selectedIds()).toEqual(['m-1']);
        service.toggleSelection('m-1');
        expect(service.selectedIds()).toEqual([]);
    });

    it('applies global association filter to request', () => {
        service.selectGlobalAssociations();
        const filters = vi.mocked(memoryApi.getMemories).mock.lastCall?.[2];
        expect(filters?.global_only).toBe(true);
    });

    it('applies personality association list to request', () => {
        service.setSelectedPersonalityIds(['p-1', 'p-2']);
        const filters = vi.mocked(memoryApi.getMemories).mock.lastCall?.[2];
        expect(filters?.pinned_personality_ids).toEqual(['p-1', 'p-2']);
    });
});
