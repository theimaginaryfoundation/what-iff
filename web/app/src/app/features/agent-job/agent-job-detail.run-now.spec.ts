import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { ActivatedRoute, Router } from '@angular/router';
import { of } from 'rxjs';

import { AgentJobDetailComponent } from './agent-job-detail.component';
import { AgentJobService } from '../../core/services/agent-job.service';
import { ChatService } from '../../core/services/chat.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { ModelService } from '../../core/services/model.service';
import { PersonalityService } from '../../core/services/personality.service';
import { RitualService } from '../../core/services/ritual.service';
import { AgentJob } from '../../core/models/agent-job.model';
import { Chat } from '../../core/models/chat.model';

describe('AgentJobDetailComponent run-now breadcrumb', () => {
  const chat: Chat = {
    id: 'chat-1',
    user_id: 'user-1',
    name: 'Morning Chat',
    created_at: '2026-03-01T00:00:00Z',
    updated_at: '2026-03-01T00:00:00Z',
  };

  const job: AgentJob = {
    id: 'job-1',
    user_id: 'user-1',
    chat_id: chat.id,
    title: 'Morning check-in',
    prompt: 'Send me a concise morning summary.',
    schedule_input: 'every day at 8 AM',
    schedule_type: 'cron',
    schedule: '0 0 8 ? * *',
    run_at: null,
    timezone: 'UTC',
    status: 'active',
    next_run_at: null,
    last_run_at: null,
    last_error: null,
    run_count: 3,
    created_at: '2026-03-01T00:00:00Z',
    updated_at: '2026-03-20T00:00:00Z',
  };

  it('opens the associated chat after a successful run-now trigger', async () => {
    const agentJobService = {
      getAgentJob: vi.fn().mockReturnValue(of(job)),
      runNow: vi.fn().mockReturnValue(of({ status: 'triggered' })),
      updateAgentJob: vi.fn().mockReturnValue(of(job)),
      updateAgentJobStatus: vi.fn().mockReturnValue(of(job)),
      deleteAgentJob: vi.fn().mockReturnValue(of(void 0)),
      parseSchedule: vi.fn(),
      addRitual: vi.fn(),
      removeRitual: vi.fn(),
    };
    const chatService = {
      setLastChatId: vi.fn(),
      getChat: vi.fn().mockReturnValue(of(chat)),
      listChats: vi.fn().mockReturnValue(of({ results: [chat], total_count: 1, page: 1 })),
    };
    const router = {
      navigate: vi.fn().mockResolvedValue(true),
      createUrlTree: vi.fn(),
      serializeUrl: vi.fn().mockReturnValue(''),
      events: of(),
    };

    await TestBed.configureTestingModule({
      imports: [AgentJobDetailComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: AgentJobService, useValue: agentJobService },
        { provide: ChatService, useValue: chatService },
        { provide: ConfirmationService, useValue: { confirm: vi.fn() } },
        { provide: ModelService, useValue: { getModels: vi.fn().mockReturnValue(of([])) } },
        { provide: PersonalityService, useValue: { listPersonalities: vi.fn().mockReturnValue(of({ results: [], total_count: 0, page: 1 })) } },
        { provide: RitualService, useValue: { listRituals: vi.fn().mockReturnValue(of({ results: [], total_count: 0, page: 1 })) } },
        { provide: Router, useValue: router },
        { provide: ActivatedRoute, useValue: { snapshot: { paramMap: { get: () => 'job-1' } } } },
      ],
    }).compileComponents();

    const component = TestBed.createComponent(AgentJobDetailComponent).componentInstance;
    component.ngOnInit();
    component.runNow();

    expect(agentJobService.runNow).toHaveBeenCalledWith('job-1');
    expect(chatService.setLastChatId).toHaveBeenCalledWith('chat-1');
    expect(router.navigate).toHaveBeenCalledWith(['/chat']);
  });
});
