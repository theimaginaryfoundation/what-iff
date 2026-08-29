import type { MockedObject } from 'vitest';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { Router } from '@angular/router';
import { of } from 'rxjs';

import { Personality } from '../../../core/models/personality.model';
import { PersonalityService } from '../../../core/services/personality.service';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { FileAttachmentService } from '../../../core/services/file-attachment.service';
import { PersonalityEditModalComponent } from './personality-edit-modal.component';

describe('personality scratchpad history deprecation', () => {
  let fixture: ComponentFixture<PersonalityEditModalComponent>;

  const personality: Personality = {
    id: 'p-history',
    name: 'History test',
    system_prompt: 'Prompt',
    scratchpad: 'Current scratchpad',
    scratchpad_history: ['Older scratchpad one', 'Older scratchpad two'],
    scratchpad_update_prompt: '',
    auto_pin_memories: false,
    expressions_enabled: true,
    image_style: 'auto',
    cover_image_id: null,
    cover_image_url: null,
    accent_color: null,
    thumbnail_circle: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    stats: { chat_count: 0, last_used_at: null },
  };

  beforeEach(async () => {
    const personalityService = {
      updatePersonality: vi.fn().mockReturnValue(of(personality)),
      deletePersonality: vi.fn().mockReturnValue(of(void 0)),
    } as unknown as Pick<MockedObject<PersonalityService>, 'updatePersonality' | 'deletePersonality'>;

    await TestBed.configureTestingModule({
      imports: [PersonalityEditModalComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideHttpClient(withXhr()),
        { provide: PersonalityService, useValue: personalityService },
        {
          provide: ImageGalleryService,
          useValue: {
            listImages: () => of({ results: [], total_count: 0, page: 1, page_size: 40 }),
            getImageUrl: () => '/img',
          },
        },
        {
          provide: FileAttachmentService,
          useValue: {
            listFileAttachments: () => of({ results: [], total_count: 0, page: 1, page_size: 40 }),
            uploadPersonalityFileAttachment: () => of(null),
            deleteFileAttachment: () => of(void 0),
          },
        },
        {
          provide: ConfirmationService,
          useValue: {
            confirm: async () => true,
            alert: async () => undefined,
            confirmDiscardChanges: async () => true,
          },
        },
        { provide: Router, useValue: { navigate: vi.fn() } },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(PersonalityEditModalComponent);
    fixture.componentRef.setInput('open', true);
    fixture.componentRef.setInput('personality', personality);
    fixture.detectChanges();
  });

  it('does not render legacy scratchpad history or restore controls', () => {
    fixture.componentInstance.toggleScratchpadAdvanced();
    fixture.detectChanges();

    const text = fixture.nativeElement.textContent as string;
    expect(text).not.toContain('Revert to a previous version');
    expect(text).not.toContain('Older scratchpad one');
    expect(text).not.toContain('Older scratchpad two');
    expect(text).not.toContain('Restore');

    expect(text).toContain('Custom scratchpad update prompt');
  });
});
