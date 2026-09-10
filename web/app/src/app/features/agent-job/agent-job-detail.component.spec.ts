import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection, signal } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { ActivatedRoute, Router } from '@angular/router';
import { of, throwError } from 'rxjs';
import { AgentJobDetailComponent } from './agent-job-detail.component';
import { AgentJobService } from '../../core/services/agent-job.service';
import { ChatService } from '../../core/services/chat.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { AuthService } from '../../core/services/auth.service';
import { ModelService } from '../../core/services/model.service';
import { PersonalityService } from '../../core/services/personality.service';
import { AgentJob, AgentJobSchedulePreview } from '../../core/models/agent-job.model';
import { Chat } from '../../core/models/chat.model';

describe('AgentJobDetailComponent', () => {
    let component: AgentJobDetailComponent;
    let fixture: ComponentFixture<AgentJobDetailComponent>;
    let mockAgentJobService: Pick<MockedObject<AgentJobService>, 'getAgentJob' | 'updateAgentJob' | 'updateAgentJobStatus' | 'deleteAgentJob' | 'parseSchedule' | 'runNow'>;
    let mockChatService: Pick<MockedObject<ChatService>, 'setLastChatId' | 'getChat' | 'listChats'>;
    let mockConfirmationService: Pick<MockedObject<ConfirmationService>, 'confirm'>;
    let mockRouter: Pick<MockedObject<Router>, 'navigate' | 'createUrlTree' | 'serializeUrl' | 'events'>;
    let mockActivatedRoute: any;
    let mockAuthService: any;
    let mockModelService: Pick<MockedObject<ModelService>, 'getModels'>;
    let mockPersonalityService: Pick<MockedObject<PersonalityService>, 'listPersonalities'>;

    const mockJob: AgentJob = {
        id: 'job-1',
        user_id: 'user-1',
        chat_id: 'chat-1',
        title: 'Morning check-in',
        prompt: 'Send me a concise morning summary.',
        schedule_input: 'every day at 8 AM',
        schedule_type: 'cron',
        schedule: '0 0 8 ? * *',
        run_at: null,
        timezone: 'America/Los_Angeles',
        status: 'active',
        next_run_at: '2026-03-21T15:00:00Z',
        last_run_at: null,
        last_error: null,
        run_count: 3,
        created_at: '2026-03-01T00:00:00Z',
        updated_at: '2026-03-20T00:00:00Z'
    };

    const mockPreview: AgentJobSchedulePreview = {
        schedule_type: 'cron',
        schedule: '0 0 9 ? * *',
        run_at: null,
        timezone: 'America/New_York',
        human_summary: 'Runs daily at 9:00 AM',
        next_runs: ['2026-03-21T13:00:00Z', '2026-03-22T13:00:00Z']
    };
    const mockChats: Chat[] = [
        {
            id: 'chat-1',
            user_id: 'user-1',
            name: 'Morning Chat',
            created_at: '2026-03-01T00:00:00Z',
            updated_at: '2026-03-01T00:00:00Z'
        },
        {
            id: 'chat-2',
            user_id: 'user-1',
            name: 'Work Chat',
            created_at: '2026-03-02T00:00:00Z',
            updated_at: '2026-03-02T00:00:00Z'
        }
    ];

    beforeEach(async () => {
        mockAgentJobService = {
            getAgentJob: vi.fn().mockName("AgentJobService.getAgentJob"),
            updateAgentJob: vi.fn().mockName("AgentJobService.updateAgentJob"),
            updateAgentJobStatus: vi.fn().mockName("AgentJobService.updateAgentJobStatus"),
            deleteAgentJob: vi.fn().mockName("AgentJobService.deleteAgentJob"),
            parseSchedule: vi.fn().mockName("AgentJobService.parseSchedule"),
            runNow: vi.fn().mockName("AgentJobService.runNow")
        } as unknown as Pick<MockedObject<AgentJobService>, 'getAgentJob' | 'updateAgentJob' | 'updateAgentJobStatus' | 'deleteAgentJob' | 'parseSchedule' | 'runNow'>;
        mockChatService = {
            setLastChatId: vi.fn().mockName("ChatService.setLastChatId"),
            getChat: vi.fn().mockName("ChatService.getChat"),
            listChats: vi.fn().mockName("ChatService.listChats")
        } as unknown as Pick<MockedObject<ChatService>, 'setLastChatId' | 'getChat' | 'listChats'>;
        mockConfirmationService = {
            confirm: vi.fn().mockName("ConfirmationService.confirm")
        } as unknown as Pick<MockedObject<ConfirmationService>, 'confirm'>;
        mockModelService = {
            getModels: vi.fn().mockName("ModelService.getModels")
        } as unknown as Pick<MockedObject<ModelService>, 'getModels'>;
        mockPersonalityService = {
            listPersonalities: vi.fn().mockName("PersonalityService.listPersonalities")
        } as unknown as Pick<MockedObject<PersonalityService>, 'listPersonalities'>;

        mockRouter = {
            navigate: vi.fn().mockName("Router.navigate"),
            createUrlTree: vi.fn().mockName("Router.createUrlTree"),
            serializeUrl: vi.fn().mockName("Router.serializeUrl"),
            events: of()
        } as unknown as Pick<MockedObject<Router>, 'navigate' | 'createUrlTree' | 'serializeUrl' | 'events'>;
        mockRouter.createUrlTree.mockReturnValue({} as any);
        mockRouter.serializeUrl.mockReturnValue('');

        mockActivatedRoute = {
            snapshot: {
                paramMap: {
                    get: vi.fn().mockName('get').mockReturnValue('job-1')
                }
            }
        };

        mockAuthService = {
            logoutPreferred: vi.fn().mockName("AuthService.logoutPreferred"),
            currentUser: signal({
                id: 'user-1',
                username: 'tester',
                email: 'tester@example.com',
                created_at: '2026-03-01T00:00:00Z',
                updated_at: '2026-03-01T00:00:00Z'
            }),
            isLoggedIn: signal(true)
        };

        await TestBed.configureTestingModule({
            imports: [AgentJobDetailComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                { provide: AgentJobService, useValue: mockAgentJobService },
                { provide: ChatService, useValue: mockChatService },
                { provide: ConfirmationService, useValue: mockConfirmationService },
                { provide: Router, useValue: mockRouter },
                { provide: ActivatedRoute, useValue: mockActivatedRoute },
                { provide: AuthService, useValue: mockAuthService },
                { provide: ModelService, useValue: mockModelService },
                { provide: PersonalityService, useValue: mockPersonalityService }
            ]
        }).compileComponents();

        mockModelService.getModels.mockReturnValue(of([]));
        mockPersonalityService.listPersonalities.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));
        mockAgentJobService.getAgentJob.mockReturnValue(of(mockJob));
        mockAgentJobService.updateAgentJob.mockReturnValue(of(mockJob));
        mockAgentJobService.updateAgentJobStatus.mockReturnValue(of(mockJob));
        mockAgentJobService.deleteAgentJob.mockReturnValue(of(void 0));
        mockAgentJobService.parseSchedule.mockReturnValue(of(mockPreview));
        mockAgentJobService.runNow.mockReturnValue(of({ status: 'triggered' }));
        mockChatService.getChat.mockReturnValue(of(mockChats[0]));
        mockChatService.listChats.mockReturnValue(of({
            results: mockChats,
            total_count: 2,
            page: 1
        }));
        fixture = TestBed.createComponent(AgentJobDetailComponent);
        component = fixture.componentInstance;
    });

    it('should create', () => {
        expect(component).toBeTruthy();
    });

    describe('ngOnInit and load', () => {
        it('loads the job and initializes edit fields when route id exists', () => {
            component.ngOnInit();

            expect(mockAgentJobService.getAgentJob).toHaveBeenCalledWith('job-1');
            expect(component.job()).toEqual(mockJob);
            expect(component.editTitle()).toBe(mockJob.title as string);
            expect(component.editPrompt()).toBe(mockJob.prompt);
            expect(component.editScheduleInput()).toBe(mockJob.schedule_input);
            expect(component.editTimezone()).toBe(mockJob.timezone);
            expect(component.selectedChatId()).toBe('chat-1');
            expect(mockChatService.listChats).toHaveBeenCalledWith(1, 50, undefined);
            expect(mockChatService.getChat).toHaveBeenCalledWith('chat-1');
            expect(component.isLoading()).toBe(false);
        });

        it('navigates back to list when route id is missing', () => {
            mockActivatedRoute.snapshot.paramMap.get.mockReturnValue(null);

            component.ngOnInit();

            expect(mockRouter.navigate).toHaveBeenCalledWith(['/agent-jobs']);
            expect(mockAgentJobService.getAgentJob).not.toHaveBeenCalled();
        });
    });

    describe('chat association selector', () => {
        beforeEach(() => {
            component.ngOnInit();
        });

        it('deduplicates options and keeps selected chat first', () => {
            mockChatService.listChats.mockReturnValue(of({
                results: [mockChats[0], mockChats[1]],
                total_count: 2,
                page: 1
            }));
            mockChatService.getChat.mockReturnValue(of({ ...mockChats[0], name: 'Morning Chat Selected' }));

            component.initializeChatSelector('chat-1');

            expect(component.chatOptions().length).toBe(2);
            expect(component.chatOptions()[0].id).toBe('chat-1');
            expect(component.chatOptions()[1].id).toBe('chat-2');
        });

        it('searches chats using name filter with debounce', () => {
            vi.useFakeTimers();
            mockChatService.listChats.mockClear();

            component.onChatSearchInput('Work');
            vi.advanceTimersByTime(299);
            expect(mockChatService.listChats).not.toHaveBeenCalled();

            vi.advanceTimersByTime(1);
            expect(mockChatService.listChats).toHaveBeenCalledWith(1, 50, { name: 'Work' });
            vi.useRealTimers();
        });

        it('does not call API for searches shorter than 3 characters', () => {
            vi.useFakeTimers();
            mockChatService.listChats.mockClear();

            component.onChatSearchInput('Wo');
            vi.advanceTimersByTime(300);

            expect(mockChatService.listChats).not.toHaveBeenCalled();
            expect(component.showChatSearchHint()).toBe(true);
            vi.useRealTimers();
        });

        it('saves selected chat association', () => {
            component.selectedChatId.set('chat-2');
            const updated = { ...mockJob, chat_id: 'chat-2' };
            mockAgentJobService.updateAgentJob.mockReturnValue(of(updated));

            component.saveChatAssociation();

            expect(mockAgentJobService.updateAgentJob).toHaveBeenCalledWith('job-1', { chat_id: 'chat-2' });
            expect(component.job()?.chat_id).toBe('chat-2');
            expect(component.chatSaveFeedback()).toBe('Saved.');
        });

        it('allows clearing chat association by saving empty selection', () => {
            const updated = { ...mockJob, chat_id: null };
            component.selectedChatId.set('');
            mockAgentJobService.updateAgentJob.mockReturnValue(of(updated));

            component.saveChatAssociation();

            expect(mockAgentJobService.updateAgentJob).toHaveBeenCalledWith('job-1', { chat_id: '' });
            expect(component.job()?.chat_id).toBeNull();
        });

        it('shows placeholder label when no chat selected', () => {
            component.selectedChatId.set('');
            component.selectedChat.set(null);
            expect(component.getSelectedChatLabel()).toBe('Select a chat');
        });
    });

    describe('save operations', () => {
        beforeEach(() => {
            component.ngOnInit();
        });

        it('saves title with trimmed value', () => {
            component.editTitle.set('  Updated title  ');
            const updated = { ...mockJob, title: 'Updated title' };
            mockAgentJobService.updateAgentJob.mockReturnValue(of(updated));

            component.saveTitle();

            expect(mockAgentJobService.updateAgentJob).toHaveBeenCalledWith('job-1', { title: 'Updated title' });
            expect(component.job()?.title).toBe('Updated title');
            expect(component.successMessage()).toBe('Saved.');
            expect(component.isSavingTitle()).toBe(false);
        });

        it('rejects empty prompt before API call', () => {
            component.editPrompt.set('   ');

            component.savePrompt();

            expect(component.errorMessage()).toBe('Prompt is required.');
            expect(mockAgentJobService.updateAgentJob).not.toHaveBeenCalledWith('job-1', { prompt: '' });
        });

        it('saves prompt with trimmed value and updates editor value', () => {
            component.editPrompt.set('  New agent prompt  ');
            const updated = { ...mockJob, prompt: 'New agent prompt' };
            mockAgentJobService.updateAgentJob.mockReturnValue(of(updated));

            component.savePrompt();

            expect(mockAgentJobService.updateAgentJob).toHaveBeenCalledWith('job-1', { prompt: 'New agent prompt' });
            expect(component.job()?.prompt).toBe('New agent prompt');
            expect(component.editPrompt()).toBe('New agent prompt');
            expect(component.promptSaveFeedback()).toBe('Saved.');
            expect(component.isSavingPrompt()).toBe(false);
        });

        it('requires schedule input before saving schedule', () => {
            component.editScheduleInput.set('  ');

            component.saveSchedule();

            expect(component.errorMessage()).toBe('Schedule input is required.');
            expect(mockAgentJobService.updateAgentJob).not.toHaveBeenCalledWith('job-1', expect.objectContaining({ schedule_input: expect.anything() }));
        });

        it('saves schedule and defaults timezone to UTC when blank', () => {
            component.editScheduleInput.set('every day at 9 AM');
            component.editTimezone.set('   ');

            component.saveSchedule();

            expect(mockAgentJobService.updateAgentJob).toHaveBeenCalledWith('job-1', {
                schedule_input: 'every day at 9 AM',
                timezone: 'UTC'
            });
            expect(component.successMessage()).toBe('Schedule updated.');
            expect(component.isSavingSchedule()).toBe(false);
        });
    });

    describe('schedule preview', () => {
        beforeEach(() => {
            component.ngOnInit();
        });

        it('requires schedule input before previewing', () => {
            component.editScheduleInput.set('   ');
            component.preview.set(mockPreview);

            component.previewSchedule();

            expect(component.errorMessage()).toBe('Schedule input is required to preview.');
            expect(component.preview()).toEqual(mockPreview);
            expect(mockAgentJobService.parseSchedule).not.toHaveBeenCalled();
        });

        it('loads preview and clears previewing state', () => {
            component.editScheduleInput.set('every day at 9 AM');
            component.editTimezone.set('America/New_York');

            component.previewSchedule();

            expect(mockAgentJobService.parseSchedule).toHaveBeenCalledWith({
                schedule_input: 'every day at 9 AM',
                timezone: 'America/New_York'
            });
            expect(component.preview()).toEqual(mockPreview);
            expect(component.isPreviewing()).toBe(false);
        });
    });

    describe('actions', () => {
        beforeEach(() => {
            component.ngOnInit();
        });

        it('opens chat by setting last chat id and navigating', () => {
            component.openChat();

            expect(mockChatService.setLastChatId).toHaveBeenCalledWith('chat-1');
            expect(mockChatService.getChat).toHaveBeenCalledWith('chat-1');
            expect(mockRouter.navigate).toHaveBeenCalledWith(['/chat']);
        });

        it('does nothing when openChat is called without chat id', () => {
            component.job.set({ ...mockJob, chat_id: null });
            mockChatService.setLastChatId.mockClear();
            mockChatService.getChat.mockClear();

            component.openChat();

            expect(mockChatService.setLastChatId).not.toHaveBeenCalled();
            expect(mockChatService.getChat).not.toHaveBeenCalled();
        });

        it('toggles active job to paused when confirmed', async () => {
            component.job.set({ ...mockJob, status: 'active' });
            mockConfirmationService.confirm.mockResolvedValue(true);
            const paused = { ...mockJob, status: 'paused' as const };
            mockAgentJobService.updateAgentJobStatus.mockReturnValue(of(paused));

            await component.togglePause();

            expect(mockConfirmationService.confirm).toHaveBeenCalled();
            expect(mockAgentJobService.updateAgentJobStatus).toHaveBeenCalledWith('job-1', { status: 'paused' });
            expect(component.job()?.status).toBe('paused');
        });

        it('does not toggle status when confirmation is cancelled', async () => {
            mockConfirmationService.confirm.mockResolvedValue(false);

            await component.togglePause();

            expect(mockAgentJobService.updateAgentJobStatus).not.toHaveBeenCalled();
        });

        it('runs job now without a chat and refreshes details', () => {
            const noChatJob = { ...mockJob, chat_id: null };
            const refreshed = { ...noChatJob, run_count: 4 };
            component.job.set(noChatJob);
            mockAgentJobService.getAgentJob.mockReturnValue(of(refreshed));
            mockRouter.navigate.mockClear();
            mockChatService.setLastChatId.mockClear();

            component.runNow();

            expect(mockAgentJobService.runNow).toHaveBeenCalledWith('job-1');
            expect(component.job()?.run_count).toBe(4);
            expect(mockChatService.setLastChatId).not.toHaveBeenCalled();
            expect(mockRouter.navigate).not.toHaveBeenCalledWith(['/chat']);
            expect(component.successMessage()).toBe('Job run triggered.');
            expect(component.isRunningNow()).toBe(false);
        });

        it('does not run job now for non-runnable statuses', () => {
            component.job.set({ ...mockJob, status: 'complete' });
            mockAgentJobService.runNow.mockClear();

            component.runNow();

            expect(mockAgentJobService.runNow).not.toHaveBeenCalled();
        });

        it('shows an error when run now fails', () => {
            mockAgentJobService.runNow.mockReturnValue(throwError(() => new Error('run failed')));

            component.ngOnInit();
            component.runNow();

            expect(component.errorMessage()).toBe('run failed');
            expect(component.isRunningNow()).toBe(false);
        });

        it('deletes job and navigates to list when confirmed', async () => {
            mockConfirmationService.confirm.mockResolvedValue(true);

            await component.deleteJob();

            expect(mockAgentJobService.deleteAgentJob).toHaveBeenCalledWith('job-1');
            expect(mockRouter.navigate).toHaveBeenCalledWith(['/agent-jobs']);
        });

        it('does not delete when confirmation is cancelled', async () => {
            mockConfirmationService.confirm.mockResolvedValue(false);

            await component.deleteJob();

            expect(mockAgentJobService.deleteAgentJob).not.toHaveBeenCalled();
        });
    });
});
