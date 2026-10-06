import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';

import { GalleryGridComponent } from './gallery-grid.component';
import { GalleryTileVm } from '../helpers/gallery-vm.helpers';

function tile(id: string, folder = ''): GalleryTileVm {
  return {
    id,
    name: `${id}.png`,
    createdAt: '2026-05-01T00:00:00Z',
    personalityId: null,
    personalityName: null,
    personalityNames: [],
    source: 'uploaded',
    sourceLabel: 'Imported',
    folder,
    isFile: false,
    badge: '',
    thumbnailUrl: `/thumb/${id}`,
    fullUrl: `/full/${id}`,
  };
}

describe('GalleryGridComponent folders', () => {
  let fixture: ComponentFixture<GalleryGridComponent>;
  let component: GalleryGridComponent;

  const el = () => fixture.nativeElement as HTMLElement;
  const set = (inputs: Record<string, unknown>) => {
    for (const [name, value] of Object.entries(inputs)) {
      fixture.componentRef.setInput(name, value);
    }
    fixture.detectChanges();
  };

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [GalleryGridComponent],
      providers: [provideZonelessChangeDetection(), provideHttpClient(withXhr())],
    }).compileComponents();
    fixture = TestBed.createComponent(GalleryGridComponent);
    component = fixture.componentInstance;
  });

  it('shows folders before the images, each with its count', () => {
    set({
      folderTiles: [
        { path: 'charts', name: 'charts', count: 7 },
        { path: 'art', name: 'art', count: 1 },
      ],
      tiles: [tile('a')],
    });

    const cells = Array.from(el().querySelectorAll('[role="listitem"]'));
    expect(cells.length).toBe(3);
    expect(cells[0].textContent).toContain('charts');
    expect(cells[0].textContent).toContain('7 items');
    expect(cells[1].textContent).toContain('1 item');
    expect(cells[1].textContent).not.toContain('1 items');
    expect(cells[2].querySelector('.tile__button')).not.toBeNull();
  });

  it('shows a grid, not the empty state, when there are only folders', () => {
    set({ folderTiles: [{ path: 'charts', name: 'charts', count: 2 }], tiles: [] });
    expect(el().querySelector('.gallery-grid')).not.toBeNull();
    expect(el().textContent).not.toContain('No images match');
  });

  it('opens a folder and offers to rename or move it', () => {
    const opened: string[] = [];
    const edited: string[] = [];
    component.openFolder.subscribe(path => opened.push(path));
    component.editFolder.subscribe(path => edited.push(path));
    set({ folderTiles: [{ path: 'charts/oura', name: 'oura', count: 2 }], tiles: [] });

    (el().querySelector('.folder__open') as HTMLButtonElement).click();
    (el().querySelector('.folder__menu') as HTMLButtonElement).click();

    expect(opened).toEqual(['charts/oura']);
    expect(edited).toEqual(['charts/oura']);
  });

  describe('select mode', () => {
    it('picks an image instead of opening it, and shows which are picked', () => {
      const toggled: string[] = [];
      const opened: string[] = [];
      component.toggleSelect.subscribe(id => toggled.push(id));
      component.openImage.subscribe(id => opened.push(id));
      set({ tiles: [tile('a'), tile('b')], selectable: true, selectedIds: new Set(['b']) });

      const checks = Array.from(el().querySelectorAll<HTMLElement>('.tile__check'));
      expect(checks.map(c => c.getAttribute('aria-checked'))).toEqual(['false', 'true']);

      (el().querySelector('.tile__button') as HTMLButtonElement).click(); // the image itself
      checks[1].click(); // its checkbox

      expect(toggled).toEqual(['a', 'b']);
      expect(opened).toEqual([]);
    });

    it('hides delete while selecting, and opens images normally otherwise', () => {
      const opened: string[] = [];
      component.openImage.subscribe(id => opened.push(id));
      set({ tiles: [tile('a')], selectable: false });

      expect(el().querySelector('.tile__delete')).not.toBeNull();
      expect(el().querySelector('.tile__check')).toBeNull();
      (el().querySelector('.tile__button') as HTMLButtonElement).click();
      expect(opened).toEqual(['a']);

      set({ selectable: true });
      expect(el().querySelector('.tile__delete')).toBeNull();
    });
  });

  it('labels each image with its folder in the flat and search views only', () => {
    set({ tiles: [tile('a', 'charts/oura'), tile('b')], showFolders: false });
    expect(el().querySelector('.tile__folder')).toBeNull();

    set({ showFolders: true });
    const labels = Array.from(el().querySelectorAll('.tile__folder')).map(l => l.textContent?.trim());
    expect(labels).toEqual(['charts/oura']);
  });

  it('says what is wrong when there is nothing to show', () => {
    set({ tiles: [], folderTiles: [] });
    expect(el().textContent).toContain('No images match these filters yet.');

    set({ emptyMessage: 'This folder is empty.' });
    expect(el().textContent).toContain('This folder is empty.');
  });
});
