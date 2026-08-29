import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideRouter } from '@angular/router';
import { of } from 'rxjs';

import { ChatService } from '../../core/services/chat.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { MemoryService } from '../../core/services/memory.service';
import { PersonalityService } from '../../core/services/personality.service';
import { CompactionEvent } from '../../core/models/memory.model';
import { CompactionLogPageComponent } from './compaction-log-page.component';

describe('CompactionLogPageComponent explanation snippets', () => {
  let fixture: ComponentFixture<CompactionLogPageComponent>;

  const event = {
    id: 'checkpoint-explanation',
    chat_id: 'chat-1',
    chat_name: 'Project thread',
    summary_explanation: 'Added the newly confirmed launch date and removed stale scheduling detail.',
    scratchpad_explanation: 'Recorded that the user now prefers the shorter onboarding flow.',
    old_summary: {
      id: 'summary-old',
      kind: 'summary',
      chat_id: 'chat-1',
      content: 'Launch timing is undecided.',
      created_at: '2026-08-29T12:00:00Z',
    },
    new_summary: {
      id: 'summary-new',
      kind: 'summary',
      chat_id: 'chat-1',
      content: 'Launch is scheduled for September 15 after the final QA pass.',
      created_at: '2026-08-29T13:00:00Z',
    },
    old_scratchpad: {
      id: 'scratch-old',
      kind: 'scratchpad',
      personality_id: 'personality-1',
      content: 'User is evaluating onboarding options.',
      created_at: '2026-08-29T12:00:00Z',
    },
    new_scratchpad: {
      id: 'scratch-new',
      kind: 'scratchpad',
      personality_id: 'personality-1',
      content: 'User prefers the shorter onboarding flow.',
      created_at: '2026-08-29T13:00:00Z',
    },
    created_at: '2026-08-29T13:00:00Z',
    updated_at: '2026-08-29T13:00:00Z',
  } as CompactionEvent & {
    summary_explanation: string;
    scratchpad_explanation: string;
  };

  beforeEach(async () => {
    const memoryService = {
      listCompactionEvents: vi.fn().mockReturnValue(of({ results: [event], total_count: 1, page: 1 })),
      revertSnapshot: vi.fn(),
    };
    const chatService = {
      listChats: vi.fn().mockReturnValue(of({ results: [], total_count: 0, page: 1 })),
    };
    const personalityService = {
      listPersonalities: vi.fn().mockReturnValue(of({ results: [], total_count: 0, page: 1 })),
    };

    await TestBed.configureTestingModule({
      imports: [CompactionLogPageComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideRouter([]),
        { provide: MemoryService, useValue: memoryService },
        { provide: ChatService, useValue: chatService },
        { provide: PersonalityService, useValue: personalityService },
        { provide: ConfirmationService, useValue: { confirm: vi.fn() } },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(CompactionLogPageComponent);
    fixture.detectChanges();
    fixture.componentInstance.toggleExpanded(event);
    fixture.detectChanges();
  });

  it('shows human-readable summary and scratchpad explanations for the checkpoint', () => {
    const text = fixture.nativeElement.textContent as string;

    expect(text).toContain(event.summary_explanation);
    expect(text).toContain(event.scratchpad_explanation);
  });
});
