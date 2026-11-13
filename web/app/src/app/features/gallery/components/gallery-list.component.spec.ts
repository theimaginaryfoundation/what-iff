import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';

import { GalleryTileVm } from '../helpers/gallery-vm.helpers';
import { GalleryListComponent } from './gallery-list.component';

function tile(id: string, overrides: Partial<GalleryTileVm> = {}): GalleryTileVm {
  return {
    id,
    name: `${id}.png`,
    createdAt: '2026-05-01T00:00:00Z',
    personalityId: null,
    personalityName: null,
    personalityNames: [],
    source: 'generated',
    sourceLabel: 'Generated',
    folder: '',
    isFile: false,
    badge: '',
    thumbnailUrl: '',
    fullUrl: '',
    previewUrl: '',
    ...overrides,
  };
}

describe('GalleryListComponent', () => {
  let fixture: ComponentFixture<GalleryListComponent>;
  const el = () => fixture.nativeElement as HTMLElement;
  const emitted: Record<string, unknown[]> = {};

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [GalleryListComponent],
      providers: [provideZonelessChangeDetection(), provideHttpClient(), provideHttpClientTesting()],
    }).compileComponents();
    fixture = TestBed.createComponent(GalleryListComponent);
    const component = fixture.componentInstance;
    for (const name of ['toggleSelect', 'toggleSelectAll', 'openImage', 'openFolder', 'deleteImage', 'loadMore'] as const) {
      emitted[name] = [];
      (component[name] as { subscribe(fn: (value: unknown) => void): void }).subscribe(value => emitted[name].push(value));
    }
    fixture.componentRef.setInput('tiles', [
      tile('a', { folder: 'charts' }),
      tile('doc', { isFile: true, badge: 'PDF', name: 'notes.pdf' }),
    ]);
    fixture.componentRef.setInput('folderTiles', [{ path: 'charts/oura', name: 'oura', count: 2 }]);
    fixture.componentRef.setInput('selectedIds', new Set(['a']));
    fixture.componentRef.setInput('selectionState', 'some');
    fixture.detectChanges();
  });

  it('lays out folders then files in the shared table, each file with a labelled checkbox', () => {
    const rows = Array.from(el().querySelectorAll('tbody tr'));
    expect(rows).toHaveLength(3);
    expect(rows[0].textContent).toContain('oura');
    expect(rows[0].querySelector('input[type="checkbox"]')).toBeNull();

    const boxes = Array.from(el().querySelectorAll('.list-row__select')) as HTMLInputElement[];
    expect(boxes.map(box => box.getAttribute('aria-label'))).toEqual(['Select image a.png', 'Select file notes.pdf']);
    expect(boxes.map(box => box.checked)).toEqual([true, false]);
    expect(rows[1].textContent).toContain('charts');
    expect(rows[2].textContent).toContain('PDF');

    const selectAll = el().querySelector('.ui-data-table__select-all') as HTMLInputElement;
    expect(selectAll.indeterminate).toBe(true);
    expect(selectAll.getAttribute('aria-label')).toBe('Select all loaded files');
  });

  it('reports ticks, select all, open, delete and folder opens', () => {
    (el().querySelectorAll('.list-row__select')[1] as HTMLInputElement).click();
    (el().querySelector('.ui-data-table__select-all') as HTMLInputElement).click();
    (el().querySelector('[aria-label="Open file notes.pdf"]') as HTMLButtonElement).click();
    (el().querySelector('[aria-label="Delete image a.png"]') as HTMLButtonElement).click();
    (el().querySelector('[aria-label="Open folder oura, 2 items"]') as HTMLButtonElement).click();

    expect(emitted['toggleSelect']).toEqual(['doc']);
    expect(emitted['toggleSelectAll']).toHaveLength(1);
    expect(emitted['openImage']).toEqual(['doc']);
    expect(emitted['deleteImage']).toEqual(['a']);
    expect(emitted['openFolder']).toEqual(['charts/oura']);
  });

  it('shows the empty message in the table, and Load more while there is more', () => {
    fixture.componentRef.setInput('tiles', []);
    fixture.componentRef.setInput('folderTiles', []);
    fixture.componentRef.setInput('emptyMessage', 'Nothing matches these filters yet.');
    fixture.componentRef.setInput('hasMore', true);
    fixture.detectChanges();

    expect(el().querySelector('.ui-data-table__message')?.textContent?.trim()).toBe('Nothing matches these filters yet.');
    const more = Array.from(el().querySelectorAll('button')).find(b => b.textContent?.trim() === 'Load more') as HTMLButtonElement;
    more.click();
    expect(emitted['loadMore']).toHaveLength(1);
  });
});
