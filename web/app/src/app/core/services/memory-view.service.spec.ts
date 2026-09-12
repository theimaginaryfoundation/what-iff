import type { MockedObject } from "vitest";
import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Subject, of, throwError } from 'rxjs';

import { Memory } from '../models/memory.model';
import { MemoryService } from './memory.service';
import { MemoryViewService } from './memory-view.service';

describe('MemoryViewService', () => {
    let service: MemoryViewService;
    let memoryApi: Pick<
        MockedObject<MemoryService>,
        'getMemories' | 'deleteMemory' | 'deleteMemoriesBatch' | 'patchMemoriesBatch' | 'patchMemory'
    >;

    beforeEach(() => {
        memoryApi = {
            getMemories: vi.fn().mockName("MemoryService.getMemories"),
            deleteMemory: vi.fn().mockName("MemoryService.deleteMemory"),
            deleteMemoriesBatch: vi.fn().mockName("MemoryService.deleteMemoriesBatch"),
            patchMemoriesBatch: vi.fn().mockName("MemoryService.patchMemoriesBatch"),
            patchMemory: vi.fn().mockName("MemoryService.patchMemory"),
        } as unknown as Pick<
            MockedObject<MemoryService>,
            'getMemories' | 'deleteMemory' | 'deleteMemoriesBatch' | 'patchMemoriesBatch' | 'patchMemory'
        >;
        memoryApi.getMemories.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));
        memoryApi.deleteMemory.mockReturnValue(of(void 0));
        memoryApi.deleteMemoriesBatch.mockReturnValue(of({ deleted_count: 1 }));
        memoryApi.patchMemoriesBatch.mockReturnValue(of({ results: [], updated_count: 0 }));
        memoryApi.patchMemory.mockReturnValue(of({
            id: 'm-1',
            content: 'x',
            level: 'global',
            type: 'Context',
            status: 'active',
            confidence: 0.6,
            starred: false,
            created_at: '2026-01-01T00:00:00Z',
            updated_at: '2026-01-01T00:00:00Z',
        }));
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

    it('forwards status to memory request', () => {
        service.setFilters({ status: 'inactive' });
        const filters = vi.mocked(memoryApi.getMemories).mock.lastCall?.[2];
        expect(filters?.status).toBe('inactive');
    });

    it('maps summaries status to level=summary on the request', () => {
        service.setFilters({ status: 'summaries' });
        const filters = vi.mocked(memoryApi.getMemories).mock.lastCall?.[2];
        expect(filters?.level).toBe('summary');
        expect(filters?.status).toBe('active');
    });

    it('toggles selected ids', () => {
        service.toggleSelection('m-1');
        expect(service.selectedIds()).toEqual(['m-1']);
        service.toggleSelection('m-1');
        expect(service.selectedIds()).toEqual([]);
    });

    it('deletes selected via batch endpoint', () => {
        service.toggleSelection('m-1');
        service.toggleSelection('m-2');
        service.deleteSelected().subscribe();
        expect(memoryApi.deleteMemoriesBatch).toHaveBeenCalledWith({
            ids: ['m-1', 'm-2'],
            all_or_none: true,
        });
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

    it('deleteSelected short-circuits without calling the API when nothing is selected', () => {
        let received: unknown = 'not-emitted';
        service.deleteSelected().subscribe(value => (received = value));

        expect(memoryApi.deleteMemoriesBatch).not.toHaveBeenCalled();
        expect(received).toBeUndefined();
        expect(service.deleting()).toBe(false);
    });

    it('deleteSelected toggles deleting true while in flight and false on success', () => {
        const subject = new Subject<{ deleted_count: number }>();
        memoryApi.deleteMemoriesBatch.mockReturnValue(subject);
        service.toggleSelection('m-1');

        service.deleteSelected().subscribe();
        expect(service.deleting()).toBe(true);

        subject.next({ deleted_count: 1 });
        subject.complete();
        expect(service.deleting()).toBe(false);
    });

    it('deleteSelected emits void regardless of the response payload', () => {
        service.toggleSelection('m-1');
        let received: unknown = 'not-emitted';
        service.deleteSelected().subscribe(value => (received = value));

        expect(received).toBeUndefined();
    });

    it('deleteSelected propagates the error unchanged and still resets deleting', () => {
        const failure = new Error('batch delete failed');
        memoryApi.deleteMemoriesBatch.mockReturnValue(throwError(() => failure));
        service.toggleSelection('m-1');

        let captured: unknown;
        service.deleteSelected().subscribe({ error: err => (captured = err) });

        expect(captured).toBe(failure);
        expect(service.deleting()).toBe(false);
    });

    it('deleteSelected does not clear selectedIds itself', () => {
        service.toggleSelection('m-1');
        service.toggleSelection('m-2');

        service.deleteSelected().subscribe();

        expect(service.selectedIds()).toEqual(['m-1', 'm-2']);
    });

    it('patchSelected short-circuits without calling the API when nothing is selected', () => {
        let received: unknown = 'not-emitted';
        service.patchSelected({ starred: true }).subscribe(value => (received = value));

        expect(memoryApi.patchMemoriesBatch).not.toHaveBeenCalled();
        expect(received).toBeUndefined();
        expect(service.mutating()).toBe(false);
    });

    it('patchSelected toggles mutating and forwards the exact patch with selected ids', () => {
        const subject = new Subject<{ results: []; updated_count: number }>();
        memoryApi.patchMemoriesBatch.mockReturnValue(subject);
        service.toggleSelection('m-1');
        service.toggleSelection('m-2');
        const patch = { starred: true };

        service.patchSelected(patch).subscribe();
        expect(service.mutating()).toBe(true);
        expect(memoryApi.patchMemoriesBatch).toHaveBeenCalledWith({
            ids: ['m-1', 'm-2'],
            patch,
            all_or_none: true,
        });

        subject.next({ results: [], updated_count: 2 });
        subject.complete();
        expect(service.mutating()).toBe(false);
    });

    it('patchSelected emits void regardless of the response payload', () => {
        service.toggleSelection('m-1');
        let received: unknown = 'not-emitted';
        service.patchSelected({ starred: true }).subscribe(value => (received = value));

        expect(received).toBeUndefined();
    });

    it('patchSelected propagates the error unchanged and still resets mutating', () => {
        const failure = new Error('batch patch failed');
        memoryApi.patchMemoriesBatch.mockReturnValue(throwError(() => failure));
        service.toggleSelection('m-1');

        let captured: unknown;
        service.patchSelected({ starred: true }).subscribe({ error: err => (captured = err) });

        expect(captured).toBe(failure);
        expect(service.mutating()).toBe(false);
    });

    it('patchOne toggles mutating and forwards id and patch to patchMemory', () => {
        const subject = new Subject<Memory>();
        memoryApi.patchMemory.mockReturnValue(subject);
        const patch = { content: 'updated content' };

        let received: unknown = 'not-emitted';
        service.patchOne('m-1', patch).subscribe(value => (received = value));
        expect(service.mutating()).toBe(true);
        expect(memoryApi.patchMemory).toHaveBeenCalledWith('m-1', patch);

        subject.next({
            id: 'm-1',
            content: 'updated content',
            level: 'global',
            type: 'Context',
            status: 'active',
            confidence: 0.6,
            starred: false,
            created_at: '2026-01-01T00:00:00Z',
            updated_at: '2026-01-01T00:00:00Z',
        });
        subject.complete();

        expect(service.mutating()).toBe(false);
        expect(received).toBeUndefined();
    });

    it('patchOne propagates the error unchanged and still resets mutating', () => {
        const failure = new Error('patch failed');
        memoryApi.patchMemory.mockReturnValue(throwError(() => failure));

        let captured: unknown;
        service.patchOne('m-1', { starred: true }).subscribe({ error: err => (captured = err) });

        expect(captured).toBe(failure);
        expect(service.mutating()).toBe(false);
    });

    it('setSelectedIds stores a defensive copy of the given ids', () => {
        const ids = ['m-1', 'm-2'];
        service.setSelectedIds(ids);
        expect(service.selectedIds()).toEqual(['m-1', 'm-2']);

        ids.push('m-3');
        expect(service.selectedIds()).toEqual(['m-1', 'm-2']);
    });

    it('clearSelection resets selectedIds to empty', () => {
        service.setSelectedIds(['m-1', 'm-2']);
        service.clearSelection();
        expect(service.selectedIds()).toEqual([]);
    });

    it('selectedCount tracks selectedIds through toggle, setSelectedIds, and clearSelection', () => {
        expect(service.selectedCount()).toBe(0);

        service.toggleSelection('m-1');
        expect(service.selectedCount()).toBe(1);

        service.setSelectedIds(['m-1', 'm-2', 'm-3']);
        expect(service.selectedCount()).toBe(3);

        service.clearSelection();
        expect(service.selectedCount()).toBe(0);
    });

    it('selectAllAssociations resets association mode to all and clears selected personality ids', () => {
        service.setSelectedPersonalityIds(['p-1']);
        expect(service.associationFilterMode()).toBe('personality');

        service.selectAllAssociations();

        expect(service.associationFilterMode()).toBe('all');
        expect(service.selectedPersonalityIds()).toEqual([]);
        const filters = vi.mocked(memoryApi.getMemories).mock.lastCall?.[2];
        expect(filters?.pinned_personality_ids).toBeUndefined();
        expect(filters?.global_only).toBeUndefined();
    });

    it('selectAllAssociations resets association mode to all after a global filter was set', () => {
        service.selectGlobalAssociations();
        expect(service.associationFilterMode()).toBe('global');

        service.selectAllAssociations();

        expect(service.associationFilterMode()).toBe('all');
        const filters = vi.mocked(memoryApi.getMemories).mock.lastCall?.[2];
        expect(filters?.global_only).toBeUndefined();
    });
});
