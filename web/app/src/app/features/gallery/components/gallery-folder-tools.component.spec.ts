import type { MockedObject } from 'vitest';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { of, throwError } from 'rxjs';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { GalleryViewService } from '../../../core/services/gallery-view.service';
import { GALLERY_LAYOUT_KEY } from '../helpers/gallery-list.helpers';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { Router } from '@angular/router';
import { PREVIEW_URL } from '../../web-preview/preview-site.config';
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
  let api: MockedObject<Pick<ImageGalleryService, 'listImages' | 'listFolders' | 'moveImages' | 'moveFolder' | 'deleteFiles'>>;
  let confirmation: { confirm: ReturnType<typeof vi.fn> };

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
      deleteFiles: vi.fn((ids: readonly string[]) => of({ deleted: [...ids], failed: [] })),
    } as unknown as typeof api;
    confirmation = { confirm: vi.fn().mockResolvedValue(true) };
    localStorage.removeItem(GALLERY_LAYOUT_KEY);
    await TestBed.configureTestingModule({
      imports: [GalleryFolderToolsComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: ImageGalleryService, useValue: api },
        { provide: ConfirmationService, useValue: confirmation },
        { provide: PREVIEW_URL, useValue: 'https://preview.example.test/index.html' },
      ],
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

    it('toggles between Show all and Show folders, so the label always says what the click does', () => {
      expect(buttons()).toContain('Show all');
      expect(buttons()).not.toContain('Show folders');

      button('Show all').click();
      fixture.detectChanges();

      expect(view.showAll()).toBe(true);
      expect(buttons()).toContain('Show folders');
      expect(buttons()).not.toContain('Show all');

      button('Show folders').click();
      fixture.detectChanges();

      expect(view.showAll()).toBe(false);
      expect(buttons()).toContain('Show all');
    });

    it('says so, and hides New folder, when searching or showing everything', () => {
      view.setFilters({ query: 'hrv' });
      fixture.detectChanges();
      expect(el().textContent).toContain('Searching every folder');
      expect(buttons()).not.toContain('New folder');

      view.setFilters({ query: '' });
      view.setShowAll(true);
      fixture.detectChanges();
      expect(el().textContent).toContain('Showing everything');
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
    const bar = () => el().querySelector('[aria-label="Bulk actions"]') as HTMLElement | null;
    const selectAll = () => el().querySelector('.folder-tools__select-all-box') as HTMLInputElement;
    const selectAllLabel = () => el().querySelector('.folder-tools__select-all')?.textContent?.trim();
    const actionButton = () => button('Choose action ▾');
    const option = (label: string) =>
      Array.from(el().querySelectorAll('[role="option"]')).find(o => o.textContent?.trim() === label) as HTMLButtonElement;

    it('shows the bar only in select mode, and cannot choose an action with nothing selected', () => {
      expect(bar()).toBeNull();

      button('Select').click();
      fixture.detectChanges();

      expect(bar()).not.toBeNull();
      expect(el().textContent).toContain('0 selected');
      expect(el().textContent).toContain('Select mode: click an image or file to pick it');
      expect(actionButton().disabled).toBe(true);
      expect(button('Clear ×').disabled).toBe(true);
    });

    it('has a select-all that becomes Unselect all, so nothing has to be unclicked by hand', () => {
      view.setSelectionMode(true);
      fixture.detectChanges();
      expect(selectAllLabel()).toBe('Select all');
      expect(selectAll().checked).toBe(false);

      selectAll().click();
      fixture.detectChanges();

      expect(el().textContent).toContain('2 selected');
      expect(selectAllLabel()).toBe('Unselect all');
      expect(selectAll().checked).toBe(true);
      expect(actionButton().disabled).toBe(false);
      expect(el().textContent).not.toContain('click an image or file to pick it');

      selectAll().click();
      fixture.detectChanges();

      expect(el().textContent).toContain('0 selected');
      expect(selectAllLabel()).toBe('Select all');
    });

    it('shows a partly selected list as indeterminate, and selects the rest from there', () => {
      view.setSelectionMode(true);
      view.toggleSelected('a');
      fixture.detectChanges();

      expect(selectAll().indeterminate).toBe(true);
      expect(selectAllLabel()).toBe('Select all');

      selectAll().click();
      fixture.detectChanges();

      expect(view.selectedCount()).toBe(2);
    });

    it('clears the selection with Clear, and leaves select mode with Done', () => {
      view.setSelectionMode(true);
      view.selectAllShown();
      fixture.detectChanges();

      button('Clear ×').click();
      fixture.detectChanges();
      expect(view.selectedCount()).toBe(0);
      expect(view.selectionMode()).toBe(true);

      button('Done').click();
      fixture.detectChanges();
      expect(view.selectionMode()).toBe(false);
      expect(bar()).toBeNull();
    });

    it('moves the selection from the Choose action menu', () => {
      view.setSelectionMode(true);
      view.selectAllShown();
      fixture.detectChanges();
      expect(el().querySelector('[role="listbox"][aria-label="Bulk actions"]')).toBeNull();

      actionButton().click();
      fixture.detectChanges();
      expect(actionButton().getAttribute('aria-expanded')).toBe('true');

      option('Move to folder…').click();
      fixture.detectChanges();

      expect(component.movingImages()).toBe(true);
      expect(el().querySelector('[role="listbox"][aria-label="Bulk actions"]')).toBeNull();
    });

    it('names the count in the bulk actions, for screen readers', () => {
      view.setSelectionMode(true);
      view.toggleSelected('a');
      fixture.detectChanges();
      actionButton().click();
      fixture.detectChanges();

      expect(actionButton().getAttribute('aria-label')).toBe('Choose action for 1 selected');
      expect(option('Move to folder…').getAttribute('aria-label')).toBe('Move 1 selected item to a folder');
      expect(option('Delete…').getAttribute('aria-label')).toBe('Delete 1 selected item');
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

  describe('dropping on the path', () => {
    /** A drag event carrying just what the gallery reads. */
    const dragEvent = (type: string): Event => {
      const event = new Event(type, { bubbles: true, cancelable: true });
      Object.defineProperty(event, 'dataTransfer', { value: { types: ['application/x-whatiff-gallery'], dropEffect: '' } });
      return event;
    };
    const crumb = (label: string) =>
      Array.from(el().querySelectorAll('.folder-tools__crumb')).find(c => c.textContent?.trim() === label) as HTMLElement;

    beforeEach(() => {
      view.openFolder('charts/oura');
      fixture.detectChanges();
    });

    it('takes images dropped on an earlier step of the path, and highlights it while over', () => {
      view.beginImageDrag('a');
      const over = dragEvent('dragover');

      crumb('charts').dispatchEvent(over);
      fixture.detectChanges();

      expect(over.defaultPrevented).toBe(true);
      expect(crumb('charts').classList).toContain('folder-tools__crumb--drop');

      crumb('charts').dispatchEvent(dragEvent('dragleave'));
      fixture.detectChanges();
      expect(crumb('charts').classList).not.toContain('folder-tools__crumb--drop');
    });

    it('moves the dragged images to that step when they are dropped', async () => {
      view.beginImageDrag('a');

      crumb('Gallery').dispatchEvent(dragEvent('drop'));
      await flush(fixture);

      expect(api.moveImages).toHaveBeenCalledWith(['a'], '');
    });

    it('moves a dragged folder up to a step, keeping its name', async () => {
      view.beginFolderDrag('charts/oura/hrv');

      crumb('charts').dispatchEvent(dragEvent('drop'));
      await flush(fixture);

      expect(api.moveFolder).toHaveBeenCalledWith('charts/oura/hrv', 'charts/hrv');
    });

    it('does not take a drop on the folder you are already in, which is not a step you can drop on', () => {
      view.beginImageDrag('a');
      const over = dragEvent('dragover');

      expect(component.acceptsDropOnCrumb('charts/oura')).toBe(false);
      el().querySelector('.folder-tools__crumb--current')?.dispatchEvent(over);

      expect(over.defaultPrevented).toBe(false);
    });

    it('ignores a drop when nothing from the gallery is being dragged', async () => {
      crumb('charts').dispatchEvent(dragEvent('dragover'));
      crumb('charts').dispatchEvent(dragEvent('drop'));
      await flush(fixture);

      expect(api.moveImages).not.toHaveBeenCalled();
      expect(api.moveFolder).not.toHaveBeenCalled();
    });
  });

  describe('moving one image from its popup', () => {
    it('opens the move dialog for just that image, without any selection', () => {
      view.requestMove(['b']);
      fixture.detectChanges();

      expect(component.moveOpen()).toBe(true);
      expect(component.moveIds()).toEqual(['b']);
      expect(view.selectedCount()).toBe(0);
    });

    it('moves that image and closes the dialog', async () => {
      view.requestMove(['b']);

      await component.submitMoveImages('charts/oura');
      await flush(fixture);

      expect(api.moveImages).toHaveBeenCalledWith(['b'], 'charts/oura');
      expect(view.moveRequest()).toBeNull();
      expect(component.moveOpen()).toBe(false);
    });

    it('closes without moving when cancelled, and goes back to using the selection', () => {
      view.requestMove(['b']);
      component.closeMoveImages();
      expect(view.moveRequest()).toBeNull();

      view.setSelectionMode(true);
      view.toggleSelected('a');
      expect(component.moveIds()).toEqual(['a']);
    });

    it('keeps the dialog open when the move fails', async () => {
      api.moveImages.mockReturnValue(throwError(() => ({ error: { error: 'nope' } })));
      view.requestMove(['b']);

      await component.submitMoveImages('x');

      expect(component.moveOpen()).toBe(true);
      expect(view.folderError()).toBe('nope');
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

  describe('in the list view', () => {
    const bar = () => el().querySelector('ui-bulk-action-bar') as HTMLElement | null;

    beforeEach(() => {
      view.setLayout('list');
      fixture.detectChanges();
    });

    afterEach(() => localStorage.removeItem(GALLERY_LAYOUT_KEY));

    it('has no Select mode, and shows the bar (without its own select-all or Done) once something is ticked', () => {
      expect(buttons()).not.toContain('Select');
      expect(bar()).toBeNull();

      view.toggleSelected('a');
      fixture.detectChanges();

      expect(bar()?.textContent).toContain('1 selected');
      expect(el().querySelector('.folder-tools__select-all')).toBeNull();
      expect(buttons()).not.toContain('Done');
    });

    it('says when picks only cover the loaded part of a longer list', () => {
      view.totalCount.set(90);
      view.toggleSelected('a');
      fixture.detectChanges();

      expect(bar()?.textContent).toContain('Picked from the 2 loaded; 88 more not loaded yet');
    });
  });

  describe('deleting the selection', () => {
    beforeEach(() => {
      view.setSelectionMode(true);
      view.selectAllShown();
      fixture.detectChanges();
    });

    it('asks first, with the count, then deletes every selected file', async () => {
      await component.deleteSelection();
      await flush(fixture);

      expect(confirmation.confirm).toHaveBeenCalledWith(
        expect.objectContaining({ title: 'Delete 2 items?', confirmText: 'Delete', type: 'danger' }),
      );
      expect(api.deleteFiles).toHaveBeenCalledWith(['a', 'b']);
      expect(view.images()).toEqual([]);
      expect(view.selectedCount()).toBe(0);
      expect(el().querySelector('[role="status"].folder-tools__bulk-result')?.textContent).toContain('Deleted 2 items.');
    });

    it('deletes nothing when the confirmation is dismissed', async () => {
      confirmation.confirm.mockResolvedValue(false);

      await component.deleteSelection();

      expect(api.deleteFiles).not.toHaveBeenCalled();
      expect(view.selectedCount()).toBe(2);
    });

    it('reports a partial failure and keeps what failed selected for a retry', async () => {
      api.deleteFiles.mockReturnValue(of({ deleted: ['a'], failed: [{ id: 'b', error: 'could not delete the file' }] }));

      await component.deleteSelection();
      await flush(fixture);

      expect(view.images().map(row => row.id)).toEqual(['b']);
      expect([...view.selectedIds()]).toEqual(['b']);
      const alert = el().querySelector('[role="alert"].folder-tools__bulk-result');
      expect(alert?.textContent).toContain('Deleted 1 of 2 items. Could not delete “b”');
    });

    it('runs from the Choose action menu', async () => {
      button('Choose action ▾').click();
      fixture.detectChanges();
      (Array.from(el().querySelectorAll('[role="option"]')).find(o => o.textContent?.trim() === 'Delete…') as HTMLElement).click();
      await flush(fixture);

      expect(confirmation.confirm).toHaveBeenCalled();
      expect(api.deleteFiles).toHaveBeenCalledWith(['a', 'b']);
    });
  });

  describe('Open as site', () => {
    it('opens the folder’s index.html, where the folder has one', async () => {
      const navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
      expect(buttons()).not.toContain('Open as site');
      const page = { ...image('page'), name: 'index.html', file_type: 'text/html', folder: 'charts' };
      api.listImages.mockReturnValue(of({ results: [page, { ...image('c'), folder: 'charts' }], total_count: 2, page: 1 }));
      view.openFolder('charts');
      await flush(fixture);
      button('Open as site').click();
      expect(navigate).toHaveBeenCalledWith([], { queryParams: { file: 'page' }, queryParamsHandling: 'merge' });
    });

    it('is not offered at the top level or in a folder without one', async () => {
      api.listImages.mockReturnValue(of({ results: [{ ...image('c'), folder: 'charts' }], total_count: 1, page: 1 }));
      view.openFolder('charts');
      await flush(fixture);
      expect(buttons()).not.toContain('Open as site');
    });
  });
});
