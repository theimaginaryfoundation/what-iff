import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { of } from 'rxjs';

import { AccountExportService } from '../../core/services/account-export.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { ExperimentalPageComponent } from './experimental-page.component';

type Spy = ReturnType<typeof vi.fn>;

describe('ExperimentalPageComponent', () => {
  let accountExport: Record<'enqueue' | 'getStatus' | 'importAccount' | 'getImportStatus', Spy>;
  let confirmation: Record<'confirm', Spy>;

  beforeEach(async () => {
    accountExport = {
      enqueue: vi.fn(),
      getStatus: vi.fn(),
      importAccount: vi.fn(),
      getImportStatus: vi.fn(),
    };
    confirmation = { confirm: vi.fn() };
    confirmation.confirm.mockReturnValue(Promise.resolve(true));

    accountExport.enqueue.mockReturnValue(
      of({
        id: 'account-export-1',
        user_id: 'user-1',
        job_type: 'account_export',
        reference: 'user-1',
        status: 'complete',
        progress: JSON.stringify({ message: 'Export ready — a download link has been emailed to you.' }),
        created_at: '',
        updated_at: '',
      }),
    );
    accountExport.getStatus.mockReturnValue(
      of({
        id: 'account-export-1',
        user_id: 'user-1',
        job_type: 'account_export',
        reference: 'user-1',
        status: 'complete',
        progress: JSON.stringify({ message: 'Export ready — a download link has been emailed to you.' }),
        created_at: '',
        updated_at: '',
      }),
    );
    accountExport.importAccount.mockReturnValue(
      of({
        id: 'account-import-1',
        user_id: 'user-1',
        job_type: 'account_import',
        reference: 'user-1',
        status: 'pending',
        created_at: '',
        updated_at: '',
      }),
    );
    accountExport.getImportStatus.mockReturnValue(
      of({
        id: 'account-import-1',
        user_id: 'user-1',
        job_type: 'account_import',
        reference: 'user-1',
        status: 'complete',
        progress: JSON.stringify({
          result: {
            conversations: { imported: 4, skipped: 1 },
            personalities: { created: 2, skipped: 0 },
            memories: { imported_count: 8, duplicate_count: 3 },
          },
        }),
        created_at: '',
        updated_at: '',
      }),
    );

    await TestBed.configureTestingModule({
      imports: [ExperimentalPageComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: AccountExportService, useValue: accountExport as unknown as AccountExportService },
        { provide: ConfirmationService, useValue: confirmation as unknown as ConfirmationService },
      ],
    }).compileComponents();
  });

  it('keeps the pre-release warning visible', () => {
    const fixture = TestBed.createComponent(ExperimentalPageComponent);
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('If you found this, beware!');
    expect(fixture.nativeElement.textContent).toContain('pre-release features');
  });

  it('runs account export and import from the unlinked page', async () => {
    const fixture = TestBed.createComponent(ExperimentalPageComponent);
    const component = fixture.componentInstance;

    await component.requestAccountExport();
    const file = new File(['zip'], 'account-export.zip', { type: 'application/zip' });
    const input = { files: [file], value: 'account-export.zip' } as unknown as HTMLInputElement;
    await component.importAccountFile({ target: input } as unknown as Event);
    await new Promise(resolve => setTimeout(resolve, 0));

    expect(accountExport.enqueue).toHaveBeenCalled();
    expect(accountExport.importAccount).toHaveBeenCalledWith(file);
    expect(component.accountImportStatus()).toContain('Conversations: 4 imported, 1 skipped');
  });
});
