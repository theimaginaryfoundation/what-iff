import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { of } from 'rxjs';

import { PersonalityExpressionsManagerComponent } from './personality-expressions-manager.component';
import { PersonalityExpression } from '../../../core/models/personality.model';
import { ExpressionAssignmentService } from '../../../core/services/expression-assignment.service';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { PersonalityMediaJobService } from '../../../core/services/personality-media-job.service';
import { PersonalityService } from '../../../core/services/personality.service';
import { DEFAULT_EXPRESSION_SUGGESTIONS } from '../helpers/expressions.helpers';

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
    let mediaJobs: Pick<MockedObject<PersonalityMediaJobService>, 'refreshActiveJob' | 'startExpressionGrid' | 'pollUntilTerminal' | 'activeJob$'>;

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
            startExpressionGrid: vi.fn().mockName("PersonalityMediaJobService.startExpressionGrid"),
            pollUntilTerminal: vi.fn().mockName("PersonalityMediaJobService.pollUntilTerminal"),
            activeJob$: of(null)
        } as unknown as Pick<MockedObject<PersonalityMediaJobService>, 'refreshActiveJob' | 'startExpressionGrid' | 'pollUntilTerminal' | 'activeJob$'>;
        mediaJobs.refreshActiveJob.mockReturnValue(of(null));

        await TestBed.configureTestingModule({
            imports: [PersonalityExpressionsManagerComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                { provide: ExpressionAssignmentService, useValue: assignment },
                { provide: ImageGalleryService, useValue: imageGallery },
                { provide: PersonalityMediaJobService, useValue: mediaJobs },
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

    it('uses Regenerate label when the full default grid is complete', () => {
        const fullGrid: PersonalityExpression[] = [...DEFAULT_EXPRESSION_SUGGESTIONS].map((key, i) => makeExpression({
            expression_key: key,
            image_id: `img-${i}`,
            image_url: `https://example.com/${key}.png`,
        }));
        fixture.componentRef.setInput('expressions', fullGrid);
        fixture.detectChanges();
        expect(component.defaultGridComplete()).toBe(true);
        expect(component.gridButtonLabel()).toContain('Regenerate');
    });

    it('uses Generate label when the grid is incomplete or empty', () => {
        expect(component.defaultGridComplete()).toBe(false);
        expect(component.gridButtonLabel()).toContain('Generate');

        fixture.componentRef.setInput('expressions', []);
        fixture.detectChanges();
        expect(component.defaultGridComplete()).toBe(false);
        expect(component.gridButtonLabel()).toContain('Generate');
    });

    it('shows no slots when the expression list is empty', () => {
        fixture.componentRef.setInput('expressions', []);
        fixture.detectChanges();
        expect(component.slots().length).toBe(0);
    });
});
