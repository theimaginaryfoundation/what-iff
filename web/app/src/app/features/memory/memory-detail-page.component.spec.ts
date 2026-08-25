import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { ActivatedRoute, Router } from '@angular/router';
import { of } from 'rxjs';

import { MemoryService } from '../../core/services/memory.service';
import { PersonalityService } from '../../core/services/personality.service';
import { MemoryDetailPageComponent } from './memory-detail-page.component';

describe('MemoryDetailPageComponent', () => {
    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [MemoryDetailPageComponent],
            providers: [
                provideZonelessChangeDetection(),
                {
                    provide: MemoryService,
                    useValue: {
                        getMemoryById: () => of({
                            id: 'm-1',
                            content: 'hello',
                            level: 'thread',
                            type: 'Context',
                            starred: false,
                            created_at: '2026-05-01T00:00:00Z',
                            updated_at: '2026-05-01T00:00:00Z',
                        }),
                        patchMemory: () => of({
                            id: 'm-1',
                            content: 'hello',
                            level: 'thread',
                            type: 'Context',
                            starred: false,
                            created_at: '2026-05-01T00:00:00Z',
                            updated_at: '2026-05-01T00:00:00Z',
                        }),
                        deleteMemory: () => of(void 0),
                        updateMemoryPin: () => of({
                            id: 'm-1',
                            content: 'hello',
                            level: 'thread',
                            type: 'Context',
                            starred: false,
                            created_at: '2026-05-01T00:00:00Z',
                            updated_at: '2026-05-01T00:00:00Z',
                        }),
                    },
                },
                {
                    provide: PersonalityService,
                    useValue: {
                        listPersonalities: () => of({ results: [], total_count: 0, page: 1 }),
                    },
                },
                {
                    provide: ActivatedRoute,
                    useValue: {
                        snapshot: {
                            paramMap: { get: () => 'm-1' },
                        },
                    },
                },
                { provide: Router, useValue: { navigate: vi.fn().mockName('navigate') } },
            ],
        }).compileComponents();
    });

    it('loads memory on init', () => {
        const fixture = TestBed.createComponent(MemoryDetailPageComponent);
        fixture.componentInstance.ngOnInit();
        expect(fixture.componentInstance.memory()?.id).toBe('m-1');
    });
});
