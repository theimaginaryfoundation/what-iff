import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { environment } from '@environments/environment';
import { Personality } from '../models/personality.model';
import { PersonalityService } from './personality.service';

describe('PersonalityService', () => {
    let service: PersonalityService;
    let httpMock: HttpTestingController;

    beforeEach(() => {
        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                PersonalityService,
            ],
        });

        service = TestBed.inject(PersonalityService);
        httpMock = TestBed.inject(HttpTestingController);
    });

    afterEach(() => {
        httpMock.verify();
    });

    it('serializes personality_ids filters as comma-separated personality_ids', () => {
        const personalities: Personality[] = [
            {
                id: 'personality-1',
                name: 'Vera',
                system_prompt: 'Be mysterious.',
                auto_pin_memories: false,
                expressions_enabled: true,
                image_style: 'auto', cover_image_id: null,
                cover_image_url: null,
                created_at: '2026-04-28T00:00:00Z',
                updated_at: '2026-04-28T00:00:00Z',
                stats: {
                    chat_count: 2,
                    last_used_at: '2026-04-28T12:00:00Z',
                },
            },
        ];

        service
            .listPersonalities(2, 25, {
            personality_ids: ['personality-1', 'personality-2'],
        })
            .subscribe((response) => {
            expect(response.results).toEqual(personalities);
        });

        const req = httpMock.expectOne(`${environment.apiUrl}/personality?page=2&limit=25&personality_ids=personality-1,personality-2`);
        expect(req.request.method).toBe('GET');
        req.flush({ results: personalities, total_count: 1, page: 2 });
    });

    it('lists personality expressions', () => {
        service.listExpressions('personality-1').subscribe((expressions) => {
            expect(expressions).toEqual([
                {
                    expression_key: 'happy',
                    label: 'Happy',
                    image_id: 'image-1',
                    image_url: '/api/image-gallery/image-1?size=full',
                    created_at: '2026-04-28T00:00:00Z',
                    updated_at: '2026-04-28T00:00:00Z',
                },
            ]);
        });

        const req = httpMock.expectOne(`${environment.apiUrl}/personality/personality-1/expressions`);
        expect(req.request.method).toBe('GET');
        req.flush([
            {
                expression_key: 'happy',
                label: 'Happy',
                image_id: 'image-1',
                image_url: '/api/image-gallery/image-1?size=full',
                created_at: '2026-04-28T00:00:00Z',
                updated_at: '2026-04-28T00:00:00Z',
            },
        ]);
    });

    it('upserts personality expressions', () => {
        service
            .upsertExpression('personality-1', 'happy', {
            image_id: 'image-1',
            label: 'Happy',
        })
            .subscribe();

        const req = httpMock.expectOne(`${environment.apiUrl}/personality/personality-1/expressions/happy`);
        expect(req.request.method).toBe('PUT');
        expect(req.request.body).toEqual({
            image_id: 'image-1',
            label: 'Happy',
        });
        req.flush({
            expression_key: 'happy',
            label: 'Happy',
            image_id: 'image-1',
            image_url: '/api/image-gallery/image-1?size=full',
            created_at: '2026-04-28T00:00:00Z',
            updated_at: '2026-04-28T00:00:00Z',
        });
    });

    it('deletes personality expressions', () => {
        service.deleteExpression('personality-1', 'happy').subscribe();

        const req = httpMock.expectOne(`${environment.apiUrl}/personality/personality-1/expressions/happy`);
        expect(req.request.method).toBe('DELETE');
        req.flush(null);
    });
});
