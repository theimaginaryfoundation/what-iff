import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { Subject, of, throwError } from 'rxjs';

import { ExpressionGenerateModalComponent } from './expression-generate-modal.component';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { PersonalityMediaJobService } from '../../../core/services/personality-media-job.service';
import { PersonalityService } from '../../../core/services/personality.service';
import { Job } from '../../../core/models/job.model';
import { PersonalityExpression } from '../../../core/models/personality.model';
import { DEFAULT_EXPRESSION_SUGGESTIONS } from '../helpers/expressions.helpers';

const KEYS = [...DEFAULT_EXPRESSION_SUGGESTIONS];

function completedJob(imageIds: string[], keys: string[] = KEYS): Job {
    return {
        id: 'job-1', user_id: 'u', job_type: 'expression_grid', reference: 'p-1', status: 'complete',
        progress: JSON.stringify({
            mode: 'candidates',
            expressions: keys,
            candidates: keys.map((k, i) => ({ expression_key: k, image_id: imageIds[i] })),
        }),
        created_at: '', updated_at: '',
    };
}

const ids = (prefix: string) => KEYS.map((_, i) => `${prefix}-${i}`);

describe('ExpressionGenerateModalComponent', () => {
    let fixture: ComponentFixture<ExpressionGenerateModalComponent>;
    let component: ExpressionGenerateModalComponent;
    let poll$: Subject<Job>;
    let mediaJobs: { startExpressionCandidates: ReturnType<typeof vi.fn>; pollUntilTerminal: ReturnType<typeof vi.fn> };
    let gallery: { getImageUrl: ReturnType<typeof vi.fn>; deleteImage: ReturnType<typeof vi.fn>; listImages: ReturnType<typeof vi.fn>; importImage: ReturnType<typeof vi.fn> };
    let personalityApi: { upsertExpression: ReturnType<typeof vi.fn>; listExpressions: ReturnType<typeof vi.fn> };

    beforeEach(async () => {
        poll$ = new Subject<Job>();
        mediaJobs = {
            startExpressionCandidates: vi.fn().mockReturnValue(of({ job_id: 'job-1', job_type: 'expression_grid' })),
            pollUntilTerminal: vi.fn().mockImplementation(() => poll$),
        };
        gallery = {
            getImageUrl: vi.fn().mockImplementation((id: string) => `/api/image-gallery/${id}?size=thumbnail`),
            deleteImage: vi.fn().mockReturnValue(of(undefined)),
            listImages: vi.fn().mockReturnValue(of({ results: [], total_count: 0, page: 1 })),
            importImage: vi.fn(),
        };
        personalityApi = {
            upsertExpression: vi.fn().mockImplementation((_p: string, key: string) => of({ expression_key: key })),
            listExpressions: vi.fn().mockReturnValue(of([])),
        };

        await TestBed.configureTestingModule({
            imports: [ExpressionGenerateModalComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                { provide: PersonalityMediaJobService, useValue: mediaJobs },
                { provide: ImageGalleryService, useValue: gallery },
                { provide: PersonalityService, useValue: personalityApi },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(ExpressionGenerateModalComponent);
        component = fixture.componentInstance;
        fixture.componentRef.setInput('personalityId', 'p-1');
        fixture.componentRef.setInput('coverImageId', 'cover-1');
        fixture.componentRef.setInput('open', true);
        fixture.detectChanges();
    });

    it('prefills the nine default names and preselects the cover as reference', () => {
        expect(component.keys()).toEqual(KEYS);
        expect(component.cells()[7].name).toBe('in love');
        expect(component.referenceImageId()).toBe('cover-1');
    });

    it('sends slugged custom names and the reference to the candidates endpoint', () => {
        component.setName(0, 'Big Grin');
        component.generate();
        const expected = ['big-grin', ...KEYS.slice(1)];
        expect(mediaJobs.startExpressionCandidates).toHaveBeenCalledWith('p-1', expected, 'cover-1');
        expect(component.generating()).toBe(true);
    });

    it('sends a null reference after Remove', () => {
        component.referenceImageId.set(null);
        component.generate();
        expect(mediaJobs.startExpressionCandidates).toHaveBeenCalledWith('p-1', KEYS, null);
    });

    it('blocks generation on duplicate or empty names', () => {
        component.setName(1, 'Happy');
        expect(component.cellError(1)).toBe('Duplicate name');
        component.setName(2, '!!!');
        expect(component.cellError(2)).toBe('Name required');
        expect(component.canGenerate()).toBe(false);
        component.generate();
        expect(mediaJobs.startExpressionCandidates).not.toHaveBeenCalled();
    });

    it('fills the grid with candidates when the job completes', () => {
        component.generate();
        poll$.next(completedJob(ids('a')));
        expect(component.generating()).toBe(false);
        expect(component.cells().map(c => c.imageId)).toEqual(ids('a'));
        expect(component.keptCount()).toBe(9);
        expect(component.hasCandidates()).toBe(true);
    });

    it('surfaces a failed job and keeps the names', () => {
        component.setName(0, 'smirk');
        component.generate();
        poll$.next({ ...completedJob([]), status: 'failed', error: 'boom' });
        expect(component.error()).toBe('boom');
        expect(component.cells()[0].name).toBe('smirk');
        expect(component.hasCandidates()).toBe(false);
    });

    it('regenerate replaces only discarded cells and deletes surplus/replaced images', () => {
        component.generate();
        poll$.next(completedJob(ids('a')));
        component.setKeep(0, false);
        component.setKeep(4, false);
        expect(component.regenerateCount()).toBe(2);

        component.generate();
        poll$.next(completedJob(ids('b')));

        const cells = component.cells();
        expect(cells[0].imageId).toBe('b-0');
        expect(cells[4].imageId).toBe('b-4');
        expect(cells[1].imageId).toBe('a-1');
        expect(cells.every(c => c.keep)).toBe(true);
        const deleted = gallery.deleteImage.mock.calls.map(c => c[0]).sort();
        // Replaced a-0/a-4 plus the seven surplus panels from round b.
        expect(deleted).toEqual(['a-0', 'a-4', 'b-1', 'b-2', 'b-3', 'b-5', 'b-6', 'b-7', 'b-8'].sort());
    });

    it('saves kept candidates, deletes discarded ones, and emits the refreshed list', () => {
        const rows: PersonalityExpression[] = [{ expression_key: 'happy', label: null, image_id: 'a-0', image_url: null, created_at: '', updated_at: '' }];
        personalityApi.listExpressions.mockReturnValue(of(rows));
        let saved: readonly PersonalityExpression[] | undefined;
        component.saved.subscribe(v => { saved = v; });

        component.generate();
        poll$.next(completedJob(ids('a')));
        component.setKeep(8, false);
        component.setName(1, 'Calm');
        component.save();

        expect(personalityApi.upsertExpression).toHaveBeenCalledTimes(8);
        expect(personalityApi.upsertExpression).toHaveBeenCalledWith('p-1', 'calm', { image_id: 'a-1' });
        expect(personalityApi.upsertExpression).not.toHaveBeenCalledWith('p-1', 'thinking', expect.anything());
        expect(gallery.deleteImage).toHaveBeenCalledWith('a-8');
        expect(saved).toEqual(rows);
        expect(component.hasCandidates()).toBe(false);
        expect(component.keys()).toEqual(KEYS);
    });

    it('keeps candidates when save fails', () => {
        personalityApi.upsertExpression.mockReturnValue(throwError(() => ({ error: { message: 'nope' } })));
        component.generate();
        poll$.next(completedJob(ids('a')));
        component.save();
        expect(component.error()).toBe('nope');
        expect(component.hasCandidates()).toBe(true);
        expect(gallery.deleteImage).not.toHaveBeenCalled();
    });

    it('discard all deletes every candidate and keeps names', () => {
        component.setName(3, 'livid');
        component.generate();
        poll$.next(completedJob(ids('a')));
        component.discardAll();
        expect(gallery.deleteImage).toHaveBeenCalledTimes(9);
        expect(component.hasCandidates()).toBe(false);
        expect(component.cells()[3].name).toBe('livid');
    });

    it('flags keys whose existing image would be replaced', () => {
        fixture.componentRef.setInput('existingExpressions', [
            { expression_key: 'sad', label: null, image_id: 'old', image_url: null, created_at: '', updated_at: '' },
        ]);
        component.generate();
        poll$.next(completedJob(ids('a')));
        expect(component.replacesExisting(2)).toBe(true);
        expect(component.replacesExisting(0)).toBe(false);
        component.setKeep(2, false);
        expect(component.replacesExisting(2)).toBe(false);
    });

    it('resume restores names and reference from the job progress', () => {
        const names = ['smug', ...KEYS.slice(1)];
        component.resume('job-9', { mode: 'candidates', expressions: names, reference_image_id: 'ref-2' });
        expect(component.keys()).toEqual(names);
        expect(component.referenceImageId()).toBe('ref-2');
        expect(mediaJobs.pollUntilTerminal).toHaveBeenCalledWith('job-9');
        expect(component.generating()).toBe(true);
    });

    it('deletes unsaved candidates when destroyed', () => {
        component.generate();
        poll$.next(completedJob(ids('a')));
        fixture.destroy();
        expect(gallery.deleteImage).toHaveBeenCalledTimes(9);
    });
});
