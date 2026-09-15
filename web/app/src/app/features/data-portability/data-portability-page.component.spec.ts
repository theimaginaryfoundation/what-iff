import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection, signal } from '@angular/core';
import { Router } from '@angular/router';
import { of } from 'rxjs';

import { ACTIVE_ACCOUNT_IMPORT_JOB_STORAGE_KEY, AccountExportService } from '../../core/services/account-export.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { AccountArchiveService } from './account-archive.service';
import { DataPortabilityPageComponent } from './data-portability-page.component';

describe('DataPortabilityPageComponent', () => {
  let router: { navigate: ReturnType<typeof vi.fn> };
  let confirmation: { confirm: ReturnType<typeof vi.fn> };
  let accountExport: {
    getActivity: ReturnType<typeof vi.fn>;
    getImportStatus: ReturnType<typeof vi.fn>;
    activeImportJobId: ReturnType<typeof signal<string | null>>;
    clearActiveImport: ReturnType<typeof vi.fn>;
  };

  beforeEach(async () => {
    sessionStorage.clear();
    router = { navigate: vi.fn().mockName('Router.navigate') };
    confirmation = { confirm: vi.fn().mockResolvedValue(false) };
    accountExport = {
      getActivity: vi.fn().mockReturnValue(of([])),
      getImportStatus: vi.fn().mockReturnValue(of({ status: 'complete', progress: '{"message":"Import complete."}' })),
      activeImportJobId: signal<string | null>(null),
      clearActiveImport: vi.fn().mockImplementation(() => accountExport.activeImportJobId.set(null)),
    };

    await TestBed.configureTestingModule({
      imports: [DataPortabilityPageComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: Router, useValue: router },
        { provide: ConfirmationService, useValue: confirmation },
        { provide: AccountArchiveService, useValue: { inspect: vi.fn() } },
        { provide: AccountExportService, useValue: accountExport },
      ],
    })
      .overrideComponent(DataPortabilityPageComponent, { set: { template: '' } })
      .compileComponents();
  });

  afterEach(() => sessionStorage.clear());

  it('returns to the Thread Manager when closed', () => {
    const fixture = TestBed.createComponent(DataPortabilityPageComponent);

    fixture.componentInstance.close();

    expect(router.navigate).toHaveBeenCalledWith(['/chat']);
  });

  it('uses grammatical item lists in the account-import confirmation', async () => {
    const fixture = TestBed.createComponent(DataPortabilityPageComponent);
    const component = fixture.componentInstance;
    (component as any).pendingFile = new File(['zip'], 'account.zip', { type: 'application/zip' });
    component.selectedPersonalityIds.set(new Set(['personality-1']));
    component.selectedConversationIds.set(new Set(['thread-1']));
    component.includeMemories.set(true);

    await component.startSelectedImport();

    expect(confirmation.confirm).toHaveBeenCalledWith(
      expect.objectContaining({
        message: expect.stringContaining('1 personality, 1 thread, and memories'),
      }),
    );
  });

  it('explains secure asynchronous export delivery before confirmation', async () => {
    const fixture = TestBed.createComponent(DataPortabilityPageComponent);

    await fixture.componentInstance.requestAccountExport();

    expect(confirmation.confirm).toHaveBeenCalledWith(
      expect.objectContaining({
        confirmText: 'Confirm export',
        message: expect.stringContaining('link expires 24 hours after delivery'),
      }),
    );
  });

  it('resumes an active account import after returning to the page', async () => {
    sessionStorage.setItem(ACTIVE_ACCOUNT_IMPORT_JOB_STORAGE_KEY, 'import-job-1');
    accountExport.activeImportJobId.set('import-job-1');

    const fixture = TestBed.createComponent(DataPortabilityPageComponent);
    await vi.waitFor(() => expect(accountExport.getImportStatus).toHaveBeenCalledWith('import-job-1'));

    expect(fixture.componentInstance.importPhase()).toBe('complete');
    expect(fixture.componentInstance.importMessage()).toBe('Import complete.');
    expect(accountExport.clearActiveImport).toHaveBeenCalled();
  });
});
