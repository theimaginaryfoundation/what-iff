import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';

import { GALLERY_DRAG_TYPE, GalleryDrag } from '../helpers/gallery-dnd.helpers';
import { GalleryTileVm } from '../helpers/gallery-vm.helpers';
import { GalleryFolderTileComponent } from './gallery-folder-tile.component';
import { GalleryGridComponent } from './gallery-grid.component';
import { GalleryTileComponent } from './gallery-tile.component';

/** A drag event with just the parts the gallery reads (jsdom has no DataTransfer). */
function dragEvent(
  type: string,
  types: string[] = [GALLERY_DRAG_TYPE],
): { event: Event; data: Record<string, string>; dropEffect: () => string } {
  const data: Record<string, string> = {};
  let dropEffect = '';
  const event = new Event(type, { bubbles: true, cancelable: true });
  Object.defineProperty(event, 'dataTransfer', {
    value: {
      types,
      effectAllowed: '',
      get dropEffect() {
        return dropEffect;
      },
      set dropEffect(value: string) {
        dropEffect = value;
      },
      setData: (key: string, value: string) => {
        data[key] = value;
      },
    },
  });
  return { event, data, dropEffect: () => dropEffect };
}

const tile: GalleryTileVm = {
  id: 'img-1',
  name: 'a.png',
  createdAt: '2026-05-01T00:00:00Z',
  personalityId: null,
  personalityName: null,
  personalityNames: [],
  source: 'uploaded',
  sourceLabel: 'Imported',
  folder: '',
  isFile: false,
  badge: '',
  thumbnailUrl: '/thumb',
  fullUrl: '/full',
};

describe('gallery drag and drop components', () => {
  beforeEach(() => {
    TestBed.configureTestingModule({ providers: [provideZonelessChangeDetection(), provideHttpClient(withXhr())] });
  });

  describe('image tile', () => {
    let fixture: ComponentFixture<GalleryTileComponent>;

    beforeEach(() => {
      fixture = TestBed.createComponent(GalleryTileComponent);
      fixture.componentRef.setInput('tile', tile);
      fixture.detectChanges();
    });

    it('can be dragged, and starts a gallery drag carrying its id', () => {
      const started: string[] = [];
      fixture.componentInstance.dragImage.subscribe(id => started.push(id));
      const article = fixture.nativeElement.querySelector('article') as HTMLElement;
      expect(article.getAttribute('draggable')).toBe('true');

      const { event, data } = dragEvent('dragstart', []);
      article.dispatchEvent(event);

      expect(started).toEqual(['img-1']);
      expect(JSON.parse(data[GALLERY_DRAG_TYPE])).toEqual({ kind: 'images', ids: ['img-1'] });
    });

    it('says when the drag ends', () => {
      let ended = 0;
      fixture.componentInstance.dragEnd.subscribe(() => ended++);
      (fixture.nativeElement.querySelector('article') as HTMLElement).dispatchEvent(dragEvent('dragend').event);
      expect(ended).toBe(1);
    });
  });

  describe('folder tile', () => {
    let fixture: ComponentFixture<GalleryFolderTileComponent>;
    let component: GalleryFolderTileComponent;
    const article = () => fixture.nativeElement.querySelector('article') as HTMLElement;

    beforeEach(() => {
      fixture = TestBed.createComponent(GalleryFolderTileComponent);
      component = fixture.componentInstance;
      fixture.componentRef.setInput('folder', { path: 'charts/oura', name: 'oura', count: 2 });
      fixture.detectChanges();
    });

    it('accepts a drag over it only when it can take what is dragged, and highlights', () => {
      const { event, dropEffect } = dragEvent('dragover');
      fixture.componentRef.setInput('acceptsDrop', true);
      fixture.detectChanges();

      article().dispatchEvent(event);
      fixture.detectChanges();

      expect(event.defaultPrevented).toBe(true);
      expect(dropEffect()).toBe('move');
      expect(article().classList).toContain('folder--drop');
    });

    it('ignores a drag it cannot take, and one that is not the gallery’s (a file from the desktop)', () => {
      const refused = dragEvent('dragover');
      article().dispatchEvent(refused.event);
      expect(refused.event.defaultPrevented).toBe(false);

      fixture.componentRef.setInput('acceptsDrop', true);
      fixture.detectChanges();
      const foreign = dragEvent('dragover', ['Files']);
      article().dispatchEvent(foreign.event);

      expect(foreign.event.defaultPrevented).toBe(false);
      expect(article().classList).not.toContain('folder--drop');
    });

    it('stops highlighting when the drag leaves', () => {
      fixture.componentRef.setInput('acceptsDrop', true);
      fixture.detectChanges();
      article().dispatchEvent(dragEvent('dragover').event);
      fixture.detectChanges();

      article().dispatchEvent(dragEvent('dragleave').event);
      fixture.detectChanges();

      expect(article().classList).not.toContain('folder--drop');
    });

    it('stays highlighted when the drag just moves onto the folder’s own label', () => {
      fixture.componentRef.setInput('acceptsDrop', true);
      fixture.detectChanges();
      article().dispatchEvent(dragEvent('dragover').event);

      const leave = new MouseEvent('dragleave', { bubbles: true, relatedTarget: article().querySelector('.folder__name') });
      article().dispatchEvent(leave);
      fixture.detectChanges();

      expect(article().classList).toContain('folder--drop');
    });

    it('emits its path when something is dropped on it', () => {
      const dropped: string[] = [];
      component.dropped.subscribe(path => dropped.push(path));
      fixture.componentRef.setInput('acceptsDrop', true);
      fixture.detectChanges();

      const { event } = dragEvent('drop');
      article().dispatchEvent(event);

      expect(dropped).toEqual(['charts/oura']);
      expect(event.defaultPrevented).toBe(true);
    });

    it('does not take a drop it refused', () => {
      const dropped: string[] = [];
      component.dropped.subscribe(path => dropped.push(path));

      article().dispatchEvent(dragEvent('drop').event);

      expect(dropped).toEqual([]);
    });

    it('can itself be dragged, as a folder', () => {
      const started: string[] = [];
      component.dragFolder.subscribe(path => started.push(path));
      expect(article().getAttribute('draggable')).toBe('true');

      const { event, data } = dragEvent('dragstart', []);
      article().dispatchEvent(event);

      expect(started).toEqual(['charts/oura']);
      expect(JSON.parse(data[GALLERY_DRAG_TYPE])).toEqual({ kind: 'folder', path: 'charts/oura' });
    });
  });

  describe('grid', () => {
    let fixture: ComponentFixture<GalleryGridComponent>;

    beforeEach(() => {
      fixture = TestBed.createComponent(GalleryGridComponent);
      fixture.componentRef.setInput('folderTiles', [
        { path: 'charts', name: 'charts', count: 3 },
        { path: 'art', name: 'art', count: 1 },
      ]);
      fixture.componentRef.setInput('tiles', [tile]);
      fixture.detectChanges();
    });

    const folderTile = (name: string) =>
      Array.from(fixture.nativeElement.querySelectorAll('app-gallery-folder-tile')).find(
        el => (el as HTMLElement).querySelector('.folder__name')?.textContent?.trim() === name,
      ) as HTMLElement;

    it('tells each folder whether it takes the drag, so a folder is not offered itself', () => {
      const drag: GalleryDrag = { kind: 'folder', path: 'charts' };
      fixture.componentRef.setInput('drag', drag);
      fixture.detectChanges();

      folderTile('charts').querySelector('article')?.dispatchEvent(dragEvent('dragover').event);
      const art = dragEvent('dragover');
      folderTile('art').querySelector('article')?.dispatchEvent(art.event);

      expect(art.event.defaultPrevented).toBe(true);
      expect(fixture.componentInstance.acceptsDrop('charts')).toBe(false);
      expect(fixture.componentInstance.acceptsDrop('art')).toBe(true);
    });

    it('offers nothing to drop on when nothing is being dragged', () => {
      expect(fixture.componentInstance.acceptsDrop('art')).toBe(false);
    });

    it('passes up what is dragged and what it is dropped on', () => {
      const events: string[] = [];
      const grid = fixture.componentInstance;
      grid.dragImage.subscribe(id => events.push(`image:${id}`));
      grid.dragFolder.subscribe(path => events.push(`folder:${path}`));
      grid.dragEnd.subscribe(() => events.push('end'));
      grid.dropOnFolder.subscribe(path => events.push(`drop:${path}`));
      fixture.componentRef.setInput('drag', { kind: 'images', ids: ['img-1'] } satisfies GalleryDrag);
      fixture.detectChanges();

      (fixture.nativeElement.querySelector('app-gallery-tile article') as HTMLElement).dispatchEvent(dragEvent('dragstart', []).event);
      (fixture.nativeElement.querySelector('app-gallery-tile article') as HTMLElement).dispatchEvent(dragEvent('dragend').event);
      folderTile('charts').querySelector('article')?.dispatchEvent(dragEvent('dragstart', []).event);
      folderTile('art').querySelector('article')?.dispatchEvent(dragEvent('drop').event);

      expect(events).toEqual(['image:img-1', 'end', 'folder:charts', 'drop:art']);
    });
  });
});
