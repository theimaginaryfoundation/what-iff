import type { MockedObject } from 'vitest';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { of, throwError } from 'rxjs';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { GalleryViewService } from '../../../core/services/gallery-view.service';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { GalleryFolderToolsComponent } from './gallery-folder-tools.component';

function image(id: string): FileAttachment {
  return { id, user_id: 'u', name: id, file_type: 'image/png', created_at: '2026-05-01T00:00:00Z' };
}

async function flush(fixture: ComponentFixture<unknown>): Promise<void> {
  // The move actions are promise-based, which zoneless change detection does not wait for.
  for (let i = 0; i < 3; i++) {
    await new Promise(resolve => setTimeout(resolve, 0));
  }
  fixture.detectChanges();
}

describe('GalleryFolderToolsComponent', () => {
  let fixture: ComponentFixture<GalleryFolderToolsComponent>;
  let component: GalleryFolderToolsComponent;
  let view: GalleryViewService;
  let api: MockedObject<Pick<ImageGalleryService, 'listImages' | 'listFolders' | 'moveImages' | 'moveFolder'>>;

  const el = () => fixture.nativeElement as HTMLElement;
  const buttons = () => Array.from(el().querySelectorAll('button')).map(b => b.textContent?.trim());
  const button = (label: string) =>
    Array.from(el().querySelectorAll('button')).find(b => b.textContent?.trim() === label) as HTMLButtonElement;

  beforeEach(async () => {
    api = {
      listImages: vi.fn().mockReturnValue(of({ results: [image('a'), image('b')], total_count: 2, page: 1 })),
      listFolders: vi.fn().mockReturnValue(
        of([
          { path: 'charts', count: 1 },
          { path: 'charts/oura', count: 2 },
        ]),
      ),
      moveImages: vi.fn().mockReturnValue(of(2)),
      moveFolder: vi.fn().mockReturnValue(of(3)),
    } as unknown as typeof api;
    await TestBed.configureTestingModule({
      imports: [GalleryFolderToolsComponent],
      providers: [provideZonelessChangeDetection(), { provide: ImageGalleryService, useValue: api }],
    }).compileComponents();
    view = TestBed.inject(GalleryViewService);
    view.loadInitial();
    view.loadFolders();
    fixture = TestBed.createComponent(GalleryFolderToolsComponent);
    component = fixture.componentInstance;
    fixture.detectChanges();
  });

  describe('where you are', () => {
    it('shows the path as steps, with the current folder as plain text', () => {
      view.openFolder('charts/oura');
      fixture.detectChanges();

      expect(el().querySelector('nav')?.textContent).toContain('Gallery');
      expect(buttons()).toContain('Gallery');
      expect(buttons()).toContain('charts');
      expect(buttons()).not.toContain('oura');
      expect(el().querySelector('[aria-current="page"]')?.textContent?.trim()).toBe('oura');
    });

    it('goes up when an earlier step is clicked', () => {
      view.openFolder('charts/oura');
      fixture.detectChanges();

      button('charts').click();

      expect(view.currentFolder()).toBe('charts');
    });

    it('says so, and hides New folder, when searching or showing everything', () => {
      view.setFilters({ query: 'hrv' });
      fixture.detectChanges();
      expect(el().textContent).toContain('Searching every folder');
      expect(buttons()).not.toContain('New folder');

      view.setFilters({ query: '' });
      view.setShowAll(true);
      fixture.detectChanges();
      expect(el().textContent).toContain('Showing every image');
      expect(el().querySelector('nav')).toBeNull();
    });
  });

  describe('new folder', () => {
    it('opens a form, creates the folder under the open one and goes into it', () => {
      view.openFolder('charts');
      fixture.detectChanges();
      button('New folder').click();
      fixture.detectChanges();
      expect(el().textContent).toContain('New folder in charts');

      component.newName.set('Sleep');
      component.createFolder();
      fixture.detectChanges();

      expect(view.currentFolder()).toBe('charts/sleep');
      expect(el().querySelector('form')).toBeNull();
    });

    it('keeps the form open and says why when the name is refused', () => {
      component.startCreating();
      fixture.detectChanges();
      component.newName.set('../up');
      component.createFolder();
      fixture.detectChanges();

      expect(el().querySelector('form')).not.toBeNull();
      expect(el().textContent).toContain('not allowed');
      expect(view.currentFolder()).toBe('');
    });

    it('can be cancelled', () => {
      component.startCreating();
      fixture.detectChanges();
      button('Cancel').click();
      fixture.detectChanges();
      expect(el().querySelector('form')).toBeNull();
    });
  });

  describe('selecting and moving images', () => {
    it('shows the selection bar only in select mode, and cannot move nothing', () => {
      expect(el().querySelector('[aria-label="Selected images"]')).toBeNull();

      button('Select').click();
      fixture.detectChanges();

      expect(el().querySelector('[aria-label="Selected images"]')).not.toBeNull();
      expect(el().textContent).toContain('0 selected');
      expect(button('Move to folder…').disabled).toBe(true);
    });

    it('selects everything shown and counts it', () => {
      button('Select').click();
      fixture.detectChanges(); // the selection bar appears
      button('Select all shown').click();
      fixture.detectChanges();
      expect(el().textContent).toContain('2 selected');
      expect(button('Move to folder…').disabled).toBe(false);
    });

    it('moves the selected images to the chosen folder, then closes the dialog and leaves select mode alone', async () => {
      view.setSelectionMode(true);
      view.selectAllShown();
      fixture.detectChanges();

      component.openMoveImages();
      fixture.detectChanges();
      expect(component.movingImages()).toBe(true);
      await component.submitMoveImages('charts/oura');
      await flush(fixture);

      expect(api.moveImages).toHaveBeenCalledWith(['a', 'b'], 'charts/oura');
      expect(component.movingImages()).toBe(false);
      expect(view.images()).toEqual([]);
    });

    it('keeps the dialog open when the move fails', async () => {
      api.moveImages.mockReturnValue(throwError(() => ({ error: { error: 'nope' } })));
      view.setSelectionMode(true);
      view.selectAllShown();
      component.openMoveImages();

      await component.submitMoveImages('x');

      expect(component.movingImages()).toBe(true);
      expect(view.folderError()).toBe('nope');
      expect(component.submitting()).toBe(false);
    });

    it('does nothing when asked to move an empty selection', () => {
      component.openMoveImages();
      expect(component.movingImages()).toBe(false);
    });
  });

  describe('renaming or moving a folder', () => {
    it('moves it and closes the dialog', async () => {
      view.startEditingFolder('charts');
      fixture.detectChanges();

      await component.submitMoveFolder('archive/charts');
      await flush(fixture);

      expect(api.moveFolder).toHaveBeenCalledWith('charts', 'archive/charts');
      expect(view.folderEditing()).toBeNull();
    });

    it('keeps the dialog open and shows the reason when it is refused', async () => {
      api.moveFolder.mockReturnValue(throwError(() => ({ error: { error: 'nope' } })));
      view.startEditingFolder('charts');

      await component.submitMoveFolder('other');

      expect(view.folderEditing()).toBe('charts');
      expect(view.folderError()).toBe('nope');
    });
  });
});
