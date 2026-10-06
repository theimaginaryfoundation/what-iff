import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { NEVER, Subject, of, throwError } from 'rxjs';

import { PersonalityExpressionsManagerComponent } from './personality-expressions-manager.component';
import { PersonalityExpression } from '../../../core/models/personality.model';
import { ExpressionAssignmentService } from '../../../core/services/expression-assignment.service';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { PersonalityMediaJobService } from '../../../core/services/personality-media-job.service';
import { PersonalityService } from '../../../core/services/personality.service';
import { By } from '@angular/platform-browser';
import { ExpressionGenerateModalComponent } from './expression-generate-modal.component';
import { JobService } from '../../../core/services/job.service';
import { Job } from '../../../core/models/job.model';
import { ActivePersonalityMediaJob } from '../../../core/models/personality-media-job.model';

function makeExpression(overrides: Partial<PersonalityExpression> = {}): PersonalityExpression {
    return {
        expression_key: 'happy',
        label: null,
        image_id: null,
        image_url: null,
        created_at: '2026-04-01T00:00:00Z',
        updated_at: '2026-04-01T00:00:00Z',
        ...overrides,
    };
}

describe('PersonalityExpressionsManagerComponent', () => {
    let fixture: ComponentFixture<PersonalityExpressionsManagerComponent>;
    let component: PersonalityExpressionsManagerComponent;
    let assignment: Pick<MockedObject<ExpressionAssignmentService>, 'assignFromGallery' | 'setLabel' | 'clear' | 'remove'>;
    let imageGallery: Pick<MockedObject<ImageGalleryService>, 'listImages' | 'getImageUrl'>;
    let jobService: { getJob: ReturnType<typeof vi.fn> };
    let mediaJobs: Pick<MockedObject<PersonalityMediaJobService>, 'refreshActiveJob' | 'startExpressionCandidates' | 'pollUntilTerminal' | 'activeJob$'>;

    const expressions: PersonalityExpression[] = [
        {
            expression_key: 'happy',
            label: 'Happy',
            image_id: 'img-1',
            image_url: 'https://example.com/happy.png',
            created_at: '2026-04-01T00:00:00Z',
            updated_at: '2026-04-01T00:00:00Z',
        },
    ];

    beforeEach(async () => {
        assignment = {
            assignFromGallery: vi.fn().mockName("ExpressionAssignmentService.assignFromGallery"),
            setLabel: vi.fn().mockName("ExpressionAssignmentService.setLabel"),
            clear: vi.fn().mockName("ExpressionAssignmentService.clear"),
            remove: vi.fn().mockName("ExpressionAssignmentService.remove")
        } as unknown as Pick<MockedObject<ExpressionAssignmentService>, 'assignFromGallery' | 'setLabel' | 'clear' | 'remove'>;
        imageGallery = {
            listImages: vi.fn().mockName("ImageGalleryService.listImages"),
            getImageUrl: vi.fn().mockName("ImageGalleryService.getImageUrl")
        } as unknown as Pick<MockedObject<ImageGalleryService>, 'listImages' | 'getImageUrl'>;

        imageGallery.listImages.mockReturnValue(of({ results: [], total_count: 0, page: 1 } as any));
        imageGallery.getImageUrl.mockImplementation((id: string, size?: 'thumbnail' | 'full') => `/api/image-gallery/${id}?size=${size ?? 'thumbnail'}`);

        mediaJobs = {
            refreshActiveJob: vi.fn().mockName("PersonalityMediaJobService.refreshActiveJob"),
            startExpressionCandidates: vi.fn().mockName("PersonalityMediaJobService.startExpressionCandidates"),
            pollUntilTerminal: vi.fn().mockName("PersonalityMediaJobService.pollUntilTerminal"),
            activeJob$: of(null)
        } as unknown as Pick<MockedObject<PersonalityMediaJobService>, 'refreshActiveJob' | 'startExpressionCandidates' | 'pollUntilTerminal' | 'activeJob$'>;
        mediaJobs.refreshActiveJob.mockReturnValue(of(null));
        mediaJobs.pollUntilTerminal.mockReturnValue(NEVER);
        jobService = { getJob: vi.fn().mockName('JobService.getJob') };

        await TestBed.configureTestingModule({
            imports: [PersonalityExpressionsManagerComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                { provide: ExpressionAssignmentService, useValue: assignment },
                { provide: ImageGalleryService, useValue: imageGallery },
                { provide: PersonalityMediaJobService, useValue: mediaJobs },
                { provide: JobService, useValue: jobService },
                { provide: PersonalityService, useValue: {
                        listExpressions: vi.fn().mockName("PersonalityService.listExpressions")
                    } },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(PersonalityExpressionsManagerComponent);
        component = fixture.componentInstance;
        fixture.componentRef.setInput('personalityId', 'p-1');
        fixture.componentRef.setInput('expressions', expressions);
        fixture.detectChanges();
    });

    it('renders only persisted expression rows', () => {
        expect(component.slots().length).toBe(1);
        expect(component.slots()[0].expressionKey).toBe('happy');
    });

    it('counts slots missing an image', () => {
        expect(component.missingCount()).toBe(0);
        fixture.componentRef.setInput('expressions', [makeExpression({ expression_key: 'a', image_id: null }), makeExpression({ expression_key: 'b', image_id: 'x' })]);
        fixture.detectChanges();
        expect(component.missingCount()).toBe(1);
    });

    it('rejects invalid custom keys', () => {
        component.onCreateCustomKey();
        component.customKeyDraft = 'INVALID KEY';
        component.submitCustomKey();
        expect(component.customKeyError()).toContain('lowercase');
        expect(component.isCustomKeyOpen()).toBe(true);
    });

    it('emits expressionsChanged for a valid custom key', () => {
        let emitted: readonly PersonalityExpression[] | undefined;
        component.expressionsChanged.subscribe(value => { emitted = value; });
        component.onCreateCustomKey();
        component.customKeyDraft = 'mischievous';
        component.submitCustomKey();
        expect(emitted?.find(e => e.expression_key === 'mischievous')).toBeTruthy();
        expect(component.isCustomKeyOpen()).toBe(false);
        expect(component.isGalleryOpen()).toBe(true);
        expect(component.activeKey()).toBe('mischievous');
    });

    it('renders aria-label according to slot state', () => {
        const happy = component.slots().find(s => s.expressionKey === 'happy')!;
        expect(component.slotAriaLabel(happy)).toBe('happy: set');
    });

    it('derives slot image URLs from the gallery image id', () => {
        const happy = component.slots().find(s => s.expressionKey === 'happy')!;
        expect(component.slotImageUrl(happy)).toBe('/api/image-gallery/img-1?size=thumbnail');
    });

    describe('larger image viewer', () => {
        const FULL_URL = '/api/image-gallery/img-1?size=full';

        function expandButton(): HTMLButtonElement | null {
            return fixture.nativeElement.querySelector('button[aria-label="View happy larger"]');
        }

        function dialog(): HTMLElement | null {
            return fixture.nativeElement.querySelector('[role="dialog"]');
        }

        beforeEach(() => {
            if (!URL.createObjectURL) {
                (URL as unknown as { createObjectURL: (b: Blob) => string }).createObjectURL = () => 'blob:full';
            }
        });

        it('renders a native, keyboard-reachable expand button only for slots with an image', () => {
            const btn = expandButton()!;
            expect(btn).toBeTruthy();
            // A real <button type="button"> is focusable and activates on Enter/Space without extra key handlers.
            expect(btn.tagName).toBe('BUTTON');
            expect(btn.type).toBe('button');
            expect(btn.getAttribute('tabindex')).toBeNull();
            expect(btn.getAttribute('aria-haspopup')).toBe('dialog');

            fixture.componentRef.setInput('expressions', [makeExpression({ expression_key: 'empty', image_id: null })]);
            fixture.detectChanges();
            expect(fixture.nativeElement.querySelector('button[aria-label="View empty larger"]')).toBeNull();
        });

        it('opens a labelled dialog with the full-size image and expression alt text', () => {
            expect(dialog()).toBeNull();
            expandButton()!.click();
            fixture.detectChanges();

            const dlg = dialog()!;
            expect(dlg).toBeTruthy();
            expect(dlg.getAttribute('aria-modal')).toBe('true');
            expect(document.getElementById(dlg.getAttribute('aria-labelledby')!)?.textContent).toContain('happy');

            const http = TestBed.inject(HttpTestingController);
            http.expectOne(r => r.url.endsWith(FULL_URL)).flush(new Blob(['x']));
            fixture.detectChanges();

            const img = dlg.querySelector('img.expression-viewer__image') as HTMLImageElement;
            expect(img).toBeTruthy();
            expect(img.getAttribute('alt')).toBe('Happy');
        });

        it('falls back to the expression key for alt text when there is no label', () => {
            fixture.componentRef.setInput('expressions', [makeExpression({ expression_key: 'calm', image_id: 'img-2' })]);
            fixture.detectChanges();
            (fixture.nativeElement.querySelector('button[aria-label="View calm larger"]') as HTMLButtonElement).click();
            fixture.detectChanges();
            TestBed.inject(HttpTestingController).expectOne(r => r.url.endsWith('/api/image-gallery/img-2?size=full')).flush(new Blob(['x']));
            fixture.detectChanges();
            expect(dialog()!.querySelector('img')!.getAttribute('alt')).toBe('calm');
        });

        it('closes with the Escape key and restores focus to the expand button', () => {
            const btn = expandButton()!;
            btn.focus();
            btn.click();
            fixture.detectChanges();
            expect(component.viewer()?.key).toBe('happy');

            const backdrop = fixture.nativeElement.querySelector('.ui-modal') as HTMLElement;
            backdrop.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
            fixture.detectChanges();

            expect(component.viewer()).toBeNull();
            expect(dialog()).toBeNull();
            expect(document.activeElement).toBe(btn);
        });

        it('closes with the visible close button', () => {
            expandButton()!.click();
            fixture.detectChanges();
            (dialog()!.querySelector('button[aria-label="Close"]') as HTMLButtonElement).click();
            fixture.detectChanges();
            expect(dialog()).toBeNull();
        });

        it('closes on backdrop click', () => {
            expandButton()!.click();
            fixture.detectChanges();
            (fixture.nativeElement.querySelector('.ui-modal') as HTMLElement).click();
            fixture.detectChanges();
            expect(dialog()).toBeNull();
        });

        it('closes itself if the expression loses its image while open', () => {
            expandButton()!.click();
            fixture.detectChanges();
            expect(component.viewer()).not.toBeNull();
            fixture.componentRef.setInput('expressions', [makeExpression({ expression_key: 'happy', image_id: null })]);
            fixture.detectChanges();
            expect(component.viewer()).toBeNull();
            expect(dialog()).toBeNull();
        });
    });

    it('opens the Generate modal from the Generate button', () => {
        expect(component.generateButtonLabel()).toBe('Generate');
        component.openGenerate();
        expect(component.isGenerateOpen()).toBe(true);
    });

    it('closes the modal and emits the refreshed list after the modal saves', () => {
        let emitted: readonly PersonalityExpression[] | undefined;
        component.expressionsChanged.subscribe(value => { emitted = value; });
        component.openGenerate();
        const rows = [makeExpression({ expression_key: 'smug', image_id: 'img-9' })];
        component.onGenerateSaved(rows);
        expect(component.isGenerateOpen()).toBe(false);
        expect(emitted).toEqual(rows);
    });

    it('disables add-expression while the Generate modal is busy', () => {
        component.generateBusy.set(true);
        expect(component.gridGenerating()).toBe(true);
        component.generateBusy.set(false);
        expect(component.gridGenerating()).toBe(false);
    });

    it('wires the Generate modal outputs (dismiss, busyChange, saved)', () => {
        let emitted: readonly PersonalityExpression[] | undefined;
        component.expressionsChanged.subscribe(v => { emitted = v; });
        const modal = fixture.debugElement.query(By.directive(ExpressionGenerateModalComponent)).componentInstance as ExpressionGenerateModalComponent;
        component.openGenerate();
        modal.busyChange.emit(true);
        expect(component.generateBusy()).toBe(true);
        modal.busyChange.emit(false);
        modal.dismiss.emit();
        expect(component.isGenerateOpen()).toBe(false);
        component.openGenerate();
        const rows = [makeExpression({ expression_key: 'smug', image_id: 'img-9' })];
        modal.saved.emit(rows);
        expect(component.isGenerateOpen()).toBe(false);
        expect(emitted).toEqual(rows);
    });

    describe('resuming an active expression_grid job', () => {
        const active: ActivePersonalityMediaJob = {
            job_id: 'job-1',
            job_type: 'expression_grid',
            reference: 'p-1',
            status: 'processing',
            personality_id: 'p-1',
        };

        function job(progress?: string): Job {
            return {
                id: 'job-1', user_id: 'u', job_type: 'expression_grid', reference: 'p-1', status: 'processing',
                progress, created_at: '', updated_at: '',
            };
        }

        async function init(progress?: string): Promise<void> {
            const mode = progress ? 'candidates' : 'default';
            mediaJobs.refreshActiveJob.mockReturnValue(of({ ...active, expression_mode: mode }));
            jobService.getJob.mockReturnValue(of(job(progress)));
            fixture = TestBed.createComponent(PersonalityExpressionsManagerComponent);
            component = fixture.componentInstance;
            fixture.componentRef.setInput('personalityId', 'p-1');
            fixture.componentRef.setInput('expressions', expressions);
            fixture.detectChanges();
            await fixture.whenStable();
        }

        it('reopens the Generate modal for a candidate run', async () => {
            await init(JSON.stringify({ mode: 'candidates', expressions: ['happy', 'content', 'sad', 'angry', 'surprised', 'confused', 'tired', 'in-love', 'smug'] }));
            expect(component.isGenerateOpen()).toBe(true);
            expect(component.defaultGridRunning()).toBe(false);
            expect(mediaJobs.pollUntilTerminal).toHaveBeenCalledWith('job-1');
        });

        it('shows Generating… for a default-grid run', async () => {
            await init(undefined);
            expect(component.isGenerateOpen()).toBe(false);
            expect(component.defaultGridRunning()).toBe(true);
            expect(component.generateButtonLabel()).toBe('Generating…');
            expect(jobService.getJob).not.toHaveBeenCalled();
        });

        it('does not reopen the modal when the candidate job progress is unparseable', async () => {
            mediaJobs.refreshActiveJob.mockReturnValue(of({ ...active, expression_mode: 'candidates' }));
            jobService.getJob.mockReturnValue(of(job('not json')));
            fixture = TestBed.createComponent(PersonalityExpressionsManagerComponent);
            component = fixture.componentInstance;
            fixture.componentRef.setInput('personalityId', 'p-1');
            fixture.componentRef.setInput('expressions', expressions);
            fixture.detectChanges();
            await fixture.whenStable();
            expect(jobService.getJob).toHaveBeenCalledWith('job-1');
            expect(component.isGenerateOpen()).toBe(false);
            expect(mediaJobs.pollUntilTerminal).not.toHaveBeenCalled();
        });

        async function initWith(activeJob: ActivePersonalityMediaJob | null): Promise<void> {
            mediaJobs.refreshActiveJob.mockReturnValue(of(activeJob));
            fixture = TestBed.createComponent(PersonalityExpressionsManagerComponent);
            component = fixture.componentInstance;
            fixture.componentRef.setInput('personalityId', 'p-1');
            fixture.componentRef.setInput('expressions', expressions);
            fixture.detectChanges();
            await fixture.whenStable();
        }

        it('ignores jobs for another personality or of another type', async () => {
            await initWith({ ...active, personality_id: 'p-2' });
            await initWith({ ...active, job_type: 'personality_portrait' });
            expect(component.defaultGridRunning()).toBe(false);
            expect(mediaJobs.pollUntilTerminal).not.toHaveBeenCalled();
        });

        it('ignores terminal jobs', async () => {
            await initWith({ ...active, status: 'complete' });
            await initWith({ ...active, status: 'failed' });
            expect(component.defaultGridRunning()).toBe(false);
            expect(mediaJobs.pollUntilTerminal).not.toHaveBeenCalled();
            expect(jobService.getJob).not.toHaveBeenCalled();
        });

        describe('default-grid poll', () => {
            let poll$: Subject<Job>;
            let listExpressions: ReturnType<typeof vi.fn>;

            beforeEach(async () => {
                poll$ = new Subject<Job>();
                mediaJobs.pollUntilTerminal.mockReturnValue(poll$);
                listExpressions = TestBed.inject(PersonalityService).listExpressions as unknown as ReturnType<typeof vi.fn>;
                await initWith({ ...active, expression_mode: 'default' });
                expect(component.defaultGridRunning()).toBe(true);
            });

            it('blocks opening the Generate modal while running', () => {
                component.openGenerate();
                expect(component.isGenerateOpen()).toBe(false);
            });

            it('ignores non-terminal updates', () => {
                poll$.next(job());
                expect(component.defaultGridRunning()).toBe(true);
            });

            it('reloads and emits the list when the job completes', () => {
                const rows = [makeExpression({ expression_key: 'sad', image_id: 'i' })];
                listExpressions.mockReturnValue(of(rows));
                let emitted: readonly PersonalityExpression[] | undefined;
                component.expressionsChanged.subscribe(v => { emitted = v; });
                poll$.next({ ...job(), status: 'complete' });
                expect(listExpressions).toHaveBeenCalledWith('p-1');
                expect(component.defaultGridRunning()).toBe(false);
                expect(emitted).toEqual(rows);
            });

            it('surfaces a reload failure', () => {
                listExpressions.mockReturnValue(throwError(() => new Error('reload boom')));
                poll$.next({ ...job(), status: 'complete' });
                expect(component.defaultGridRunning()).toBe(false);
                expect(component.errorMessage()).toBe('reload boom');
            });

            it('surfaces a reload failure without a message', () => {
                listExpressions.mockReturnValue(throwError(() => ({})));
                poll$.next({ ...job(), status: 'complete' });
                expect(component.errorMessage()).toBe('Failed to reload expressions.');
            });

            it('surfaces a failed job', () => {
                poll$.next({ ...job(), status: 'failed', error: 'grid boom' });
                expect(component.defaultGridRunning()).toBe(false);
                expect(component.errorMessage()).toBe('grid boom');
            });

            it('uses a fallback message for a failed job without error', () => {
                poll$.next({ ...job(), status: 'failed' });
                expect(component.errorMessage()).toBe('Expression grid generation failed.');
            });

            it('surfaces a poll error', () => {
                poll$.error(new Error('poll boom'));
                expect(component.defaultGridRunning()).toBe(false);
                expect(component.errorMessage()).toBe('poll boom');
            });

            it('uses a fallback message for a poll error without message', () => {
                poll$.error({});
                expect(component.errorMessage()).toBe('Generation failed.');
            });
        });
    });

    it('shows no slots when the expression list is empty', () => {
        fixture.componentRef.setInput('expressions', []);
        fixture.detectChanges();
        expect(component.slots().length).toBe(0);
    });
});
