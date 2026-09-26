import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { Subject, of, throwError } from 'rxjs';

import { ExpressionGenerateModalComponent } from './expression-generate-modal.component';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { PersonalityMediaJobService } from '../../../core/services/personality-media-job.service';
import { PersonalityService } from '../../../core/services/personality.service';
import { FileAttachment } from '../../../core/models/file-attachment.model';
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

    // --- Additional coverage ---

    const img = (id: string, created_at: string): FileAttachment =>
        ({ id, name: `${id}.png`, created_at } as unknown as FileAttachment);

    function render(): void {
        fixture.detectChanges();
    }

    function buttonByText(text: string): HTMLButtonElement | undefined {
        return Array.from(document.querySelectorAll('button')).find(b => b.textContent?.trim().startsWith(text)) as HTMLButtonElement | undefined;
    }

    function uploadEvent(file: File | null): Event {
        const input = document.createElement('input');
        input.type = 'file';
        Object.defineProperty(input, 'files', { value: file ? [file] : [] });
        return { target: input } as unknown as Event;
    }

    it('does not reset the reference when reopened', () => {
        component.referenceImageId.set('other');
        fixture.componentRef.setInput('open', false);
        render();
        fixture.componentRef.setInput('open', true);
        render();
        expect(component.referenceImageId()).toBe('other');
    });

    it('renders the idle state with cancel/generate and a status line', () => {
        render();
        expect(component.statusText()).toBe('One image generates all nine portraits.');
        let dismissed = false;
        component.dismiss.subscribe(() => { dismissed = true; });
        buttonByText('Cancel')!.click();
        expect(dismissed).toBe(true);
        buttonByText('Generate')!.click();
        expect(mediaJobs.startExpressionCandidates).toHaveBeenCalled();
        render();
        expect(buttonByText('Hide')).toBeTruthy();
        expect(buttonByText('Generating')).toBeTruthy();
        expect(component.statusText()).toContain('Generating');
    });

    it('renders name errors and edits names through the input', async () => {
        component.setName(1, 'happy');
        render();
        expect(document.body.textContent).toContain('Duplicate name');
        await fixture.whenStable();
        const input = document.querySelector('input[aria-label="Expression 2 name"]') as HTMLInputElement;
        input.value = 'Gleeful';
        input.dispatchEvent(new Event('input'));
        render();
        expect(component.cells()[1].name).toBe('Gleeful');
    });

    it('renders candidates with keep/discard controls, replaces badge, save and discard-all', () => {
        fixture.componentRef.setInput('existingExpressions', [
            { expression_key: 'happy', label: null, image_id: 'old', image_url: null, created_at: '', updated_at: '' },
        ]);
        component.generate();
        poll$.next(completedJob(ids('a')));
        render();
        expect(document.body.textContent).toContain('replaces');
        (document.querySelector('button[aria-label="Discard happy"]') as HTMLButtonElement).click();
        render();
        expect(component.cells()[0].keep).toBe(false);
        expect(component.statusText()).toBe('8 kept · 1 discarded');
        (document.querySelector('button[aria-label="Keep happy"]') as HTMLButtonElement).click();
        expect(component.cells()[0].keep).toBe(true);
        component.setKeep(0, false);
        render();
        expect(buttonByText('Regenerate 1')).toBeTruthy();
        buttonByText('Save 8')!.click();
        expect(personalityApi.upsertExpression).toHaveBeenCalledTimes(8);
        component.generate();
        poll$.next(completedJob(ids('c')));
        render();
        buttonByText('Discard all')!.click();
        expect(component.hasCandidates()).toBe(false);
    });

    it('renders the saving state and ignores actions while busy', () => {
        const pending = new Subject<PersonalityExpression[]>();
        personalityApi.listExpressions.mockReturnValue(pending);
        const busy: boolean[] = [];
        component.busyChange.subscribe(v => busy.push(v));
        component.generate();
        poll$.next(completedJob(ids('a')));
        component.save();
        render();
        expect(component.saving()).toBe(true);
        expect(component.statusText()).toBe('Saving…');
        expect(buttonByText('Saving…')).toBeTruthy();
        component.save();
        component.discardAll();
        component.generate();
        component.resume('job-x', { mode: 'candidates', expressions: KEYS });
        expect(personalityApi.upsertExpression).toHaveBeenCalledTimes(9);
        expect(mediaJobs.startExpressionCandidates).toHaveBeenCalledTimes(1);
        // Destroy while saving must not delete the candidates being saved.
        fixture.destroy();
        expect(gallery.deleteImage).not.toHaveBeenCalled();
        expect(busy).toEqual([true, false, true]);
    });

    it('refuses to save when a kept candidate has an invalid name', () => {
        component.generate();
        poll$.next(completedJob(ids('a')));
        component.setName(0, '');
        component.save();
        expect(component.error()).toBe('Fix the highlighted names before saving.');
        expect(personalityApi.upsertExpression).not.toHaveBeenCalled();
        render();
        expect(document.querySelector('[role="alert"]')?.textContent).toContain('Fix the highlighted');
    });

    it('does nothing on save without kept candidates', () => {
        component.save();
        expect(personalityApi.upsertExpression).not.toHaveBeenCalled();
    });

    it('falls back to generic save error messages', () => {
        personalityApi.upsertExpression.mockReturnValue(throwError(() => new Error('net down')));
        component.generate();
        poll$.next(completedJob(ids('a')));
        component.save();
        expect(component.error()).toBe('net down');

        personalityApi.upsertExpression.mockReturnValue(throwError(() => ({})));
        component.save();
        expect(component.error()).toBe('Failed to save expressions.');
    });

    describe('generate errors', () => {
        it('uses the server message on 409', () => {
            mediaJobs.startExpressionCandidates.mockReturnValue(throwError(() => ({ status: 409, error: { message: 'job running' } })));
            component.generate();
            expect(component.error()).toBe('job running');
            expect(component.generating()).toBe(false);
        });

        it('uses a default message on 409 without body', () => {
            mediaJobs.startExpressionCandidates.mockReturnValue(throwError(() => ({ status: 409 })));
            component.generate();
            expect(component.error()).toBe('Another image job is already running.');
        });

        it('uses the server message on 404 (missing reference)', () => {
            mediaJobs.startExpressionCandidates.mockReturnValue(throwError(() => ({ status: 404, error: { message: 'reference image not found' } })));
            component.generate();
            expect(component.error()).toBe('reference image not found');
        });

        it('falls back to err.message then a generic message', () => {
            mediaJobs.startExpressionCandidates.mockReturnValue(throwError(() => ({ status: 500, message: 'Http failure' })));
            component.generate();
            expect(component.error()).toBe('Http failure');
            mediaJobs.startExpressionCandidates.mockReturnValue(throwError(() => ({})));
            component.generate();
            expect(component.error()).toBe('Generation failed.');
        });
    });

    describe('poll outcomes', () => {
        it('ignores non-terminal updates', () => {
            component.generate();
            poll$.next({ ...completedJob([]), status: 'processing' });
            expect(component.generating()).toBe(true);
        });

        it('uses a fallback message for a failed job without error', () => {
            component.generate();
            poll$.next({ ...completedJob([]), status: 'failed' });
            expect(component.error()).toBe('Expression generation failed.');
        });

        it('deletes partial results when the job completes with the wrong candidate count', () => {
            component.generate();
            poll$.next(completedJob(['x-0', 'x-1'], KEYS.slice(0, 2)));
            expect(component.error()).toBe('Generation finished without results. Please try again.');
            expect(gallery.deleteImage.mock.calls.map(c => c[0])).toEqual(['x-0', 'x-1']);
            expect(component.hasCandidates()).toBe(false);
        });

        it('treats unparseable progress as no results', () => {
            component.generate();
            poll$.next({ ...completedJob([]), progress: 'garbage' });
            expect(component.error()).toBe('Generation finished without results. Please try again.');
            expect(gallery.deleteImage).not.toHaveBeenCalled();
        });

        it('surfaces poll errors', () => {
            component.generate();
            poll$.error(new Error('poll boom'));
            expect(component.error()).toBe('poll boom');
            expect(component.generating()).toBe(false);
        });

        it('uses a fallback message for poll errors without message', () => {
            component.generate();
            poll$.error({});
            expect(component.error()).toBe('Generation failed.');
        });
    });

    it('resume without a reference clears it and caps names at nine', () => {
        component.resume('job-9', { mode: 'candidates', expressions: [...KEYS, 'extra'] });
        expect(component.cells().length).toBe(9);
        expect(component.referenceImageId()).toBeNull();
    });

    describe('reference picker', () => {
        it('loads personality images newest first and picks one', () => {
            gallery.listImages.mockReturnValue(of({ results: [img('old', '2026-01-01T00:00:00Z'), img('new', '2026-02-01T00:00:00Z')], total_count: 2, page: 1 }));
            component.togglePicker();
            expect(gallery.listImages).toHaveBeenCalledWith(1, 60, { personalityId: 'p-1' });
            expect(component.pickerImages().map(i => i.id)).toEqual(['new', 'old']);
            render();
            expect(document.body.textContent).toContain('This personality');
            expect(buttonByText('Hide gallery')).toBeTruthy();
            (document.querySelector('button[aria-label="Use old.png as reference"]') as HTMLButtonElement).click();
            expect(component.referenceImageId()).toBe('old');
            expect(component.pickerOpen()).toBe(false);
        });

        it('switches to all images and back', () => {
            component.togglePicker();
            render();
            expect(document.body.textContent).toContain('No images yet');
            buttonByText('Show all images')!.click();
            expect(gallery.listImages).toHaveBeenLastCalledWith(1, 60, { personalityId: undefined });
            render();
            expect(document.body.textContent).toContain('All images');
            component.togglePickerScope();
            expect(gallery.listImages).toHaveBeenLastCalledWith(1, 60, { personalityId: 'p-1' });
        });

        it('shows a loading state and tolerates missing results', () => {
            const pending = new Subject<{ results?: FileAttachment[]; total_count: number; page: number }>();
            gallery.listImages.mockReturnValue(pending);
            component.togglePicker();
            render();
            expect(document.body.textContent).toContain('Loading gallery');
            pending.next({ total_count: 0, page: 1 });
            expect(component.pickerLoading()).toBe(false);
            expect(component.pickerImages()).toEqual([]);
        });

        it('closing the picker does not reload', () => {
            component.togglePicker();
            component.togglePicker();
            expect(component.pickerOpen()).toBe(false);
            expect(gallery.listImages).toHaveBeenCalledTimes(1);
        });

        it('reports gallery load failure', () => {
            gallery.listImages.mockReturnValue(throwError(() => new Error('x')));
            component.togglePicker();
            expect(component.error()).toBe('Failed to load image gallery.');
            expect(component.pickerLoading()).toBe(false);
        });

        it('opens the picker from the button and removes the reference', () => {
            render();
            buttonByText('Choose from gallery')!.click();
            expect(component.pickerOpen()).toBe(true);
            render();
            buttonByText('Remove')!.click();
            expect(component.referenceImageId()).toBeNull();
            render();
            expect(document.body.textContent).toContain('No reference');
        });
    });

    describe('reference upload', () => {
        it('ignores an empty selection', () => {
            component.onUpload(uploadEvent(null));
            expect(gallery.importImage).not.toHaveBeenCalled();
        });

        it('rejects non-image files', () => {
            component.onUpload(uploadEvent(new File(['x'], 'a.txt', { type: 'text/plain' })));
            expect(component.error()).toBe('Reference must be an image.');
            expect(gallery.importImage).not.toHaveBeenCalled();
        });

        it('uploads an image, selects it, and dedupes it into the picker list', () => {
            const pending = new Subject<FileAttachment>();
            gallery.importImage.mockReturnValue(pending);
            component.pickerImages.set([img('up-1', ''), img('b', '')]);
            component.onUpload(uploadEvent(new File(['x'], 'a.png', { type: 'image/png' })));
            expect(component.uploading()).toBe(true);
            render();
            expect(document.querySelector('ui-spinner')).toBeTruthy();
            pending.next(img('up-1', ''));
            expect(component.uploading()).toBe(false);
            expect(component.referenceImageId()).toBe('up-1');
            expect(component.pickerImages().map(i => i.id)).toEqual(['up-1', 'b']);
        });

        it('reports upload errors with server or fallback message', () => {
            gallery.importImage.mockReturnValue(throwError(() => ({ error: { message: 'too big' } })));
            component.onUpload(uploadEvent(new File(['x'], 'a.png', { type: 'image/png' })));
            expect(component.error()).toBe('too big');
            expect(component.uploading()).toBe(false);
            gallery.importImage.mockReturnValue(throwError(() => ({})));
            component.onUpload(uploadEvent(new File(['x'], 'a.png', { type: 'image/png' })));
            expect(component.error()).toBe('Failed to upload reference image.');
        });

        it('uploads through the file input change event', () => {
            gallery.importImage.mockReturnValue(of(img('up-2', '')));
            render();
            const input = document.querySelector('input[type="file"]') as HTMLInputElement;
            Object.defineProperty(input, 'files', { value: [new File(['x'], 'a.png', { type: 'image/png' })], configurable: true });
            input.dispatchEvent(new Event('change'));
            expect(component.referenceImageId()).toBe('up-2');
        });
    });

    it('swallows delete failures when discarding', () => {
        gallery.deleteImage.mockReturnValue(throwError(() => new Error('gone')));
        component.generate();
        poll$.next(completedJob(ids('a')));
        expect(() => component.discardAll()).not.toThrow();
        expect(component.hasCandidates()).toBe(false);
    });
});
