import type { MockedObject } from 'vitest';
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { of, throwError } from 'rxjs';

import { FileAttachment } from '../models/file-attachment.model';
import { PaginatedResponse } from '../models/common.model';
import { ImageGalleryService } from './image-gallery.service';
import { GalleryViewService } from './gallery-view.service';

function image(id: string, folder?: string): FileAttachment {
  return { id, user_id: 'u', name: id, file_type: 'image/png', created_at: '2026-05-01T00:00:00Z', folder };
}

function page(rows: FileAttachment[], total = rows.length): PaginatedResponse<FileAttachment> {
  return { results: rows, total_count: total, page: 1 } as PaginatedResponse<FileAttachment>;
}

describe('GalleryViewService folders', () => {
  let service: GalleryViewService;
  let api: MockedObject<Pick<ImageGalleryService, 'listImages' | 'listFolders' | 'moveImages' | 'moveFolder'>>;

  beforeEach(() => {
    api = {
      listImages: vi.fn().mockReturnValue(of(page([image('a'), image('b')]))),
      listFolders: vi.fn().mockReturnValue(of([])),
      moveImages: vi.fn().mockReturnValue(of(1)),
      moveFolder: vi.fn().mockReturnValue(of(1)),
    } as unknown as typeof api;
    TestBed.configureTestingModule({
      providers: [provideZonelessChangeDetection(), GalleryViewService, { provide: ImageGalleryService, useValue: api }],
    });
    service = TestBed.inject(GalleryViewService);
  });

  const lastFolderAsked = () => (api.listImages.mock.lastCall?.[2] as { folder?: string }).folder;

  describe('what is listed', () => {
    it('asks for the top level by default and one folder when it is open', () => {
      service.loadInitial();
      expect(lastFolderAsked()).toBe('');

      service.openFolder('charts/oura');
      expect(service.currentFolder()).toBe('charts/oura');
      expect(lastFolderAsked()).toBe('charts/oura');
    });

    it('searches every folder, then goes back to the open folder when the search is cleared', () => {
      service.openFolder('charts');
      service.setFilters({ query: 'hrv' });
      expect(lastFolderAsked()).toBeUndefined();
      expect(service.browsingFolders()).toBe(false);
      expect(service.folderTiles()).toEqual([]);

      service.setFilters({ query: '' });
      expect(lastFolderAsked()).toBe('charts');
      expect(service.browsingFolders()).toBe(true);
    });

    it('lists every image, ignoring folders, in the flat view', () => {
      service.openFolder('charts');
      service.setShowAll(true);
      expect(lastFolderAsked()).toBeUndefined();
      expect(service.folderTiles()).toEqual([]);

      service.setShowAll(false);
      expect(lastFolderAsked()).toBe('charts');
    });

    it('builds the tiles for the open level from the server folders and this session’s new ones', () => {
      api.listFolders.mockReturnValue(
        of([
          { path: 'charts/oura', count: 2 },
          { path: 'art', count: 1 },
        ]),
      );
      service.loadFolders();
      expect(service.folderTiles().map(tile => [tile.name, tile.count])).toEqual([
        ['art', 1],
        ['charts', 2],
      ]);

      service.openFolder('charts');
      expect(service.folderTiles().map(tile => tile.path)).toEqual(['charts/oura']);
      expect(service.breadcrumbs().map(crumb => crumb.label)).toEqual(['Gallery', 'charts']);
    });
  });

  describe('creating a folder', () => {
    it('makes one under the open folder, opens it, and keeps it until an image arrives', () => {
      service.openFolder('charts');
      expect(service.createFolder('  Oura ')).toBeNull();

      expect(service.currentFolder()).toBe('charts/oura');
      expect(service.pendingFolders()).toEqual(['charts/oura']);

      service.openFolder('charts');
      expect(service.folderTiles()).toEqual([{ path: 'charts/oura', name: 'oura', count: 0 }]);
    });

    it('refuses a blank or invalid name without opening anything', () => {
      expect(service.createFolder('   ')).toContain('name');
      expect(service.createFolder('../up')).toContain('not allowed');
      expect(service.pendingFolders()).toEqual([]);
      expect(service.currentFolder()).toBe('');
    });

    it('stops tracking a new folder as pending once the server lists it', () => {
      service.createFolder('new');
      api.listFolders.mockReturnValue(of([{ path: 'new', count: 1 }]));
      service.loadFolders();
      expect(service.pendingFolders()).toEqual([]);
    });
  });

  describe('moving images', () => {
    beforeEach(() => service.loadInitial());

    it('moves them to a normalized folder, drops them from the grid and refreshes the folders', async () => {
      service.setSelectionMode(true);
      service.toggleSelected('a');

      const ok = await service.moveImages(['a'], ' Charts / Oura ');

      expect(ok).toBe(true);
      expect(api.moveImages).toHaveBeenCalledWith(['a'], 'charts/oura');
      expect(service.images().map(row => row.id)).toEqual(['b']);
      expect(service.totalCount()).toBe(1);
      expect(service.selectedCount()).toBe(0);
      expect(api.listFolders).toHaveBeenCalled();
    });

    it('leaves the grid alone when images are moved into the folder already open', async () => {
      await service.moveImages(['a'], '');
      expect(service.images().map(row => row.id)).toEqual(['a', 'b']);
    });

    it('relabels rows instead of removing them in the flat view', async () => {
      service.setShowAll(true);
      await service.moveImages(['a'], 'charts');
      expect(service.images().find(row => row.id === 'a')?.folder).toBe('charts');
      expect(service.images().length).toBe(2);
    });

    it('refuses an invalid folder without calling the API', async () => {
      expect(await service.moveImages(['a'], 'a/../b')).toBe(false);
      expect(api.moveImages).not.toHaveBeenCalled();
      expect(service.folderError()).toContain('not allowed');
    });

    it('keeps the images and shows the server’s reason when the move fails', async () => {
      api.moveImages.mockReturnValue(throwError(() => ({ error: { error: 'Choose at least one image' } })));
      expect(await service.moveImages(['a'], 'x')).toBe(false);
      expect(service.folderError()).toBe('Choose at least one image');
      expect(service.images().length).toBe(2);

      api.moveImages.mockReturnValue(throwError(() => new Error('network')));
      await service.moveImages(['a'], 'x');
      expect(service.folderError()).toBe('Could not move the images.');
    });
  });

  describe('moving a folder', () => {
    it('renames it, follows the open folder and the new ones along, and reloads', async () => {
      service.createFolder('charts');
      service.createFolder('oura'); // charts/oura, still empty
      api.listImages.mockClear();

      const ok = await service.moveFolder('charts', 'Archive/Charts');

      expect(ok).toBe(true);
      expect(api.moveFolder).toHaveBeenCalledWith('charts', 'archive/charts');
      expect(service.currentFolder()).toBe('archive/charts/oura');
      expect(service.pendingFolders()).toEqual(['archive/charts', 'archive/charts/oura']);
      expect(api.listImages).toHaveBeenCalledTimes(1);
    });

    it('can move a folder up to the top level', async () => {
      service.openFolder('charts/oura');
      await service.moveFolder('charts/oura', '');
      expect(service.currentFolder()).toBe('');
    });

    it('refuses a move into itself, or an invalid path, without calling the API', async () => {
      expect(await service.moveFolder('charts', 'charts/oura')).toBe(false);
      expect(service.folderError()).toContain('into itself');
      expect(await service.moveFolder('charts', '../x')).toBe(false);
      expect(api.moveFolder).not.toHaveBeenCalled();
    });

    it('treats a move onto itself as done, and reports a server refusal', async () => {
      expect(await service.moveFolder('charts', 'Charts')).toBe(true);
      expect(api.moveFolder).not.toHaveBeenCalled();

      api.moveFolder.mockReturnValue(throwError(() => ({ error: { error: 'invalid folder' } })));
      expect(await service.moveFolder('charts', 'other')).toBe(false);
      expect(service.folderError()).toBe('invalid folder');
    });
  });

  describe('selection', () => {
    it('toggles ids, selects everything shown, and clears with the mode', () => {
      service.loadInitial();
      service.setSelectionMode(true);
      service.toggleSelected('a');
      expect(service.selectedIds().has('a')).toBe(true);
      service.toggleSelected('a');
      expect(service.selectedCount()).toBe(0);

      service.selectAllShown();
      expect(service.selectedCount()).toBe(2);

      service.setSelectionMode(false);
      expect(service.selectedCount()).toBe(0);
    });

    it('clears the selection when another folder is opened', () => {
      service.loadInitial();
      service.toggleSelected('a');
      service.openFolder('charts');
      expect(service.selectedCount()).toBe(0);
    });
  });
});
