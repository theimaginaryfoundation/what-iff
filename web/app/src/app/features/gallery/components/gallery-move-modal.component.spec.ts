import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { GalleryMoveModalComponent } from './gallery-move-modal.component';

describe('GalleryMoveModalComponent', () => {
  let fixture: ComponentFixture<GalleryMoveModalComponent>;
  let component: GalleryMoveModalComponent;

  function set(inputs: Record<string, unknown>): void {
    for (const [name, value] of Object.entries(inputs)) {
      fixture.componentRef.setInput(name, value);
    }
    fixture.detectChanges();
  }

  const rows = () => Array.from(document.querySelectorAll<HTMLElement>('.gallery-move__option')).map(el => el.textContent?.trim());

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [GalleryMoveModalComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();
    fixture = TestBed.createComponent(GalleryMoveModalComponent);
    component = fixture.componentInstance;
  });

  describe('moving images', () => {
    beforeEach(() => set({ open: true, mode: 'images', imageCount: 3, folders: ['art', 'charts', 'charts/oura'], currentFolder: 'art' }));

    it('titles itself with how many images are moving and starts at the folder they are in', () => {
      expect(component.title()).toBe('Move 3 images');
      expect(component.path()).toBe('art');
      set({ imageCount: 1 });
      expect(component.title()).toBe('Move 1 image');
    });

    it('lists the top level and every folder to pick from', () => {
      expect(rows()).toEqual(['⌂Gallery', '▸art', '▸charts', '▸charts / oura']);
    });

    it('picks a folder from the list and submits its path', () => {
      const submitted: string[] = [];
      component.submitPath.subscribe(path => submitted.push(path));

      component.pick(component.destinations()[3]);
      component.submit();

      expect(submitted).toEqual(['charts/oura']);
    });

    it('moves to the top level when the gallery row is picked', () => {
      const submitted: string[] = [];
      component.submitPath.subscribe(path => submitted.push(path));

      component.pick(component.destinations()[0]);
      component.submit();

      expect(submitted).toEqual(['']);
    });

    it('creates a folder by typing a path, normalizing what was typed', () => {
      const submitted: string[] = [];
      component.submitPath.subscribe(path => submitted.push(path));

      component.path.set('  Daily Graphs / Oura ');
      expect(component.resultingPath()).toBe('daily graphs/oura');
      component.submit();

      expect(submitted).toEqual(['daily graphs/oura']);
    });

    it('will not submit a path the server would refuse, or while busy, or with nothing selected', () => {
      const submitted: string[] = [];
      component.submitPath.subscribe(path => submitted.push(path));

      component.path.set('a/../b');
      expect(component.pathError()).toContain('not allowed');
      expect(component.canSubmit()).toBe(false);
      component.submit();

      component.path.set('fine');
      set({ submitting: true });
      expect(component.canSubmit()).toBe(false);
      expect(component.submitLabel()).toBe('Moving…');

      set({ submitting: false, imageCount: 0 });
      expect(component.canSubmit()).toBe(false);

      expect(submitted).toEqual([]);
    });

    it('resets to where the images are each time it opens', () => {
      component.path.set('something typed');
      set({ open: false });
      set({ open: true });
      expect(component.path()).toBe('art');
    });
  });

  describe('moving a folder', () => {
    beforeEach(() =>
      set({ open: true, mode: 'folder', folderPath: 'charts/oura', folders: ['art', 'charts', 'charts/oura', 'charts/oura/hrv', 'zeta'] }),
    );

    it('starts with the folder’s own path, which is not yet a change', () => {
      expect(component.title()).toBe('Rename or move “oura”');
      expect(component.path()).toBe('charts/oura');
      expect(component.canSubmit()).toBe(false);
    });

    it('does not offer the folder itself or anything inside it as a place to move it', () => {
      expect(rows()).toEqual(['⌂Gallery', '▸art', '▸charts', '▸zeta']);
    });

    it('moving into a picked folder keeps the folder’s name', () => {
      const submitted: string[] = [];
      component.submitPath.subscribe(path => submitted.push(path));

      component.pick(component.destinations().find(d => d.folder === 'zeta') as never);
      expect(component.path()).toBe('zeta/oura');
      component.submit();

      expect(submitted).toEqual(['zeta/oura']);
    });

    it('picking the gallery moves it up to the top level', () => {
      component.pick(component.destinations()[0]);
      expect(component.path()).toBe('oura');
      expect(component.canSubmit()).toBe(true);
    });

    it('renames by editing the path', () => {
      const submitted: string[] = [];
      component.submitPath.subscribe(path => submitted.push(path));

      component.path.set('charts/Heart Rate');
      component.submit();

      expect(submitted).toEqual(['charts/heart rate']);
    });

    it('refuses an empty path, a move into itself, and an unchanged path', () => {
      component.path.set('');
      expect(component.canSubmit()).toBe(false);
      component.path.set('charts/oura/inside');
      expect(component.canSubmit()).toBe(false);
      component.path.set('  Charts/Oura/ ');
      expect(component.canSubmit()).toBe(false);
      component.path.set('charts/other');
      expect(component.canSubmit()).toBe(true);
    });
  });

  it('shows why the server refused', () => {
    set({ open: true, mode: 'images', imageCount: 1, serverError: 'A folder cannot be moved into itself' });
    expect(document.body.textContent).toContain('A folder cannot be moved into itself');
  });
});
