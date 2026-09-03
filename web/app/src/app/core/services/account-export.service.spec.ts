import { TestBed } from '@angular/core/testing';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { provideHttpClient } from '@angular/common/http';
import { provideZonelessChangeDetection } from '@angular/core';

import { environment } from '@environments/environment';
import { AccountExportService } from './account-export.service';

describe('AccountExportService', () => {
  let service: AccountExportService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [AccountExportService, provideZonelessChangeDetection(), provideHttpClient(), provideHttpClientTesting()],
    });
    service = TestBed.inject(AccountExportService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('enqueues an account export', () => {
    service.enqueue().subscribe();

    const request = http.expectOne(`${environment.apiUrl}/account/export`);
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual({});
    request.flush({
      id: 'job-1',
      user_id: 'user-1',
      job_type: 'account_export',
      reference: 'user-1',
      status: 'pending',
      created_at: '',
      updated_at: '',
    });
  });

  it('loads an account export status', () => {
    service.getStatus('job-1').subscribe();

    const request = http.expectOne(`${environment.apiUrl}/account/export/job-1`);
    expect(request.request.method).toBe('GET');
    request.flush({
      id: 'job-1',
      user_id: 'user-1',
      job_type: 'account_export',
      reference: 'user-1',
      status: 'complete',
      created_at: '',
      updated_at: '',
    });
  });

  it('enqueues an account import and loads its status', () => {
    const file = new File(['zip'], 'account-export.zip', { type: 'application/zip' });
    service.importAccount(file).subscribe();

    const importRequest = http.expectOne(`${environment.apiUrl}/account/import`);
    expect(importRequest.request.method).toBe('POST');
    expect(importRequest.request.body.get('file')).toBe(file);
    importRequest.flush({
      id: 'job-2', user_id: 'user-1', job_type: 'account_import', reference: 'user-1',
      status: 'pending', created_at: '', updated_at: '',
    });

    service.getImportStatus('job-2').subscribe();
    const statusRequest = http.expectOne(`${environment.apiUrl}/account/import/job-2`);
    expect(statusRequest.request.method).toBe('GET');
    statusRequest.flush({
      id: 'job-2', user_id: 'user-1', job_type: 'account_import', reference: 'user-1',
      status: 'complete', created_at: '', updated_at: '',
    });
  });
});
