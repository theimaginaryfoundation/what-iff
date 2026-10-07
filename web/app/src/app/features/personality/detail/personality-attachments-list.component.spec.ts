import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideRouter } from '@angular/router';
import { of } from 'rxjs';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { FileAttachmentService } from '../../../core/services/file-attachment.service';
import { PersonalityAttachmentsListComponent } from './personality-attachments-list.component';

function attachment(overrides: Partial<FileAttachment>): FileAttachment {
  return {
    id: 'a-1',
    user_id: 'u-1',
    name: 'lore.md',
    file_type: 'text/markdown',
    created_at: '2026-01-01T00:00:00Z',
    ...overrides,
  };
}

describe('PersonalityAttachmentsListComponent', () => {
  let fixture: ComponentFixture<PersonalityAttachmentsListComponent>;
  let listFileAttachments: ReturnType<typeof vi.fn>;

  beforeEach(async () => {
    listFileAttachments = vi.fn().mockReturnValue(
      of({ results: [attachment({ id: 'doc-1', name: 'lore.md' })], total_count: 1, page: 1, page_size: 40 }),
    );

    await TestBed.configureTestingModule({
      imports: [PersonalityAttachmentsListComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideRouter([]),
        {
          provide: FileAttachmentService,
          useValue: {
            listFileAttachments,
            uploadPersonalityFileAttachment: vi.fn(),
            deleteFileAttachment: vi.fn(),
          },
        },
        { provide: ConfirmationService, useValue: { confirm: async () => true, alert: async () => undefined } },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(PersonalityAttachmentsListComponent);
  });

  it('lists only reference docs, so a personality portrait (an image pinned to it) is not shown', async () => {
    fixture.componentRef.setInput('personalityId', 'p-1');
    fixture.detectChanges();
    await fixture.whenStable();

    // docs_only is the contract that keeps images (the uploaded portrait, expression images)
    // out of this list and out of its slot count; the backend applies it as "not image/*".
    expect(listFileAttachments).toHaveBeenCalledTimes(1);
    expect(listFileAttachments).toHaveBeenCalledWith(1, 40, { personality_id: 'p-1', docs_only: true });
    expect(fixture.nativeElement.textContent).toContain('lore.md');
    expect(fixture.componentInstance.remaining()).toBe(39);
  });

  it('links each doc to the gallery file viewer, so its contents can be read (#43)', async () => {
    fixture.componentRef.setInput('personalityId', 'p-1');
    fixture.detectChanges();
    await fixture.whenStable();

    const view = (fixture.nativeElement as HTMLElement).querySelector<HTMLAnchorElement>('a[aria-label="View lore.md"]');
    expect(view).not.toBeNull();
    expect(view!.getAttribute('href')).toBe('/gallery?file=doc-1');
  });
});
