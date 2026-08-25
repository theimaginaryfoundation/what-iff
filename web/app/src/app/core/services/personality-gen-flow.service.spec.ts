import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { environment } from '@environments/environment';
import { PersonalityGenFlowService } from './personality-gen-flow.service';

describe('PersonalityGenFlowService', () => {
    let service: PersonalityGenFlowService;
    let httpMock: HttpTestingController;

    beforeEach(() => {
        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                PersonalityGenFlowService,
            ],
        });

        service = TestBed.inject(PersonalityGenFlowService);
        httpMock = TestBed.inject(HttpTestingController);
    });

    afterEach(() => {
        httpMock.verify();
    });

    it('enqueues complete flow generation as async job', () => {
        service.completeFlow('flow-1').subscribe((resp) => {
            expect(resp.job_id).toBe('job-1');
            expect(resp.job_type).toBe('personality_generation');
        });

        const req = httpMock.expectOne(`${environment.apiUrl}/personality/generate/flow-1/complete`);
        expect(req.request.method).toBe('POST');
        req.flush({ job_id: 'job-1', job_type: 'personality_generation' });
    });

    it('loads a specific flow by id', () => {
        service.getFlow('flow-1').subscribe((flow) => {
            expect(flow.id).toBe('flow-1');
            expect(flow.current_step).toBe(2);
        });

        const req = httpMock.expectOne(`${environment.apiUrl}/personality/generate/flow-1`);
        expect(req.request.method).toBe('GET');
        req.flush({
            id: 'flow-1',
            status: 'in_progress',
            current_step: 2,
            answers: { general_description: 'fox' },
            generated_prompt: '',
            generated_about_me: '',
            generated_names: [],
            image_style: 'auto',
            created_at: '2026-06-05T00:00:00Z',
            updated_at: '2026-06-05T00:00:00Z',
        });
    });

    it('enqueues regenerate flow generation as async job', () => {
        service.regenerateFlow('flow-1').subscribe((resp) => {
            expect(resp.job_id).toBe('job-2');
            expect(resp.job_type).toBe('personality_generation');
        });

        const req = httpMock.expectOne(`${environment.apiUrl}/personality/generate/flow-1/regenerate`);
        expect(req.request.method).toBe('POST');
        req.flush({ job_id: 'job-2', job_type: 'personality_generation' });
    });

    it('returns active generation job when present', () => {
        service.getActiveGenerationJob('flow-1').subscribe((resp) => {
            expect(resp?.job_id).toBe('job-3');
            expect(resp?.job_type).toBe('personality_generation');
        });

        const req = httpMock.expectOne(`${environment.apiUrl}/personality/generate/flow-1/active-job`);
        expect(req.request.method).toBe('GET');
        req.flush({
            job_id: 'job-3',
            job_type: 'personality_generation',
            reference: 'flow-1',
            status: 'processing',
            flow_id: 'flow-1',
        });
    });

    it('throws when active generation job payload shape is invalid', () => {
        let gotError = false;
        service.getActiveGenerationJob('flow-1').subscribe({
            next: () => expect.fail('expected error'),
            error: () => { gotError = true; },
        });

        const req = httpMock.expectOne(`${environment.apiUrl}/personality/generate/flow-1/active-job`);
        expect(req.request.method).toBe('GET');
        req.flush({ message: 'not-a-job' });
        expect(gotError).toBe(true);
    });

    it('resets draft flow and returns a fresh flow', () => {
        service.resetFlow('flow-1').subscribe((flow) => {
            expect(flow.id).toBe('flow-2');
            expect(flow.current_step).toBe(0);
            expect(flow.status).toBe('in_progress');
        });

        const req = httpMock.expectOne(`${environment.apiUrl}/personality/generate/flow-1/reset`);
        expect(req.request.method).toBe('POST');
        req.flush({
            id: 'flow-2',
            status: 'in_progress',
            current_step: 0,
            answers: {},
            generated_prompt: '',
            generated_about_me: '',
            generated_names: [],
            image_style: 'auto',
            created_at: '2026-06-05T00:00:00Z',
            updated_at: '2026-06-05T00:00:00Z',
        });
    });
});
