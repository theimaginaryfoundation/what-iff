import type { MockedObject } from "vitest";
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { of, throwError } from 'rxjs';

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
});
