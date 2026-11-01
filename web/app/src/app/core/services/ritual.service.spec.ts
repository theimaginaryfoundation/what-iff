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

    describe('import and export', () => {
        it('posts the bundle as multipart with the personality when given', () => {
            const file = new File(['---\nname: x\n---'], 'x.zip', { type: 'application/zip' });
            service.importBundle(file, 'personality-1').subscribe();

            const req = httpMock.expectOne(`${environment.apiUrl}/ritual/import`);
            expect(req.request.method).toBe('POST');
            const body = req.request.body as FormData;
            expect(body.get('file')).toBeInstanceOf(File);
            expect(body.get('personality_id')).toBe('personality-1');
            req.flush({ ritual: {}, warnings: [], files_imported: 0, connectors_linked: [], connectors_missing: [] });
        });

        it('omits personality_id when none is given', () => {
            service.importBundle(new File(['x'], 'SKILL.md')).subscribe();
            const req = httpMock.expectOne(`${environment.apiUrl}/ritual/import`);
            expect((req.request.body as FormData).has('personality_id')).toBe(false);
            req.flush({ ritual: {}, warnings: [], files_imported: 0, connectors_linked: [], connectors_missing: [] });
        });

        it('imports into an existing skill when asked', () => {
            service.importBundle(new File(['x'], 'SKILL.md'), null, 'ritual-9').subscribe();
            const req = httpMock.expectOne(`${environment.apiUrl}/ritual/import`);
            expect((req.request.body as FormData).get('ritual_id')).toBe('ritual-9');
            req.flush({ ritual: {}, warnings: [], files_imported: 2, connectors_linked: [], connectors_missing: [] });
        });

        it('lists, uploads and deletes bundle files', () => {
            service.listSkillFiles('ritual-1').subscribe();
            httpMock.expectOne(`${environment.apiUrl}/ritual/ritual-1/files`).flush([]);

            service.uploadSkillFile('ritual-1', new File(['ref'], 'FORMS.md'), 'references/FORMS.md').subscribe();
            const upload = httpMock.expectOne(`${environment.apiUrl}/ritual/ritual-1/files`);
            expect(upload.request.method).toBe('POST');
            expect((upload.request.body as FormData).get('path')).toBe('references/FORMS.md');
            upload.flush({ id: 'f1', path: 'references/FORMS.md', file_type: 'text/markdown', folder: 'skills/x/references', name: 'FORMS.md', created_at: '' });

            service.deleteSkillFile('ritual-1', 'f1').subscribe();
            const del = httpMock.expectOne(`${environment.apiUrl}/ritual/ritual-1/files/f1`);
            expect(del.request.method).toBe('DELETE');
            del.flush(null);
        });

        it('fetches the export as a blob response', () => {
            service.exportRitual('ritual-1').subscribe(res => {
                expect(res.headers.get('Content-Disposition')).toContain('pdf-forms.zip');
            });
            const req = httpMock.expectOne(`${environment.apiUrl}/ritual/ritual-1/export`);
            expect(req.request.method).toBe('GET');
            expect(req.request.responseType).toBe('blob');
            req.flush(new Blob(['zip']), { headers: { 'Content-Disposition': 'attachment; filename="pdf-forms.zip"' } });
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


