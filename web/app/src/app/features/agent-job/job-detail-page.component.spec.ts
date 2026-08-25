import type { Mock, MockedObject } from "vitest";
import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { ActivatedRoute, Router } from '@angular/router';
import { NEVER, of, throwError } from 'rxjs';

import { AgentJob } from '../../core/models/agent-job.model';
import { Ritual } from '../../core/models/ritual.model';
import { AgentJobService } from '../../core/services/agent-job.service';
import { ChatService } from '../../core/services/chat.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { ModelService } from '../../core/services/model.service';
import { PersonalityService } from '../../core/services/personality.service';
import { RitualService } from '../../core/services/ritual.service';
import { JobDetailPageComponent } from './job-detail-page.component';

describe('JobDetailPageComponent', () => {
    let fixture: ComponentFixture<JobDetailPageComponent>;
    let agentJobService: Pick<MockedObject<AgentJobService>, 'getAgentJob' | 'createAgentJob' | 'updateAgentJob' | 'updateAgentJobStatus' | 'deleteAgentJob' | 'parseSchedule' | 'runNow' | 'addRitual' | 'removeRitual'>;
    let chatService: Pick<MockedObject<ChatService>, 'listAllChats' | 'setLastChatId'>;
    let modelService: Pick<MockedObject<ModelService>, 'getModels'>;
    let personalityService: Pick<MockedObject<PersonalityService>, 'listPersonalities'>;
    let ritualService: Pick<MockedObject<RitualService>, 'listRituals'>;
    let route: {
        snapshot: {
            paramMap: {
                get: Mock;
            };
        };
    };

    const job: AgentJob = {
        id: 'job-1',
        user_id: 'user-1',
        chat_id: 'chat-1',
        title: 'Morning summary',
        prompt: 'Summarize my day',
        schedule_input: 'Every day at 8am',
        schedule_type: 'cron',
        schedule: '0 8 * * *',
        timezone: 'UTC',
        status: 'active',
        run_count: 2,
        rituals: [],
        created_at: '2026-08-01T12:00:00Z',
        updated_at: '2026-08-01T12:00:00Z',
    };

    const ritual: Ritual = {
        id: 'ritual-1',
        name: 'Daily brief',
        description: 'Prepare a brief',
        content: 'Summarize today',
        hotkeys: '',
        personality_id: null,
        created_at: '2026-08-01T12:00:00Z',
        updated_at: '2026-08-01T12:00:00Z',
    };

    beforeEach(async () => {
        agentJobService = {
            getAgentJob: vi.fn().mockName("AgentJobService.getAgentJob"),
            createAgentJob: vi.fn().mockName("AgentJobService.createAgentJob"),
            updateAgentJob: vi.fn().mockName("AgentJobService.updateAgentJob"),
            updateAgentJobStatus: vi.fn().mockName("AgentJobService.updateAgentJobStatus"),
            deleteAgentJob: vi.fn().mockName("AgentJobService.deleteAgentJob"),
            parseSchedule: vi.fn().mockName("AgentJobService.parseSchedule"),
            runNow: vi.fn().mockName("AgentJobService.runNow"),
            addRitual: vi.fn().mockName("AgentJobService.addRitual"),
            removeRitual: vi.fn().mockName("AgentJobService.removeRitual")
        } as unknown as Pick<MockedObject<AgentJobService>, 'getAgentJob' | 'createAgentJob' | 'updateAgentJob' | 'updateAgentJobStatus' | 'deleteAgentJob' | 'parseSchedule' | 'runNow' | 'addRitual' | 'removeRitual'>;
        chatService = {
            listAllChats: vi.fn().mockName("ChatService.listAllChats"),
            setLastChatId: vi.fn().mockName("ChatService.setLastChatId")
        } as unknown as Pick<MockedObject<ChatService>, 'listAllChats' | 'setLastChatId'>;
        modelService = {
            getModels: vi.fn().mockName("ModelService.getModels")
        } as unknown as Pick<MockedObject<ModelService>, 'getModels'>;
        personalityService = {
            listPersonalities: vi.fn().mockName("PersonalityService.listPersonalities")
        } as unknown as Pick<MockedObject<PersonalityService>, 'listPersonalities'>;
        ritualService = {
            listRituals: vi.fn().mockName("RitualService.listRituals")
        } as unknown as Pick<MockedObject<RitualService>, 'listRituals'>;
        const confirmation = {
            confirm: vi.fn().mockName("ConfirmationService.confirm")
        };
        const router = {
            navigate: vi.fn().mockName("Router.navigate")
        };
        route = { snapshot: { paramMap: { get: vi.fn().mockName('get').mockReturnValue('job-1') } } };

        agentJobService.getAgentJob.mockReturnValue(of(job));
        chatService.listAllChats.mockReturnValue(of({ chats: [], truncated: false }));
        modelService.getModels.mockReturnValue(of([]));
        personalityService.listPersonalities.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));
        ritualService.listRituals.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));

        await TestBed.configureTestingModule({
            imports: [JobDetailPageComponent],
            providers: [
                provideZonelessChangeDetection(),
                { provide: AgentJobService, useValue: agentJobService },
                { provide: ChatService, useValue: chatService },
                { provide: ModelService, useValue: modelService },
                { provide: PersonalityService, useValue: personalityService },
                { provide: RitualService, useValue: ritualService },
                { provide: ConfirmationService, useValue: confirmation },
                { provide: Router, useValue: router },
                { provide: ActivatedRoute, useValue: route },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(JobDetailPageComponent);
    });

    it('creates', () => {
        expect(fixture.componentInstance).toBeTruthy();
    });

    it('renders loading and error branches', () => {
        agentJobService.getAgentJob.mockReturnValue(NEVER);
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('[role="status"]')?.textContent).toContain('Loading job');

        fixture.destroy();
        fixture = TestBed.createComponent(JobDetailPageComponent);
        agentJobService.getAgentJob.mockReturnValue(throwError(() => new Error('Job unavailable')));
        fixture.detectChanges();

        expect(fixture.nativeElement.querySelector('[role="alert"]')?.textContent).toContain('Job unavailable');
        expect(fixture.nativeElement.querySelector('app-job-form')).toBeNull();
    });

    it('renders the populated job actions and empty attached-skills branch', () => {
        fixture.detectChanges();
        const host = fixture.nativeElement as HTMLElement;

        expect(host.textContent).toContain('Morning summary');
        expect(host.textContent).toContain('Status: active');
        expect(host.textContent).toContain('Open chat');
        expect(host.textContent).toContain('Run now');
        expect(host.textContent).toContain('Pause');
        expect(host.textContent).toContain('No attached skills.');
        expect(host.querySelector('app-job-form')).not.toBeNull();
        expect(host.querySelector('app-job-run-history')).not.toBeNull();
    });

    it('renders every attached and available skill, and hides ineligible actions', () => {
        fixture.detectChanges();
        const component = fixture.componentInstance;
        component.attachedRituals.set([ritual]);
        component.ritualOptions.set([{ ...ritual, id: 'ritual-2', name: 'Weekly review' }]);
        component.job.set({ ...job, chat_id: null, status: 'complete' });
        fixture.detectChanges();
        const host = fixture.nativeElement as HTMLElement;

        expect(host.textContent).toContain('Daily brief');
        expect(host.textContent).toContain('+ Weekly review');
        expect(host.textContent).not.toContain('No attached skills.');
        expect(host.textContent).not.toContain('Open chat');
        expect(host.textContent).not.toContain('Pause');
        expect(host.textContent).not.toContain('Resume');
    });

    it('renders create mode without edit-only sections or actions', () => {
        route.snapshot.paramMap.get.mockReturnValue('new');
        fixture.detectChanges();
        const host = fixture.nativeElement as HTMLElement;

        expect(host.textContent).toContain('Create job');
        expect(host.textContent).not.toContain('Status:');
        expect(host.textContent).not.toContain('Attached skills');
        expect(host.textContent).not.toContain('Run now');
        expect(host.querySelector('app-job-form')).not.toBeNull();
        expect(host.querySelector('app-job-run-history')).toBeNull();
    });
});
