import type { MockedObject } from "vitest";
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { of, throwError } from 'rxjs';

import { RitualService } from './ritual.service';
import { RitualViewService } from './ritual-view.service';
import { Ritual } from '../models/ritual.model';

describe('RitualViewService', () => {
    let service: RitualViewService;
    let ritualService: Pick<MockedObject<RitualService>, 'listRituals' | 'listSystemRituals' | 'deleteRitual'>;

    const ritual: Ritual = {
        id: 'r-1',
        name: 'Morning brief',
        description: 'Start the day',
        content: 'Summarize priorities',
        hotkeys: 'ctrl+m',
        personality_id: null,
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
    };

    beforeEach(() => {
        ritualService = {
            listRituals: vi.fn().mockName("RitualService.listRituals"),
            listSystemRituals: vi.fn().mockName("RitualService.listSystemRituals"),
            deleteRitual: vi.fn().mockName("RitualService.deleteRitual")
        } as unknown as Pick<MockedObject<RitualService>, 'listRituals' | 'listSystemRituals' | 'deleteRitual'>;
        ritualService.listRituals.mockReturnValue(of({ results: [ritual], total_count: 1, page: 1 }));
        ritualService.listSystemRituals.mockReturnValue(of([]));
        ritualService.deleteRitual.mockReturnValue(of(void 0));

        TestBed.configureTestingModule({
            providers: [provideZonelessChangeDetection(), RitualViewService, { provide: RitualService, useValue: ritualService }],
        });
        service = TestBed.inject(RitualViewService);
    });

    it('loads rituals and resets selection', () => {
        service.selectedIds.set(['old']);
        service.load(2);

        expect(ritualService.listRituals).toHaveBeenCalled();
        expect(service.currentPage()).toBe(2);
        expect(service.rituals()).toEqual([ritual]);
        expect(service.selectedIds()).toEqual([]);
    });

    it('sets error when loading fails', () => {
        ritualService.listRituals.mockReturnValue(throwError(() => new Error('boom')));
        service.load();
        expect(service.error()).toBe('boom');
        expect(service.rituals()).toEqual([]);
    });

    it('updates filters and reloads first page', () => {
        service.setFilters({ query: 'meeting' });
        expect(service.filters().query).toBe('meeting');
        expect(service.currentPage()).toBe(1);
    });

    it('toggles and bulk-selects ritual ids', () => {
        service.load();
        service.toggleSelection('r-1');
        expect(service.selectedIds()).toEqual(['r-1']);
        service.setAllSelected(true);
        expect(service.selectedIds()).toEqual(['r-1']);
    });

    it('deletes selected rituals', async () => {
        service.load();
        service.setAllSelected(true);
        service.deleteSelected().subscribe(() => {
            expect(ritualService.deleteRitual).toHaveBeenCalledWith('r-1');
            ;
        });
    });
});
