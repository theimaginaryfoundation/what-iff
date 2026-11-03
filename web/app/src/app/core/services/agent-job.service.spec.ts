import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { environment } from '@environments/environment';
import { AgentJobService, runNowErrorMessage } from './agent-job.service';
import { HttpErrorResponse } from '@angular/common/http';

describe('AgentJobService', () => {
    let service: AgentJobService;
    let httpMock: HttpTestingController;

    beforeEach(() => {
        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                AgentJobService
            ]
        });

        service = TestBed.inject(AgentJobService);
        httpMock = TestBed.inject(HttpTestingController);
    });

    afterEach(() => {
        httpMock.verify();
    });

    it('should be created', () => {
        expect(service).toBeTruthy();
    });

    it('should POST run-now request for agent job', () => {
        service.runNow('job-1').subscribe((resp) => {
            expect(resp.status).toBe('triggered');
        });

        const req = httpMock.expectOne(`${environment.apiUrl}/agent-job/job-1/run`);
        expect(req.request.method).toBe('POST');
        expect(req.request.body).toEqual({});
        req.flush({ status: 'triggered' });
    });

    // Issue #427: a rate-limited run names the limit and when to retry, never the
    // inactive-scheduler message.
    it('reports a rate-limited run with its reset time', () => {
        const retryAt = '2026-11-03T15:05:00Z';
        let message = '';
        service.runNow('job-1').subscribe({ error: (err: Error) => (message = err.message) });

        httpMock.expectOne(`${environment.apiUrl}/agent-job/job-1/run`).flush(
            {
                message: 'Job run limit reached (10 runs per 30 minutes). Try again at 15:05 UTC.',
                code: 'job_run_limit_reached',
                details: { limit: 10, window_seconds: 1800, retry_at: retryAt },
            },
            { status: 429, statusText: 'Too Many Requests' },
        );

        const time = new Date(retryAt).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
        expect(message).toBe(`Job run limit reached — try again at ${time}.`);
        expect(message).not.toContain('not active');
    });

    it('keeps the inactive-scheduler message distinct', () => {
        let message = '';
        service.runNow('job-1').subscribe({ error: (err: Error) => (message = err.message) });

        httpMock.expectOne(`${environment.apiUrl}/agent-job/job-1/run`).flush(
            { message: 'AgentJob scheduler is not active', code: 'service_unavailable' },
            { status: 503, statusText: 'Service Unavailable' },
        );

        expect(message).toBe('AgentJob scheduler is not active');
    });
});

describe('runNowErrorMessage', () => {
    const limitError = (details: unknown) =>
        new HttpErrorResponse({
            status: 429,
            error: { message: 'Job run limit reached (10 runs per 30 minutes). Try again later.', code: 'job_run_limit_reached', details },
        });

    it('formats retry_at in the given locale', () => {
        expect(runNowErrorMessage(limitError({ retry_at: '2026-11-03T15:05:00Z' }), 'en-US')).toMatch(
            /^Job run limit reached — try again at \d{1,2}:05\s?[AP]M\.$/,
        );
    });

    it('falls back to the server message without a usable reset time', () => {
        expect(runNowErrorMessage(limitError({ limit: 10 }))).toBe('Job run limit reached (10 runs per 30 minutes). Try again later.');
        expect(runNowErrorMessage(limitError({ retry_at: 'not a date' }))).toBe('Job run limit reached (10 runs per 30 minutes). Try again later.');
    });

    it('uses the fallback when there is no envelope', () => {
        expect(runNowErrorMessage(new HttpErrorResponse({ status: 0, error: new ProgressEvent('error') }))).toBe('Failed to run job now');
    });
});
