import type { MockedObject } from 'vitest';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { of, throwError, Subject } from 'rxjs';
import { Router } from '@angular/router';

import { PersonalityGenerateComponent } from './personality-generate.component';
import { GeneratePersonalityModalService } from '../../../core/services/generate-personality-modal.service';
import { PersonalityGenFlowService } from '../../../core/services/personality-gen-flow.service';
import { PersonalityMediaJobService } from '../../../core/services/personality-media-job.service';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { ChatService } from '../../../core/services/chat.service';
import { PersonalityGenFlow } from '../../../core/models/personality-gen-flow.model';
import { Job } from '../../../core/models/job.model';
import { Personality } from '../../../core/models/personality.model';
import { Chat } from '../../../core/models/chat.model';
import { FileAttachment } from '../../../core/models/file-attachment.model';

// Reliably drains all pending microtasks (chained awaits inside a
// fire-and-forget void method), unlike a fixed count of `await Promise.resolve()`.
function tick(): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, 0));
}

function makeFlow(overrides: Partial<PersonalityGenFlow> = {}): PersonalityGenFlow {
    return {
        id: 'flow-1',
        status: 'in_progress',
        current_step: 0,
        answers: {},
        generated_prompt: '',
        generated_about_me: '',
        generated_names: [],
        image_style: 'auto',
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
        ...overrides,
    };
}

describe('PersonalityGenerateComponent', () => {
    let fixture: ComponentFixture<PersonalityGenerateComponent>;
    let component: PersonalityGenerateComponent;

    let genFlowService: MockedObject<PersonalityGenFlowService>;
    let mediaJobs: MockedObject<PersonalityMediaJobService>;
    let imageGallery: Pick<MockedObject<ImageGalleryService>, 'getImageUrl' | 'importImage'>;
    let confirmationService: Pick<MockedObject<ConfirmationService>, 'confirm' | 'alert'>;
    let chatService: Pick<MockedObject<ChatService>, 'createChat' | 'setLastChatId'>;
    let generateModal: Pick<MockedObject<GeneratePersonalityModalService>, 'notifyPersonalityCreated'>;
    let router: { navigate: ReturnType<typeof vi.fn> };

    // Wires up mock providers and TestBed, but doesn't create the component yet
    // so individual tests can override a mock's return value (e.g. to reject)
    // before ngOnInit's loadFlow() runs.
    function setupProviders(initialFlow: PersonalityGenFlow = makeFlow()): void {
        genFlowService = {
            getOrCreateFlow: vi.fn().mockName('PersonalityGenFlowService.getOrCreateFlow'),
            updateFlow: vi.fn().mockName('PersonalityGenFlowService.updateFlow'),
            getFlow: vi.fn().mockName('PersonalityGenFlowService.getFlow'),
            resetFlow: vi.fn().mockName('PersonalityGenFlowService.resetFlow'),
            completeFlow: vi.fn().mockName('PersonalityGenFlowService.completeFlow'),
            regenerateFlow: vi.fn().mockName('PersonalityGenFlowService.regenerateFlow'),
            getActiveGenerationJob: vi.fn().mockName('PersonalityGenFlowService.getActiveGenerationJob'),
            acceptFlow: vi.fn().mockName('PersonalityGenFlowService.acceptFlow'),
        } as unknown as MockedObject<PersonalityGenFlowService>;
        genFlowService.getOrCreateFlow.mockReturnValue(of(initialFlow));
        genFlowService.getActiveGenerationJob.mockReturnValue(of(null));

        mediaJobs = {
            startFlowPortrait: vi.fn().mockName('PersonalityMediaJobService.startFlowPortrait'),
            pollUntilTerminal: vi.fn().mockName('PersonalityMediaJobService.pollUntilTerminal'),
        } as unknown as MockedObject<PersonalityMediaJobService>;

        imageGallery = {
            getImageUrl: vi.fn().mockName('ImageGalleryService.getImageUrl'),
            importImage: vi.fn().mockName('ImageGalleryService.importImage'),
        };
        imageGallery.getImageUrl.mockImplementation((id: string, size = 'thumbnail') => `/img/${id}/${size}`);

        confirmationService = {
            confirm: vi.fn().mockName('ConfirmationService.confirm'),
            alert: vi.fn().mockName('ConfirmationService.alert'),
        };
        confirmationService.confirm.mockResolvedValue(true);
        confirmationService.alert.mockResolvedValue(undefined);

        chatService = {
            createChat: vi.fn().mockName('ChatService.createChat'),
            setLastChatId: vi.fn().mockName('ChatService.setLastChatId'),
        };

        generateModal = {
            notifyPersonalityCreated: vi.fn().mockName('GeneratePersonalityModalService.notifyPersonalityCreated'),
        };

        router = { navigate: vi.fn().mockName('Router.navigate').mockResolvedValue(true) };

        TestBed.configureTestingModule({
            imports: [PersonalityGenerateComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                { provide: PersonalityGenFlowService, useValue: genFlowService },
                { provide: PersonalityMediaJobService, useValue: mediaJobs },
                { provide: ImageGalleryService, useValue: imageGallery },
                { provide: ConfirmationService, useValue: confirmationService },
                { provide: ChatService, useValue: chatService },
                { provide: GeneratePersonalityModalService, useValue: generateModal },
                { provide: Router, useValue: router },
            ],
        });
    }

    function createFixture(): void {
        fixture = TestBed.createComponent(PersonalityGenerateComponent);
        component = fixture.componentInstance;
        fixture.detectChanges();
    }

    /** setupProviders() + createFixture() in one call, for the common case. */
    function configure(initialFlow: PersonalityGenFlow = makeFlow()): void {
        setupProviders(initialFlow);
        createFixture();
    }

    describe('loadFlow (ngOnInit)', () => {
        it('hydrates state from the loaded flow', () => {
            configure(makeFlow({ current_step: 2, answers: { general_description: 'a fox' }, image_style: 'anime' }));

            expect(component.flow()?.id).toBe('flow-1');
            expect(component.currentStep()).toBe(2);
            expect(component.answers()).toEqual({ general_description: 'a fox' });
            expect(component.imageStyleSelection()).toBe('anime');
            expect(component.isLoading()).toBe(false);
        });

        it('surfaces an error message when the flow fails to load', () => {
            setupProviders();
            genFlowService.getOrCreateFlow.mockReturnValue(throwError(() => new Error('boom')));
            createFixture();

            expect(component.errorMessage()).toBe('Failed to load personality generation flow. Please try again.');
            expect(component.isLoading()).toBe(false);
        });

        it('sets the cover image from a reference image and skips portrait polling', () => {
            configure(makeFlow({ reference_image_id: 'ref-1' }));

            expect(component.coverImageId()).toBe('ref-1');
            expect(component.referenceImageId()).toBe('ref-1');
            expect(mediaJobs.startFlowPortrait).not.toHaveBeenCalled();
        });

        it('resumes polling an active generation job for an in-progress flow', () => {
            setupProviders(makeFlow({ status: 'in_progress' }));
            genFlowService.getActiveGenerationJob.mockReturnValue(
                of({ job_id: 'job-9', job_type: 'personality_generation', reference: 'flow-1', status: 'processing' }),
            );
            mediaJobs.pollUntilTerminal.mockReturnValue(new Subject<Job>());
            createFixture();

            expect(mediaJobs.pollUntilTerminal).toHaveBeenCalledWith('job-9');
            expect(component.isGenerating()).toBe(true);
        });

        it('ignores an active job of a different job_type', () => {
            setupProviders(makeFlow({ status: 'in_progress' }));
            genFlowService.getActiveGenerationJob.mockReturnValue(
                of({ job_id: 'job-9', job_type: 'personality_portrait', reference: 'flow-1', status: 'processing' }),
            );
            createFixture();

            expect(mediaJobs.pollUntilTerminal).not.toHaveBeenCalled();
        });

        it('starts portrait generation for a generated flow with no reference image', () => {
            setupProviders(makeFlow({ status: 'generated', generated_names: ['Vex'] }));
            mediaJobs.startFlowPortrait.mockReturnValue(
                of({ job_id: 'job-p1', job_type: 'personality_portrait' }),
            );
            mediaJobs.pollUntilTerminal.mockReturnValue(new Subject<Job>());
            createFixture();

            expect(mediaJobs.startFlowPortrait).toHaveBeenCalledWith('flow-1');
            expect(component.portraitGenerating()).toBe(true);
        });

        it('skips portrait generation when the image style is none', () => {
            setupProviders(makeFlow({ status: 'generated', image_style: 'none' }));
            createFixture();

            expect(mediaJobs.startFlowPortrait).not.toHaveBeenCalled();
        });
    });

    describe('answers and wizard navigation', () => {
        beforeEach(() => configure());

        it('getAnswer returns an empty string for an unset question', () => {
            expect(component.getAnswer('general_description')).toBe('');
        });

        it('setAnswer stores the value and getAnswer reads it back', () => {
            component.setAnswer('general_description', 'a sharp-witted fox');
            expect(component.getAnswer('general_description')).toBe('a sharp-witted fox');
        });

        it('reports the current page data for the active step', () => {
            expect(component.currentPageData().title).toBe('General');
        });

        it('canGoNext/canGoBack/isLastPage reflect position in the wizard', () => {
            expect(component.canGoBack()).toBe(false);
            expect(component.canGoNext()).toBe(true);
            expect(component.isLastPage()).toBe(false);

            component.currentStep.set(component.totalSteps - 1);
            expect(component.canGoNext()).toBe(false);
            expect(component.isLastPage()).toBe(true);
            expect(component.canGoBack()).toBe(true);
        });

        it('goNext advances the step and saves progress', () => {
            genFlowService.updateFlow.mockReturnValue(of(makeFlow({ current_step: 1 })));

            component.goNext();

            expect(component.currentStep()).toBe(1);
            expect(genFlowService.updateFlow).toHaveBeenCalledWith('flow-1', expect.objectContaining({ current_step: 1 }));
        });

        it('goNext does nothing on the last page', () => {
            component.currentStep.set(component.totalSteps - 1);
            component.goNext();
            expect(component.currentStep()).toBe(component.totalSteps - 1);
            expect(genFlowService.updateFlow).not.toHaveBeenCalled();
        });

        it('goBack retreats the step and saves progress', () => {
            component.currentStep.set(2);
            genFlowService.updateFlow.mockReturnValue(of(makeFlow({ current_step: 1 })));

            component.goBack();

            expect(component.currentStep()).toBe(1);
            expect(genFlowService.updateFlow).toHaveBeenCalled();
        });

        it('goBack does nothing on the first page', () => {
            component.goBack();
            expect(component.currentStep()).toBe(0);
            expect(genFlowService.updateFlow).not.toHaveBeenCalled();
        });

        it('jumpToStep moves directly to a step and saves', () => {
            genFlowService.updateFlow.mockReturnValue(of(makeFlow({ current_step: 3 })));
            component.jumpToStep(3);
            expect(component.currentStep()).toBe(3);
            expect(genFlowService.updateFlow).toHaveBeenCalled();
        });

        it('jumpToStep is a no-op for the current step', () => {
            component.jumpToStep(0);
            expect(genFlowService.updateFlow).not.toHaveBeenCalled();
        });

        it('jumpToStep is blocked on the review screen', () => {
            component.flow.set(makeFlow({ status: 'generated' }));
            component.jumpToStep(1);
            expect(component.currentStep()).toBe(0);
            expect(genFlowService.updateFlow).not.toHaveBeenCalled();
        });

        it('saveProgress surfaces no error message but resets isSaving on failure', () => {
            genFlowService.updateFlow.mockReturnValue(throwError(() => new Error('network down')));
            component.goNext();
            expect(component.isSaving()).toBe(false);
            expect(component.currentStep()).toBe(1);
        });

        it('progressPercent scales with the current step and is 100 on the review screen', () => {
            expect(component.progressPercent()).toBe(Math.round((1 / component.totalSteps) * 100));

            component.flow.set(makeFlow({ status: 'generated' }));
            expect(component.progressPercent()).toBe(100);
        });

        it('showSkipCta is true only past the first step, off the review screen, while not generating', () => {
            expect(component.showSkipCta()).toBe(false);

            component.currentStep.set(1);
            expect(component.showSkipCta()).toBe(true);

            component.isGenerating.set(true);
            expect(component.showSkipCta()).toBe(false);
        });
    });

    describe('finishAndGenerate', () => {
        beforeEach(() => configure());

        it('blocks generation and sets an error when no answers are set', () => {
            component.finishAndGenerate();

            expect(component.errorMessage()).toBe('You must set at least one field to generate a personality.');
            expect(component.isGenerating()).toBe(false);
            expect(genFlowService.updateFlow).not.toHaveBeenCalled();
        });

        it('treats whitespace-only answers as unset', () => {
            component.setAnswer('general_description', '   ');
            component.finishAndGenerate();
            expect(component.errorMessage()).toBe('You must set at least one field to generate a personality.');
        });

        it('reuses existing output when re-editing with unchanged answers', () => {
            component.flow.set(makeFlow({ status: 'generated', answers: { general_description: 'a fox' } }));
            component.answers.set({ general_description: 'a fox' });
            component.isEditingAnswers.set(true);

            component.finishAndGenerate();

            expect(component.isEditingAnswers()).toBe(false);
            expect(genFlowService.updateFlow).not.toHaveBeenCalled();
        });

        it('saves and enqueues generation, then polls the job to completion', () => {
            component.setAnswer('general_description', 'a fox');
            genFlowService.updateFlow.mockReturnValue(of(makeFlow({ answers: { general_description: 'a fox' } })));
            genFlowService.completeFlow.mockReturnValue(of({ job_id: 'job-1', job_type: 'personality_generation' }));
            genFlowService.getFlow.mockReturnValue(
                of(makeFlow({
                    status: 'generated',
                    answers: { general_description: 'a fox' },
                    generated_names: ['Vex'],
                    image_style: 'none',
                })),
            );
            mediaJobs.pollUntilTerminal.mockReturnValue(
                of({ id: 'job-1', user_id: 'u', job_type: 'personality_generation', reference: 'flow-1', status: 'complete', created_at: '', updated_at: '' }),
            );
            // refreshFlowAfterGeneration() (see below, `finishAndGenerate` ->
            // `refreshFlowAfterGeneration` -> `maybeStartPortrait`) doesn't sync
            // imageStyleSelection from the newly-fetched flow (see the dedicated bug
            // test below), so maybeStartPortrait still consults the stale 'auto'
            // signal from the initial load and starts a portrait job here despite
            // this flow's image_style being 'none'.
            mediaJobs.startFlowPortrait.mockReturnValue(of({ job_id: 'job-p', job_type: 'personality_portrait' }));

            component.finishAndGenerate();

            expect(genFlowService.completeFlow).toHaveBeenCalledWith('flow-1');
            expect(component.isGenerating()).toBe(false);
            expect(component.flow()?.status).toBe('generated');
        });

        it('surfaces the job error and stops generating when the job fails', () => {
            component.setAnswer('general_description', 'a fox');
            genFlowService.updateFlow.mockReturnValue(of(makeFlow()));
            genFlowService.completeFlow.mockReturnValue(of({ job_id: 'job-1', job_type: 'personality_generation' }));
            mediaJobs.pollUntilTerminal.mockReturnValue(
                of({ id: 'job-1', user_id: 'u', job_type: 'personality_generation', reference: 'flow-1', status: 'failed', error: 'model overloaded', created_at: '', updated_at: '' }),
            );

            component.finishAndGenerate();

            expect(component.isGenerating()).toBe(false);
            expect(component.errorMessage()).toBe('model overloaded');
        });

        it('resumes an already-running job on a 409 conflict instead of failing', () => {
            component.setAnswer('general_description', 'a fox');
            genFlowService.updateFlow.mockReturnValue(of(makeFlow()));
            genFlowService.completeFlow.mockReturnValue(
                throwError(() => ({ status: 409, error: { active: { job_id: 'job-existing' } } })),
            );
            mediaJobs.pollUntilTerminal.mockReturnValue(new Subject<Job>());

            component.finishAndGenerate();

            expect(mediaJobs.pollUntilTerminal).toHaveBeenCalledWith('job-existing');
            expect(component.isGenerating()).toBe(true);
        });

        it('surfaces a generic error when the generation request fails outright', () => {
            component.setAnswer('general_description', 'a fox');
            genFlowService.updateFlow.mockReturnValue(of(makeFlow()));
            genFlowService.completeFlow.mockReturnValue(throwError(() => ({ status: 500 })));

            component.finishAndGenerate();

            expect(component.isGenerating()).toBe(false);
            expect(component.errorMessage()).toBe('AI generation failed. Please try again.');
        });

        it('surfaces an error when saving answers before generation fails', () => {
            component.setAnswer('general_description', 'a fox');
            genFlowService.updateFlow.mockReturnValue(throwError(() => new Error('offline')));

            component.finishAndGenerate();

            expect(component.isGenerating()).toBe(false);
            expect(component.errorMessage()).toBe('Failed to save your answers. Please try again.');
            expect(genFlowService.completeFlow).not.toHaveBeenCalled();
        });

        it('BUG: refreshFlowAfterGeneration does not resync imageStyleSelection, so a fresh flow with image_style "none" still auto-starts a portrait job', () => {
            // Documents a real behavior gap in personality-generate.component.ts:
            // loadFlow() and resetDraft() both call parseStoredImageStyle(flow.image_style)
            // to set imageStyleSelection/customImageStyle, but refreshFlowAfterGeneration()
            // (around line 617) does not. maybeStartPortrait() (around line 558) branches on
            // the imageStyleSelection *signal*, not on flow().image_style, so after a
            // generation completes it uses whatever imageStyleSelection was left over from
            // the last explicit sync rather than the freshly-fetched flow's value.
            // Fix: refreshFlowAfterGeneration should parse and set imageStyleSelection /
            // customImageStyle (and referenceImageId) from `updated`, the same way loadFlow does.
            component.setAnswer('general_description', 'a fox');
            genFlowService.updateFlow.mockReturnValue(of(makeFlow({ image_style: 'auto' })));
            genFlowService.completeFlow.mockReturnValue(of({ job_id: 'job-1', job_type: 'personality_generation' }));
            genFlowService.getFlow.mockReturnValue(
                of(makeFlow({ status: 'generated', generated_names: ['Vex'], image_style: 'none' })),
            );
            mediaJobs.pollUntilTerminal.mockReturnValue(
                of({ id: 'job-1', user_id: 'u', job_type: 'personality_generation', reference: 'flow-1', status: 'complete', created_at: '', updated_at: '' }),
            );
            mediaJobs.startFlowPortrait.mockReturnValue(of({ job_id: 'job-p', job_type: 'personality_portrait' }));

            component.finishAndGenerate();

            // The fetched flow says "no image", but imageStyleSelection is still 'auto'
            // (its value from the initial loadFlow() in this spec's configure() helper),
            // so a portrait job is started anyway.
            expect(component.flow()?.image_style).toBe('none');
            expect(component.imageStyleSelection()).toBe('auto');
            expect(mediaJobs.startFlowPortrait).toHaveBeenCalledWith('flow-1');
        });

        it('refreshFlowAfterGeneration surfaces an error when the refresh itself fails', () => {
            component.setAnswer('general_description', 'a fox');
            genFlowService.updateFlow.mockReturnValue(of(makeFlow()));
            genFlowService.completeFlow.mockReturnValue(of({ job_id: 'job-1', job_type: 'personality_generation' }));
            genFlowService.getFlow.mockReturnValue(throwError(() => new Error('gone')));
            mediaJobs.pollUntilTerminal.mockReturnValue(
                of({ id: 'job-1', user_id: 'u', job_type: 'personality_generation', reference: 'flow-1', status: 'complete', created_at: '', updated_at: '' }),
            );

            component.finishAndGenerate();

            expect(component.isGenerating()).toBe(false);
            expect(component.errorMessage()).toBe('Generation finished, but we could not load the result. Please refresh.');
        });
    });

    describe('name selection on the review screen', () => {
        beforeEach(() => {
            configure(makeFlow({ status: 'generated', generated_names: ['Vex', 'Luma', 'Ash'], image_style: 'none' }));
        });

        it('falls back to a default label when there are no generated names', () => {
            component.flow.set(makeFlow({ status: 'generated', generated_names: [], image_style: 'none' }));
            expect(component.selectedGeneratedName()).toBe('Generated Personality');
        });

        it('cycles forward through generated names and wraps around', () => {
            expect(component.selectedGeneratedName()).toBe('Vex');
            component.nextName();
            expect(component.selectedGeneratedName()).toBe('Luma');
            component.nextName();
            component.nextName();
            expect(component.selectedGeneratedName()).toBe('Vex');
        });

        it('cycles backward through generated names and wraps around', () => {
            component.prevName();
            expect(component.selectedGeneratedName()).toBe('Ash');
        });

        it('nextName/prevName are no-ops when there are no names', () => {
            component.flow.set(makeFlow({ status: 'generated', generated_names: [], image_style: 'none' }));
            component.nextName();
            component.prevName();
            expect(component.selectedNameIndex()).toBe(0);
        });

        it('toggleManualNameMode seeds the manual field with the current generated name', () => {
            component.toggleManualNameMode();
            expect(component.isManualNameMode()).toBe(true);
            expect(component.manualName()).toBe('Vex');
            expect(component.selectedName()).toBe('Vex');

            component.toggleManualNameMode();
            expect(component.isManualNameMode()).toBe(false);
        });

        it('selectedName prefers a non-blank manual name while in manual mode', () => {
            component.toggleManualNameMode();
            component.manualName.set('Custom Name');
            expect(component.selectedName()).toBe('Custom Name');
        });

        it('selectedName falls back to the generated name when the manual name is blank', () => {
            component.toggleManualNameMode();
            component.manualName.set('   ');
            expect(component.selectedName()).toBe('Vex');
        });

        it('nextName resets manual mode back to the generated-name carousel', () => {
            component.toggleManualNameMode();
            component.nextName();
            expect(component.isManualNameMode()).toBe(false);
        });
    });

    describe('regenerate', () => {
        beforeEach(() => configure(makeFlow({ status: 'generated', generated_names: ['Vex'], image_style: 'none' })));

        it('resets the portrait job, enqueues regeneration and polls to completion', () => {
            genFlowService.regenerateFlow.mockReturnValue(of({ job_id: 'job-2', job_type: 'personality_generation' }));
            genFlowService.getFlow.mockReturnValue(of(makeFlow({ status: 'generated', generated_names: ['Nova'], image_style: 'none' })));
            mediaJobs.pollUntilTerminal.mockReturnValue(
                of({ id: 'job-2', user_id: 'u', job_type: 'personality_generation', reference: 'flow-1', status: 'complete', created_at: '', updated_at: '' }),
            );

            component.regenerate();

            expect(genFlowService.regenerateFlow).toHaveBeenCalledWith('flow-1');
            expect(component.selectedNameIndex()).toBe(0);
            expect(component.isGenerating()).toBe(false);
            expect(component.flow()?.generated_names).toEqual(['Nova']);
        });

        it('resumes an existing job on a 409 conflict', () => {
            genFlowService.regenerateFlow.mockReturnValue(
                throwError(() => ({ status: 409, error: { active: { job_id: 'job-active' } } })),
            );
            mediaJobs.pollUntilTerminal.mockReturnValue(new Subject<Job>());

            component.regenerate();

            expect(mediaJobs.pollUntilTerminal).toHaveBeenCalledWith('job-active');
        });

        it('surfaces an error when regeneration fails outright', () => {
            genFlowService.regenerateFlow.mockReturnValue(throwError(() => ({ status: 500 })));

            component.regenerate();

            expect(component.isGenerating()).toBe(false);
            expect(component.errorMessage()).toBe('AI generation failed. Please try again.');
        });
    });

    describe('accept', () => {
        const personality: Personality = {
            id: 'p-1',
            name: 'Vex',
            system_prompt: '',
            auto_pin_memories: false,
            cover_image_id: null,
            cover_image_url: null,
            expressions_enabled: true,
            image_style: 'auto',
            created_at: '',
            updated_at: '',
            stats: { chat_count: 0, last_used_at: null },
        };
        const chat: Chat = { id: 'chat-1', user_id: 'u', name: 'New Chat', created_at: '', updated_at: '' };

        beforeEach(() => configure(makeFlow({ status: 'generated', generated_names: ['Vex'], image_style: 'none' })));

        it('creates a personality, starts a chat and navigates to it', async () => {
            genFlowService.acceptFlow.mockReturnValue(of(personality));
            chatService.createChat.mockReturnValue(of(chat));

            // accept() itself returns void; it fires the request and handles the
            // result asynchronously inside the subscribe callback, so drain a
            // macrotask to let that chain (which may itself await a rejected
            // firstValueFrom, then an alert, then a navigate) fully settle.
            component.accept();
            await tick();

            expect(genFlowService.acceptFlow).toHaveBeenCalledWith('flow-1', { name: 'Vex' });
            expect(chatService.createChat).toHaveBeenCalledWith({ name: 'New Chat', personality_id: 'p-1' });
            expect(chatService.setLastChatId).toHaveBeenCalledWith('chat-1');
            expect(router.navigate).toHaveBeenCalledWith(['/chat', 'chat-1'], { queryParams: { welcome: 'true' } });
            expect(component.isAccepting()).toBe(false);
        });

        it('includes the cover image id when a portrait was generated', async () => {
            component.coverImageId.set('cover-9');
            genFlowService.acceptFlow.mockReturnValue(of(personality));
            chatService.createChat.mockReturnValue(of(chat));

            // accept() itself returns void; it fires the request and handles the
            // result asynchronously inside the subscribe callback, so drain a
            // macrotask to let that chain (which may itself await a rejected
            // firstValueFrom, then an alert, then a navigate) fully settle.
            component.accept();
            await tick();

            expect(genFlowService.acceptFlow).toHaveBeenCalledWith('flow-1', { name: 'Vex', cover_image_id: 'cover-9' });
        });

        it('notifies the modal service and closes instead of navigating in modal mode', async () => {
            fixture.componentRef.setInput('modalMode', true);
            vi.spyOn(component.closeModal, 'emit').mockReturnValue(undefined);
            genFlowService.acceptFlow.mockReturnValue(of(personality));
            chatService.createChat.mockReturnValue(of(chat));

            // accept() itself returns void; it fires the request and handles the
            // result asynchronously inside the subscribe callback, so drain a
            // macrotask to let that chain (which may itself await a rejected
            // firstValueFrom, then an alert, then a navigate) fully settle.
            component.accept();
            await tick();

            expect(generateModal.notifyPersonalityCreated).toHaveBeenCalledWith(personality);
            expect(component.closeModal.emit).toHaveBeenCalled();
            expect(router.navigate).toHaveBeenCalledWith(['/chat', 'chat-1'], { queryParams: { welcome: 'true' } });
        });

        it('is a no-op while already accepting', async () => {
            component.isAccepting.set(true);
            // accept() itself returns void; it fires the request and handles the
            // result asynchronously inside the subscribe callback, so drain a
            // macrotask to let that chain (which may itself await a rejected
            // firstValueFrom, then an alert, then a navigate) fully settle.
            component.accept();
            await tick();
            expect(genFlowService.acceptFlow).not.toHaveBeenCalled();
        });

        it('falls back to the personality detail page and alerts when chat creation fails', async () => {
            genFlowService.acceptFlow.mockReturnValue(of(personality));
            chatService.createChat.mockReturnValue(throwError(() => new Error('chat failed')));

            // accept() itself returns void; it fires the request and handles the
            // result asynchronously inside the subscribe callback, so drain a
            // macrotask to let that chain (which may itself await a rejected
            // firstValueFrom, then an alert, then a navigate) fully settle.
            component.accept();
            await tick();

            expect(confirmationService.alert).toHaveBeenCalledWith(
                expect.objectContaining({ message: expect.stringContaining('could not open a new chat') }),
            );
            expect(router.navigate).toHaveBeenCalledWith(['/personality', 'p-1']);
            expect(component.isAccepting()).toBe(false);
        });

        it('alerts and resets isAccepting when acceptFlow itself fails', async () => {
            genFlowService.acceptFlow.mockReturnValue(throwError(() => new Error('server error')));

            // accept() itself returns void; it fires the request and handles the
            // result asynchronously inside the subscribe callback, so drain a
            // macrotask to let that chain (which may itself await a rejected
            // firstValueFrom, then an alert, then a navigate) fully settle.
            component.accept();
            await tick();

            expect(confirmationService.alert).toHaveBeenCalledWith(
                expect.objectContaining({ message: 'Failed to create personality. Please try again.' }),
            );
            expect(component.isAccepting()).toBe(false);
        });
    });

    describe('resetDraft', () => {
        beforeEach(() => configure(makeFlow({ status: 'generated', generated_names: ['Vex'], image_style: 'none' })));

        it('does nothing without confirmation', async () => {
            confirmationService.confirm.mockResolvedValue(false);
            await component.resetDraft();
            expect(genFlowService.resetFlow).not.toHaveBeenCalled();
        });

        it('resets wizard state to the fresh draft flow on confirmation', async () => {
            confirmationService.confirm.mockResolvedValue(true);
            genFlowService.resetFlow.mockReturnValue(of(makeFlow({ id: 'flow-2', current_step: 0, status: 'in_progress' })));

            await component.resetDraft();

            expect(component.flow()?.id).toBe('flow-2');
            expect(component.currentStep()).toBe(0);
            expect(component.isResetting()).toBe(false);
            expect(component.selectedNameIndex()).toBe(0);
            expect(component.isManualNameMode()).toBe(false);
        });

        it('is a no-op while another operation is already in flight', async () => {
            component.isSaving.set(true);
            await component.resetDraft();
            expect(confirmationService.confirm).not.toHaveBeenCalled();
        });

        it('treats a rejected confirmation prompt as cancelled', async () => {
            confirmationService.confirm.mockRejectedValue(new Error('dialog closed'));
            await component.resetDraft();
            expect(genFlowService.resetFlow).not.toHaveBeenCalled();
        });

        it('surfaces an error and alerts when the reset request fails', async () => {
            confirmationService.confirm.mockResolvedValue(true);
            genFlowService.resetFlow.mockReturnValue(throwError(() => new Error('server down')));

            await component.resetDraft();

            expect(component.isResetting()).toBe(false);
            expect(component.errorMessage()).toBe('Failed to reset draft. Please try again.');
            expect(confirmationService.alert).toHaveBeenCalled();
        });
    });

    describe('navigation helpers', () => {
        beforeEach(() => configure());

        it('goBackToEdit switches to the last page in edit mode', () => {
            component.flow.set(makeFlow({ status: 'generated' }));
            component.goBackToEdit();

            expect(component.isEditingAnswers()).toBe(true);
            expect(component.currentStep()).toBe(component.totalSteps - 1);
        });

        it('goBackToList navigates to the personality list', () => {
            component.goBackToList();
            expect(router.navigate).toHaveBeenCalledWith(['/personality']);
        });

        it('closeOrBack emits closeModal in modal mode instead of navigating', () => {
            fixture.componentRef.setInput('modalMode', true);
            vi.spyOn(component.closeModal, 'emit').mockReturnValue(undefined);

            component.closeOrBack();

            expect(component.closeModal.emit).toHaveBeenCalled();
            expect(router.navigate).not.toHaveBeenCalled();
        });

        it('closeOrBack navigates to the personality list outside modal mode', () => {
            component.closeOrBack();
            expect(router.navigate).toHaveBeenCalledWith(['/personality']);
        });
    });

    describe('image style selection', () => {
        beforeEach(() => configure());

        it('selectImageStyle updates the selection and clears the custom text for non-"other" values', () => {
            component.customImageStyle.set('leftover');
            component.selectImageStyle('anime');

            expect(component.imageStyleSelection()).toBe('anime');
            expect(component.customImageStyle()).toBe('');
            expect(component.isImageStyleSelected('anime')).toBe(true);
        });

        it('selectImageStyle preserves custom text when selecting "other"', () => {
            component.selectImageStyle('other');
            component.customImageStyle.set('a moody noir style');

            expect(component.customImageStyle()).toBe('a moody noir style');
        });

        it('imageStylePillClass reflects the auto/none/other/default emphasis and selection', () => {
            expect(component.imageStylePillClass({ value: 'auto', label: 'Auto', emphasis: 'auto' })).toContain('emerald');
            expect(component.imageStylePillClass({ value: 'none', label: 'No Image', emphasis: 'none' })).toContain('amber');
            expect(component.imageStylePillClass({ value: 'other', label: 'Other', emphasis: 'other' })).toContain('violet');
            // No emphasis and not selected: the plain gray "unselected" style.
            expect(component.imageStylePillClass({ value: 'anime', label: 'Anime' })).toContain('gray');

            component.selectImageStyle('anime');
            expect(component.imageStylePillClass({ value: 'anime', label: 'Anime' })).toContain('bg-indigo-600');
        });
    });

    describe('portrait image and reference image', () => {
        beforeEach(() => configure(makeFlow({ image_style: 'none' })));

        it('portraitImageUrl is null with no cover image and resolves via ImageGalleryService otherwise', () => {
            expect(component.portraitImageUrl()).toBeNull();

            component.coverImageId.set('cover-1');
            expect(component.portraitImageUrl()).toBe('/img/cover-1/full');
        });

        it('showPortraitSection is false only when the image style is "none"', () => {
            expect(component.showPortraitSection()).toBe(false);
            component.selectImageStyle('anime');
            expect(component.showPortraitSection()).toBe(true);
        });

        it('portraitSectionLabel reflects whether a reference image was supplied', () => {
            expect(component.portraitSectionLabel()).toBe('Generated Portrait');
            component.referenceImageId.set('ref-1');
            expect(component.portraitSectionLabel()).toBe('Reference Image');
        });

        it('uploadReferenceImage does nothing without a selected file', () => {
            const event = { target: { files: [] } } as unknown as Event;
            component.uploadReferenceImage(event);
            expect(imageGallery.importImage).not.toHaveBeenCalled();
        });

        it('uploads, sets the reference/cover image and persists it', () => {
            const file = new File(['x'], 'ref.png', { type: 'image/png' });
            const event = { target: { files: [file] } } as unknown as Event;
            const attachment: FileAttachment = {
                id: 'att-1', user_id: 'u', name: 'ref.png', file_type: 'image/png', created_at: '',
            };
            imageGallery.importImage.mockReturnValue(of(attachment));
            genFlowService.updateFlow.mockReturnValue(of(makeFlow({ reference_image_id: 'att-1' })));

            component.uploadReferenceImage(event);

            expect(component.referenceImageId()).toBe('att-1');
            expect(component.coverImageId()).toBe('att-1');
            expect(component.imageUploadLoading()).toBe(false);
            expect(genFlowService.updateFlow).toHaveBeenCalledWith(
                'flow-1',
                expect.objectContaining({ reference_image_id: 'att-1' }),
            );
        });

        it('rolls back and alerts when persisting the reference image fails', async () => {
            const file = new File(['x'], 'ref.png', { type: 'image/png' });
            const event = { target: { files: [file] } } as unknown as Event;
            const attachment: FileAttachment = {
                id: 'att-1', user_id: 'u', name: 'ref.png', file_type: 'image/png', created_at: '',
            };
            imageGallery.importImage.mockReturnValue(of(attachment));
            genFlowService.updateFlow.mockReturnValue(throwError(() => new Error('save failed')));

            component.uploadReferenceImage(event);
            await Promise.resolve();
            await Promise.resolve();

            expect(component.referenceImageId()).toBeNull();
            expect(component.coverImageId()).toBeNull();
            expect(confirmationService.alert).toHaveBeenCalled();
        });

        it('surfaces a loading-reset when the upload itself fails', () => {
            const file = new File(['x'], 'ref.png', { type: 'image/png' });
            const event = { target: { files: [file] } } as unknown as Event;
            imageGallery.importImage.mockReturnValue(throwError(() => new Error('upload failed')));

            component.uploadReferenceImage(event);

            expect(component.imageUploadLoading()).toBe(false);
            expect(component.referenceImageId()).toBeNull();
        });

        it('clearReferenceImage optimistically clears and persists the change', () => {
            component.referenceImageId.set('ref-1');
            component.coverImageId.set('ref-1');
            genFlowService.updateFlow.mockReturnValue(of(makeFlow()));

            component.clearReferenceImage();

            expect(component.referenceImageId()).toBeNull();
            expect(component.coverImageId()).toBeNull();
            expect(genFlowService.updateFlow).toHaveBeenCalled();
        });

        it('rolls back and alerts when clearing the reference image fails to persist', async () => {
            component.referenceImageId.set('ref-1');
            component.coverImageId.set('ref-1');
            genFlowService.updateFlow.mockReturnValue(throwError(() => new Error('save failed')));

            component.clearReferenceImage();
            await Promise.resolve();
            await Promise.resolve();

            expect(component.referenceImageId()).toBe('ref-1');
            expect(component.coverImageId()).toBe('ref-1');
            expect(confirmationService.alert).toHaveBeenCalled();
        });
    });

    describe('hasUnsavedAnswers', () => {
        beforeEach(() => configure());

        it('is false with no changes from the loaded flow', () => {
            expect(component.hasUnsavedAnswers()).toBe(false);
        });

        it('is true after editing an answer', () => {
            component.setAnswer('general_description', 'a fox');
            expect(component.hasUnsavedAnswers()).toBe(true);
        });

        it('ignores whitespace-only differences', () => {
            component.setAnswer('general_description', '   ');
            expect(component.hasUnsavedAnswers()).toBe(false);
        });

        it('is false on the review screen even with pending edits', () => {
            component.flow.set(makeFlow({ status: 'generated' }));
            component.setAnswer('general_description', 'a fox');
            expect(component.hasUnsavedAnswers()).toBe(false);
        });

        it('is false while generating', () => {
            component.setAnswer('general_description', 'a fox');
            component.isGenerating.set(true);
            expect(component.hasUnsavedAnswers()).toBe(false);
        });
    });
});
