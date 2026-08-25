import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { environment } from '@environments/environment';
import { AgentJobService } from './agent-job.service';

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
});
