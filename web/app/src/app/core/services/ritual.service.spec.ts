import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { environment } from '@environments/environment';
import { RitualService } from './ritual.service';
import { Ritual } from '../models/ritual.model';

describe('RitualService', () => {
    let service: RitualService;
    let httpMock: HttpTestingController;

    beforeEach(() => {
        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                RitualService
            ]
        });

        service = TestBed.inject(RitualService);
        httpMock = TestBed.inject(HttpTestingController);
    });

    afterEach(() => {
        httpMock.verify();
    });

    it('should be created', () => {
        expect(service).toBeTruthy();
    });

    describe('listSystemRituals', () => {
        const mockRituals: Ritual[] = [
            {
                id: 'ritual-1',
                name: 'Generate image',
                description: 'Generate an image from the conversation.',
                content: '',
                hotkeys: '',
                personality_id: null,
                created_at: '2024-01-01T00:00:00Z',
                updated_at: '2024-01-01T00:00:00Z'
            }
        ];

        it('should return rituals when API returns a plain array', () => {
            service.listSystemRituals().subscribe((rituals) => {
                expect(rituals).toEqual(mockRituals);
            });

            const req = httpMock.expectOne(`${environment.apiUrl}/ritual/system`);
            expect(req.request.method).toBe('GET');
            req.flush(mockRituals);
        });

        it('should unwrap results when API returns a paginated envelope', () => {
            service.listSystemRituals().subscribe((rituals) => {
                expect(rituals).toEqual(mockRituals);
            });

            const req = httpMock.expectOne(`${environment.apiUrl}/ritual/system`);
            expect(req.request.method).toBe('GET');
            req.flush({ results: mockRituals, total_count: 1, page: 1 });
        });
    });

    describe('assignSystemRitualHotkey', () => {
        it('should PUT binding with hotkeys', () => {
            service.assignSystemRitualHotkey('ritual-1', 'ctrl+shift+r').subscribe();

            const req = httpMock.expectOne(`${environment.apiUrl}/ritual/ritual-1/binding`);
            expect(req.request.method).toBe('PUT');
            expect(req.request.body).toEqual({ hotkeys: 'ctrl+shift+r' });
            req.flush({});
        });
    });

    describe('deleteSystemRitualHotkey', () => {
        it('should DELETE binding', () => {
            service.deleteSystemRitualHotkey('ritual-1').subscribe();

            const req = httpMock.expectOne(`${environment.apiUrl}/ritual/ritual-1/binding`);
            expect(req.request.method).toBe('DELETE');
            req.flush(null);
        });
    });

    describe('getSystemRitualBinding', () => {
        it('should GET binding', () => {
            const mockBinding = { hotkeys: 'ctrl+shift+r' };
            service.getSystemRitualBinding('ritual-1').subscribe((res) => {
                expect(res).toEqual(mockBinding);
            });

            const req = httpMock.expectOne(`${environment.apiUrl}/ritual/ritual-1/binding`);
            expect(req.request.method).toBe('GET');
            req.flush(mockBinding);
        });
    });

    describe('getAvailableRituals', () => {
        it('should include has_hotkeys filter when provided', () => {
            service
                .getAvailableRituals('chat-1', 1, 10, { has_hotkeys: true })
                .subscribe();

            const req = httpMock.expectOne((r) => r.url?.includes('/chat/chat-1/available-rituals') &&
                r.params?.get('has_hotkeys') === 'true');
            expect(req.request.method).toBe('GET');
            req.flush({ results: [], total_count: 0, page: 1 });
        });

        it('should not include has_hotkeys when not in filters', () => {
            service.getAvailableRituals('chat-1', 1, 10, {}).subscribe();

            const req = httpMock.expectOne((r) => r.url?.includes('/chat/chat-1/available-rituals'));
            expect(req.request.params.has('has_hotkeys')).toBe(false);
            req.flush({ results: [], total_count: 0, page: 1 });
        });

        it('should include sort when provided', () => {
            service.getAvailableRituals('chat-1', 1, 10, { sort: 'updated_desc' }).subscribe();

            const req = httpMock.expectOne((r) => r.url?.includes('/chat/chat-1/available-rituals') && r.params?.get('sort') === 'updated_desc');
            expect(req.request.method).toBe('GET');
            req.flush({ results: [], total_count: 0, page: 1 });
        });
    });

    describe('listRituals', () => {
        it('should include personality_ids, global_only, and sort when provided', () => {
            service
                .listRituals(1, 24, {
                personality_ids: ['persona-1', 'persona-2'],
                global_only: false,
                sort: 'name_asc',
                search: 'diagram',
            })
                .subscribe();

            const req = httpMock.expectOne((r) => r.url === `${environment.apiUrl}/ritual`);
            expect(req.request.method).toBe('GET');
            expect(req.request.params.getAll('personality_ids')).toEqual(['persona-1', 'persona-2']);
            expect(req.request.params.get('global_only')).toBe('false');
            expect(req.request.params.get('sort')).toBe('name_asc');
            expect(req.request.params.get('search')).toBe('diagram');
            req.flush({ results: [], total_count: 0, page: 1 });
        });
    });
});


