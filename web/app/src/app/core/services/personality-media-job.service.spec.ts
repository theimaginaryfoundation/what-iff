import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';

import { PersonalityMediaJobService } from './personality-media-job.service';
import { JobService } from './job.service';
import { ActivePersonalityMediaJob, PersonalityMediaJobResponse } from '../models/personality-media-job.model';
import { environment } from '../../../environments/environment';

const API = `${environment.apiUrl}/personality`;

describe('PersonalityMediaJobService.startExpressionCandidates', () => {
    let service: PersonalityMediaJobService;
    let http: HttpTestingController;
    const keys = ['happy', 'sad'];
    const enqueued: PersonalityMediaJobResponse = { job_id: 'job-1', job_type: 'expression_grid' } as PersonalityMediaJobResponse;
    const active: ActivePersonalityMediaJob = {
        job_id: 'job-1', job_type: 'expression_grid', reference: 'p-1', status: 'queued',
        personality_id: 'p-1', expression_mode: 'candidates',
    } as ActivePersonalityMediaJob;

    beforeEach(() => {
        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                { provide: JobService, useValue: { getJob: vi.fn() } },
            ],
        });
        service = TestBed.inject(PersonalityMediaJobService);
        http = TestBed.inject(HttpTestingController);
        vi.spyOn(console, 'error').mockImplementation(() => {});
    });

    afterEach(() => http.verify());

    function latestActive(): ActivePersonalityMediaJob | null {
        let value: ActivePersonalityMediaJob | null = null;
        service.activeJob$.subscribe(v => { value = v; }).unsubscribe();
        return value;
    }

    it('posts keys and reference, then refreshes the active job', () => {
        let res: PersonalityMediaJobResponse | undefined;
        service.startExpressionCandidates('p-1', keys, 'ref-1').subscribe(r => { res = r; });

        const post = http.expectOne(`${API}/p-1/expressions/generate-candidates`);
        expect(post.request.method).toBe('POST');
        expect(post.request.body).toEqual({ expressions: keys, reference_image_id: 'ref-1' });
        post.flush(enqueued);

        http.expectOne(`${API}/active-media-job`).flush(active);
        expect(res).toEqual(enqueued);
        expect(latestActive()).toEqual(active);
    });

    it('sends a null reference and still resolves when the refresh fails', () => {
        let res: PersonalityMediaJobResponse | undefined;
        service.startExpressionCandidates('p-1', keys, null).subscribe(r => { res = r; });

        const post = http.expectOne(`${API}/p-1/expressions/generate-candidates`);
        expect(post.request.body).toEqual({ expressions: keys, reference_image_id: null });
        post.flush(enqueued);

        http.expectOne(`${API}/active-media-job`).flush('err', { status: 500, statusText: 'Server Error' });
        expect(res).toEqual(enqueued);
    });

    it('records the conflicting active job on 409 and rethrows', () => {
        let status: number | undefined;
        service.startExpressionCandidates('p-1', keys, null).subscribe({ error: e => { status = e.status; } });

        http.expectOne(`${API}/p-1/expressions/generate-candidates`)
            .flush({ message: 'busy', active }, { status: 409, statusText: 'Conflict' });
        expect(status).toBe(409);
        expect(latestActive()).toEqual(active);
    });

    it('rethrows non-conflict errors without touching the active job', () => {
        let status: number | undefined;
        service.startExpressionCandidates('p-1', keys, 'missing').subscribe({ error: e => { status = e.status; } });

        http.expectOne(`${API}/p-1/expressions/generate-candidates`)
            .flush({ message: 'reference not found' }, { status: 404, statusText: 'Not Found' });
        expect(status).toBe(404);
        expect(latestActive()).toBeNull();
    });
});
