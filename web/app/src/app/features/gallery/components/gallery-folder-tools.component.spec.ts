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
    const bar = () => el().querySelector('[aria-label="Bulk actions"]') as HTMLElement | null;
    const selectAll = () => el().querySelector('.folder-tools__select-all-box') as HTMLInputElement;
    const selectAllLabel = () => el().querySelector('.folder-tools__select-all')?.textContent?.trim();
    const actionButton = () => button('Choose action ▾');

    it('shows the bar only in select mode, and cannot choose an action with nothing selected', () => {
      expect(bar()).toBeNull();

      button('Select').click();
      fixture.detectChanges();

      expect(bar()).not.toBeNull();
      expect(el().textContent).toContain('0 selected');
      expect(el().textContent).toContain('Select mode: click an image to pick it');
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
      expect(el().textContent).not.toContain('click an image to pick it');

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

      el()
        .querySelector('[role="option"]')
        ?.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
      fixture.detectChanges();

      expect(component.movingImages()).toBe(true);
      expect(component.actionMenuOpen()).toBe(false);
      expect(el().querySelector('[role="listbox"][aria-label="Bulk actions"]')).toBeNull();
    });

    it('closes the action menu on Escape', () => {
      view.setSelectionMode(true);
      view.selectAllShown();
      fixture.detectChanges();
      actionButton().click();
      fixture.detectChanges();

      el()
        .querySelector('.folder-tools__bulk-action')
        ?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));

      expect(component.actionMenuOpen()).toBe(false);
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
});
