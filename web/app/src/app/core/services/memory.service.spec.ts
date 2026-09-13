import { HttpErrorResponse, provideHttpClient, withXhr } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { environment } from '@environments/environment';
import { MemoryService } from './memory.service';
import { Memory } from '../models/memory.model';

describe('MemoryService', () => {
    let service: MemoryService;
    let httpMock: HttpTestingController;

    const mockMemory: Memory = {
        id: 'mem-1',
        content: 'Likes long walks',
        level: 'global',
        type: 'Context',
        status: 'active',
        confidence: 0.6,
        starred: false,
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
    };

    beforeEach(() => {
        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                MemoryService,
            ],
        });

        service = TestBed.inject(MemoryService);
        httpMock = TestBed.inject(HttpTestingController);
    });

    afterEach(() => {
        httpMock.verify();
    });

    it('should be created', () => {
        expect(service).toBeTruthy();
    });

    describe('getMemories', () => {
        it('sends default page and limit with no filters', () => {
            service.getMemories().subscribe();

            const req = httpMock.expectOne(r => r.url === `${environment.apiUrl}/memory`);
            expect(req.request.method).toBe('GET');
            expect(req.request.params.get('page')).toBe('1');
            expect(req.request.params.get('limit')).toBe('10');
            for (const key of [
                'chat_id', 'level', 'type', 'starred', 'pinned_personality_id',
                'personality_ids', 'global_only', 'query', 'status', 'sort',
                'min_date', 'max_date',
            ]) {
                expect(req.request.params.has(key)).toBe(false);
            }
            req.flush({ results: [], total_count: 0, page: 1 });
        });

        it('forwards a custom page and limit', () => {
            service.getMemories(3, 50).subscribe();

            const req = httpMock.expectOne(r => r.url === `${environment.apiUrl}/memory`);
            expect(req.request.params.get('page')).toBe('3');
            expect(req.request.params.get('limit')).toBe('50');
            req.flush({ results: [], total_count: 0, page: 3 });
        });

        it('sends starred=true when filter is true', () => {
            service.getMemories(1, 10, { starred: true }).subscribe();

            const req = httpMock.expectOne(r => r.url === `${environment.apiUrl}/memory`);
            expect(req.request.params.get('starred')).toBe('true');
            req.flush({ results: [], total_count: 0, page: 1 });
        });

        it('sends starred=false when filter is explicitly false', () => {
            service.getMemories(1, 10, { starred: false }).subscribe();

            const req = httpMock.expectOne(r => r.url === `${environment.apiUrl}/memory`);
            expect(req.request.params.get('starred')).toBe('false');
            req.flush({ results: [], total_count: 0, page: 1 });
        });

        it('sends global_only=true when filter is truthy', () => {
            service.getMemories(1, 10, { global_only: true }).subscribe();

            const req = httpMock.expectOne(r => r.url === `${environment.apiUrl}/memory`);
            expect(req.request.params.get('global_only')).toBe('true');
            req.flush({ results: [], total_count: 0, page: 1 });
        });

        it('omits global_only when filter is explicitly false', () => {
            service.getMemories(1, 10, { global_only: false }).subscribe();

            const req = httpMock.expectOne(r => r.url === `${environment.apiUrl}/memory`);
            expect(req.request.params.has('global_only')).toBe(false);
            req.flush({ results: [], total_count: 0, page: 1 });
        });

        it('appends trimmed, non-blank pinned_personality_ids as repeated personality_ids params', () => {
            service.getMemories(1, 10, { pinned_personality_ids: [' p-1 ', '', '  ', 'p-2'] }).subscribe();

            const req = httpMock.expectOne(r => r.url === `${environment.apiUrl}/memory`);
            expect(req.request.params.getAll('personality_ids')).toEqual(['p-1', 'p-2']);
            req.flush({ results: [], total_count: 0, page: 1 });
        });

        it('sends chat_id, level, type, pinned_personality_id, query, status, sort, min_date, max_date', () => {
            service.getMemories(1, 10, {
                chat_id: 'chat-1',
                level: 'personality',
                type: 'Context',
                pinned_personality_id: 'persona-1',
                query: 'coffee',
                status: 'inactive',
                sort: 'updated_desc',
                min_date: '2026-01-01',
                max_date: '2026-02-01',
            }).subscribe();

            const req = httpMock.expectOne(r => r.url === `${environment.apiUrl}/memory`);
            expect(req.request.params.get('chat_id')).toBe('chat-1');
            expect(req.request.params.get('level')).toBe('personality');
            expect(req.request.params.get('type')).toBe('Context');
            expect(req.request.params.get('pinned_personality_id')).toBe('persona-1');
            expect(req.request.params.get('query')).toBe('coffee');
            expect(req.request.params.get('status')).toBe('inactive');
            expect(req.request.params.get('sort')).toBe('updated_desc');
            expect(req.request.params.get('min_date')).toBe('2026-01-01');
            expect(req.request.params.get('max_date')).toBe('2026-02-01');
            req.flush({ results: [], total_count: 0, page: 1 });
        });

        it('returns the paginated response body', () => {
            let received: unknown;
            service.getMemories().subscribe(resp => (received = resp));

            const req = httpMock.expectOne(r => r.url === `${environment.apiUrl}/memory`);
            req.flush({ results: [mockMemory], total_count: 1, page: 1 });
            expect(received).toEqual({ results: [mockMemory], total_count: 1, page: 1 });
        });
    });

    describe('getMemoryById', () => {
        it('GETs the memory by id', () => {
            let received: Memory | undefined;
            service.getMemoryById('mem-1').subscribe(m => (received = m));

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/mem-1`);
            expect(req.request.method).toBe('GET');
            req.flush(mockMemory);
            expect(received).toEqual(mockMemory);
        });
    });

    describe('createMemory', () => {
        it('POSTs the payload verbatim', () => {
            const payload = { content: 'New memory', level: 'global' as const };
            service.createMemory(payload).subscribe();

            const req = httpMock.expectOne(`${environment.apiUrl}/memory`);
            expect(req.request.method).toBe('POST');
            expect(req.request.body).toEqual(payload);
            req.flush(mockMemory);
        });
    });

    describe('createMemoriesBatch', () => {
        it('POSTs items and all_or_none to /memory/batch', () => {
            const payload = { items: [{ content: 'a', level: 'global' as const }], all_or_none: true };
            let received: unknown;
            service.createMemoriesBatch(payload).subscribe(resp => (received = resp));

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/batch`);
            expect(req.request.method).toBe('POST');
            expect(req.request.body).toEqual(payload);
            req.flush({ results: [mockMemory], created_count: 1 });
            expect(received).toEqual({ results: [mockMemory], created_count: 1 });
        });

        it('POSTs without all_or_none when omitted', () => {
            const payload = { items: [{ content: 'a', level: 'global' as const }] };
            service.createMemoriesBatch(payload).subscribe();

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/batch`);
            expect(req.request.body).toEqual(payload);
            expect(req.request.body.all_or_none).toBeUndefined();
            req.flush({ results: [], created_count: 0 });
        });

        it('propagates the envelope message on failure', async () => {
            let captured: Error | undefined;
            service.createMemoriesBatch({ items: [] }).subscribe({ error: err => (captured = err) });

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/batch`);
            req.flush({ message: 'items must not be empty' }, new HttpErrorResponse({ status: 400, statusText: 'Bad Request' }));

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(captured?.message).toBe('items must not be empty');
        });
    });

    describe('deleteMemoriesBatch', () => {
        it('POSTs ids and all_or_none to /memory/batch/delete', () => {
            const payload = { ids: ['mem-1', 'mem-2'], all_or_none: true };
            let received: unknown;
            service.deleteMemoriesBatch(payload).subscribe(resp => (received = resp));

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/batch/delete`);
            expect(req.request.method).toBe('POST');
            expect(req.request.body).toEqual(payload);
            req.flush({ deleted_count: 2 });
            expect(received).toEqual({ deleted_count: 2 });
        });

        it('POSTs without all_or_none when omitted', () => {
            service.deleteMemoriesBatch({ ids: ['mem-1'] }).subscribe();

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/batch/delete`);
            expect(req.request.body).toEqual({ ids: ['mem-1'] });
            expect(req.request.body.all_or_none).toBeUndefined();
            req.flush({ deleted_count: 1 });
        });

        it('propagates the envelope message on failure', async () => {
            let captured: Error | undefined;
            service.deleteMemoriesBatch({ ids: ['mem-1'], all_or_none: true }).subscribe({
                error: err => (captured = err),
            });

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/batch/delete`);
            req.flush({ message: 'one or more ids not found' }, new HttpErrorResponse({ status: 404, statusText: 'Not Found' }));

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(captured?.message).toBe('one or more ids not found');
        });
    });

    describe('patchMemoriesBatch', () => {
        it('POSTs ids, patch, and all_or_none to /memory/batch/patch', () => {
            const payload = { ids: ['mem-1', 'mem-2'], patch: { starred: true }, all_or_none: true };
            let received: unknown;
            service.patchMemoriesBatch(payload).subscribe(resp => (received = resp));

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/batch/patch`);
            expect(req.request.method).toBe('POST');
            expect(req.request.body).toEqual(payload);
            req.flush({ results: [mockMemory], updated_count: 1 });
            expect(received).toEqual({ results: [mockMemory], updated_count: 1 });
        });

        it('POSTs without all_or_none when omitted', () => {
            service.patchMemoriesBatch({ ids: ['mem-1'], patch: { status: 'inactive' } }).subscribe();

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/batch/patch`);
            expect(req.request.body).toEqual({ ids: ['mem-1'], patch: { status: 'inactive' } });
            expect(req.request.body.all_or_none).toBeUndefined();
            req.flush({ results: [], updated_count: 0 });
        });

        it('propagates the envelope message on failure', async () => {
            let captured: Error | undefined;
            service.patchMemoriesBatch({ ids: ['mem-1'], patch: { starred: true }, all_or_none: true }).subscribe({
                error: err => (captured = err),
            });

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/batch/patch`);
            req.flush({ message: 'invalid patch field' }, new HttpErrorResponse({ status: 422, statusText: 'Unprocessable Entity' }));

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(captured?.message).toBe('invalid patch field');
        });
    });

    describe('patchMemory', () => {
        it('PATCHes the payload verbatim', () => {
            const payload = { content: 'Updated', starred: true };
            service.patchMemory('mem-1', payload).subscribe();

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/mem-1`);
            expect(req.request.method).toBe('PATCH');
            expect(req.request.body).toEqual(payload);
            req.flush(mockMemory);
        });
    });

    describe('deleteMemory', () => {
        it('DELETEs the memory by id', () => {
            service.deleteMemory('mem-1').subscribe();

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/mem-1`);
            expect(req.request.method).toBe('DELETE');
            req.flush(null);
        });
    });

    describe('updateMemoryPin', () => {
        it('PUTs a string pinned_personality_id', () => {
            service.updateMemoryPin('mem-1', 'persona-1').subscribe();

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/mem-1/pin`);
            expect(req.request.method).toBe('PUT');
            expect(req.request.body).toEqual({ pinned_personality_id: 'persona-1' });
            req.flush(mockMemory);
        });

        it('PUTs a null pinned_personality_id to unpin', () => {
            service.updateMemoryPin('mem-1', null).subscribe();

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/mem-1/pin`);
            expect(req.request.body).toEqual({ pinned_personality_id: null });
            req.flush(mockMemory);
        });
    });

    describe('exportMemories', () => {
        it('GETs the export endpoint as a blob', () => {
            let received: Blob | undefined;
            service.exportMemories().subscribe(blob => (received = blob));

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/export`);
            expect(req.request.method).toBe('GET');
            expect(req.request.responseType).toBe('blob');
            const blob = new Blob(['zip-bytes']);
            req.flush(blob);
            expect(received).toBe(blob);
        });
    });

    describe('importMemories', () => {
        it('POSTs the file as multipart form data under the "file" key', () => {
            const file = new File(['content'], 'export.zip');
            service.importMemories(file).subscribe();

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/import`);
            expect(req.request.method).toBe('POST');
            expect(req.request.body instanceof FormData).toBe(true);
            expect((req.request.body as FormData).get('file')).toBe(file);
            req.flush({
                imported_count: 1,
                duplicate_count: 0,
                invalid_record_count: 0,
                skipped_missing_chat_count: 0,
                skipped_missing_personality_count: 0,
            });
        });
    });

    describe('listMergeEvents', () => {
        it('sends default page and limit', () => {
            service.listMergeEvents().subscribe();

            const req = httpMock.expectOne(r => r.url === `${environment.apiUrl}/memory/merge-events`);
            expect(req.request.method).toBe('GET');
            expect(req.request.params.get('page')).toBe('1');
            expect(req.request.params.get('limit')).toBe('20');
            req.flush({ results: [], total_count: 0, page: 1 });
        });

        it('forwards a custom page and limit', () => {
            service.listMergeEvents(2, 5).subscribe();

            const req = httpMock.expectOne(r => r.url === `${environment.apiUrl}/memory/merge-events`);
            expect(req.request.params.get('page')).toBe('2');
            expect(req.request.params.get('limit')).toBe('5');
            req.flush({ results: [], total_count: 0, page: 2 });
        });
    });

    describe('undoMergeEvent', () => {
        it('POSTs an empty body to the undo endpoint', () => {
            service.undoMergeEvent('merge-1').subscribe();

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/merge-events/merge-1/undo`);
            expect(req.request.method).toBe('POST');
            expect(req.request.body).toEqual({});
            req.flush({
                id: 'merge-1',
                survivor_memory_id: 'mem-1',
                merge_type: 'fold_live',
                content: 'merged',
                duplicates_folded: 1,
                created_at: '2026-01-01T00:00:00Z',
                updated_at: '2026-01-01T00:00:00Z',
            });
        });
    });

    describe('listCompactionEvents', () => {
        it('sends default page and limit with no optional filters', () => {
            service.listCompactionEvents().subscribe();

            const req = httpMock.expectOne(r => r.url === `${environment.apiUrl}/memory/compaction-events`);
            expect(req.request.params.get('page')).toBe('1');
            expect(req.request.params.get('limit')).toBe('20');
            expect(req.request.params.has('chat_id')).toBe(false);
            expect(req.request.params.has('personality_id')).toBe(false);
            req.flush({ results: [], total_count: 0, page: 1 });
        });

        it('forwards chat_id and personality_id filters when provided', () => {
            service.listCompactionEvents(1, 20, { chat_id: 'chat-1', personality_id: 'persona-1' }).subscribe();

            const req = httpMock.expectOne(r => r.url === `${environment.apiUrl}/memory/compaction-events`);
            expect(req.request.params.get('chat_id')).toBe('chat-1');
            expect(req.request.params.get('personality_id')).toBe('persona-1');
            req.flush({ results: [], total_count: 0, page: 1 });
        });
    });

    describe('getCompactionEvent', () => {
        it('GETs the compaction event by id', () => {
            service.getCompactionEvent('evt-1').subscribe();

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/compaction-events/evt-1`);
            expect(req.request.method).toBe('GET');
            req.flush({
                id: 'evt-1',
                chat_id: 'chat-1',
                created_at: '2026-01-01T00:00:00Z',
                updated_at: '2026-01-01T00:00:00Z',
            });
        });
    });

    describe('revertSnapshot', () => {
        it('POSTs an empty body to the revert endpoint', () => {
            service.revertSnapshot('snap-1').subscribe();

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/snapshots/snap-1/revert`);
            expect(req.request.method).toBe('POST');
            expect(req.request.body).toEqual({});
            req.flush({
                id: 'snap-1',
                kind: 'summary',
                content: 'summary text',
                created_at: '2026-01-01T00:00:00Z',
            });
        });
    });

    describe('error handling', () => {
        it('falls back to the default message when the response has no envelope', async () => {
            let captured: Error | undefined;
            service.getMemoryById('mem-1').subscribe({ error: err => (captured = err) });

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/mem-1`);
            req.error(new ProgressEvent('error'), { status: 0, statusText: 'Unknown Error' });

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(captured?.message).toBe('An unexpected error occurred');
        });

        it('falls back to envelope "error" field when "message" is absent', async () => {
            let captured: Error | undefined;
            service.getMemoryById('mem-1').subscribe({ error: err => (captured = err) });

            const req = httpMock.expectOne(`${environment.apiUrl}/memory/mem-1`);
            req.flush({ error: 'not found' }, new HttpErrorResponse({ status: 404, statusText: 'Not Found' }));

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(captured?.message).toBe('not found');
        });
    });
});
