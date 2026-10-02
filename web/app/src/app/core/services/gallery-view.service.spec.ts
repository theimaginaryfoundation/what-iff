import type { MockedObject } from "vitest";
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { of, Subject, throwError } from 'rxjs';

import { PaginatedResponse } from '../models/common.model';

import { FileAttachment } from '../models/file-attachment.model';
import { ImageGalleryService } from './image-gallery.service';
import { GalleryViewService } from './gallery-view.service';

function makeAttachment(partial: Partial<FileAttachment> = {}): FileAttachment {
    return {
        id: 'img-1',
        user_id: 'user-1',
        name: 'Sunrise',
        file_type: 'image/png',
        created_at: '2026-05-01T00:00:00Z',
        ...partial,
    };
}

describe('GalleryViewService', () => {
    let service: GalleryViewService;
    let galleryApi: Pick<MockedObject<ImageGalleryService>, 'listImages'>;

    beforeEach(() => {
        galleryApi = {
            listImages: vi.fn().mockName("ImageGalleryService.listImages")
        } as unknown as Pick<MockedObject<ImageGalleryService>, 'listImages'>;
        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                GalleryViewService,
                { provide: ImageGalleryService, useValue: galleryApi },
            ],
        });
        service = TestBed.inject(GalleryViewService);
    });

    it('loads first page and sets rows', () => {
        galleryApi.listImages.mockReturnValue(of({
            results: [makeAttachment({ id: 'a' })],
            total_count: 1,
            page: 1,
        }));

        service.loadInitial();

        expect(galleryApi.listImages).toHaveBeenCalledWith(1, service.pageSize, { name: '', personalityId: undefined, globalOnly: false });
        expect(service.images().length).toBe(1);
        expect(service.totalCount()).toBe(1);
    });

    it('resets pagination when filters change', () => {
        galleryApi.listImages.mockReturnValue(of({
            results: [makeAttachment({ id: 'a' })],
            total_count: 1,
            page: 1,
        }));

        service.setFilters({ query: 'sun' });

        expect(service.filters().query).toBe('sun');
        expect(galleryApi.listImages).toHaveBeenCalledWith(1, service.pageSize, { name: 'sun', personalityId: undefined, globalOnly: false });
    });

    it('uses global_only API filter when global mode is selected', () => {
        galleryApi.listImages.mockReturnValue(of({
            results: [makeAttachment({ id: 'a' })],
            total_count: 1,
            page: 1,
        }));

        service.selectGlobalAssociations();

        expect(galleryApi.listImages).toHaveBeenCalledWith(1, service.pageSize, { name: '', personalityId: undefined, globalOnly: true });
        expect(service.associationFilterMode()).toBe('global');
    });

    it('handles load failure with an error message', () => {
        galleryApi.listImages.mockReturnValue(throwError(() => new Error('nope')));

        service.loadInitial();

        expect(service.error()).toBe('Failed to load gallery images.');
    });
    describe('source filter consistency (#141)', () => {
        const generated = (id: string) => makeAttachment({ id, source: 'generated', chat_message_id: `msg-${id}` });
        const imported = (id: string) => makeAttachment({ id, source: 'imported', chat_message_id: `umsg-${id}` });
        const page = (results: FileAttachment[], total: number, n = 1): PaginatedResponse<FileAttachment> =>
            ({ results, total_count: total, page: n }) as PaginatedResponse<FileAttachment>;
        const ids = () => service.filteredImages().map(row => row.id);

        /** Each listImages call returns its own Subject so tests control response order. */
        function deferredResponses(): Subject<PaginatedResponse<FileAttachment>>[] {
            const pending: Subject<PaginatedResponse<FileAttachment>>[] = [];
            galleryApi.listImages.mockImplementation(() => {
                const subject = new Subject<PaginatedResponse<FileAttachment>>();
                pending.push(subject);
                return subject.asObservable();
            });
            return pending;
        }

        function respond(subject: Subject<PaginatedResponse<FileAttachment>>, body: PaginatedResponse<FileAttachment>): void {
            subject.next(body);
            subject.complete();
        }

        it('shows only matching rows in both toggle directions and back to all', () => {
            galleryApi.listImages.mockReturnValue(of(page([generated('g1'), imported('i1'), generated('g2'), imported('i2')], 4)));
            service.loadInitial();

            service.setFilters({ source: 'generated' });
            expect(ids()).toEqual(['g1', 'g2']);
            service.setFilters({ source: 'uploaded' });
            expect(ids()).toEqual(['i1', 'i2']);
            service.setFilters({ source: 'generated' });
            expect(ids()).toEqual(['g1', 'g2']);
            service.setFilters({ source: 'all' });
            expect(ids()).toEqual(['g1', 'i1', 'g2', 'i2']);
        });

        it('returns an empty set when nothing matches', () => {
            galleryApi.listImages.mockReturnValue(of(page([generated('g1')], 1)));
            service.loadInitial();

            service.setFilters({ source: 'uploaded' });
            expect(ids()).toEqual([]);
        });

        it('does not refetch or drop loaded pages when only the client-side source filter changes', () => {
            galleryApi.listImages
                .mockReturnValueOnce(of(page([generated('g1'), imported('i1')], 4, 1)))
                .mockReturnValueOnce(of(page([generated('g2'), imported('i2')], 4, 2)));
            service.loadInitial();
            service.loadNextPage();
            expect(galleryApi.listImages).toHaveBeenCalledTimes(2);

            for (let i = 0; i < 5; i++) {
                service.setFilters({ source: 'uploaded' });
                expect(ids()).toEqual(['i1', 'i2']);
                service.setFilters({ source: 'generated' });
                expect(ids()).toEqual(['g1', 'g2']);
            }
            expect(galleryApi.listImages).toHaveBeenCalledTimes(2);
            expect(service.currentPage()).toBe(2);
        });

        it('still reloads from page 1 when a server-side filter changes', () => {
            galleryApi.listImages.mockReturnValue(of(page([generated('g1')], 1)));
            service.loadInitial();
            service.setFilters({ source: 'generated' });
            service.setFilters({ query: 'sun' });
            expect(galleryApi.listImages).toHaveBeenLastCalledWith(1, service.pageSize, { name: 'sun', personalityId: undefined, globalOnly: false });
            expect(service.filters().source).toBe('generated');
        });

        it('drops a stale load-more page that lands after a reload', () => {
            const pending = deferredResponses();
            service.loadInitial();
            respond(pending[0], page([generated('g1'), imported('i1')], 4, 1));
            service.loadNextPage();                 // pending[1]: page 2 of the old result set
            service.selectGlobalAssociations();     // pending[2]: new page 1 (server filter changed)

            respond(pending[2], page([imported('i9')], 1, 1));
            respond(pending[1], page([generated('g2'), imported('i2')], 4, 2));

            expect(service.images().map(row => row.id)).toEqual(['i9']);
            expect(service.currentPage()).toBe(1);
            expect(service.totalCount()).toBe(1);
            expect(service.isLoadingMore()).toBe(false);
        });

        it('does not skip a page when the stale load-more page lands before the reload', () => {
            const pending = deferredResponses();
            service.loadInitial();
            respond(pending[0], page([generated('g1'), imported('i1')], 4, 1));
            service.loadNextPage();                 // pending[1]
            service.refresh();                      // pending[2]

            respond(pending[1], page([generated('g2'), imported('i2')], 4, 2));
            respond(pending[2], page([generated('g1'), imported('i1')], 4, 1));
            expect(service.currentPage()).toBe(1);

            service.loadNextPage();
            expect(galleryApi.listImages).toHaveBeenLastCalledWith(2, service.pageSize, { name: '', personalityId: undefined, globalOnly: false });
        });

        it('keeps the newest reload when overlapping reloads resolve out of order', () => {
            const pending = deferredResponses();
            service.setFilters({ query: 'a' });     // pending[0]
            service.setFilters({ query: 'ab' });    // pending[1]

            respond(pending[1], page([imported('i-ab')], 1));
            respond(pending[0], page([generated('g-a')], 1));

            expect(service.images().map(row => row.id)).toEqual(['i-ab']);
            expect(service.isLoading()).toBe(false);
        });
    });
});
