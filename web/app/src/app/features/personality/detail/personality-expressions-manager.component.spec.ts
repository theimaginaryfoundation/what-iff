import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { NEVER, of } from 'rxjs';

import { PersonalityExpressionsManagerComponent } from './personality-expressions-manager.component';
import { PersonalityExpression } from '../../../core/models/personality.model';
import { ExpressionAssignmentService } from '../../../core/services/expression-assignment.service';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { PersonalityMediaJobService } from '../../../core/services/personality-media-job.service';
import { PersonalityService } from '../../../core/services/personality.service';
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
            mediaJobs.refreshActiveJob.mockReturnValue(of(active));
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
        });
    });

    it('shows no slots when the expression list is empty', () => {
        fixture.componentRef.setInput('expressions', []);
        fixture.detectChanges();
        expect(component.slots().length).toBe(0);
    });
});
