import { canDropOn, folderPathAfterDrop, GALLERY_DRAG_TYPE, GalleryDrag, isGalleryDrag, setGalleryDragData } from './gallery-dnd.helpers';

function dragEvent(types: string[] = []): { event: DragEvent; transfer: { effectAllowed: string; data: Record<string, string> } } {
  const transfer = { effectAllowed: '', data: {} as Record<string, string> };
  const event = {
    dataTransfer: {
      types,
      get effectAllowed() {
        return transfer.effectAllowed;
      },
      set effectAllowed(value: string) {
        transfer.effectAllowed = value;
      },
      setData: (type: string, value: string) => {
        transfer.data[type] = value;
      },
    },
  } as unknown as DragEvent;
  return { event, transfer };
}

describe('gallery drag and drop rules', () => {
  const images: GalleryDrag = { kind: 'images', ids: ['a', 'b'] };
  const folder: GalleryDrag = { kind: 'folder', path: 'charts/oura' };

  describe('canDropOn', () => {
    it('lets images go on any folder, or the top level, but needs something dragged', () => {
      expect(canDropOn(images, 'charts')).toBe(true);
      expect(canDropOn(images, '')).toBe(true);
      expect(canDropOn({ kind: 'images', ids: [] }, 'charts')).toBe(false);
      expect(canDropOn(null, 'charts')).toBe(false);
    });

    it('lets a folder go into another folder or up to the top level', () => {
      expect(canDropOn(folder, 'art')).toBe(true);
      expect(canDropOn(folder, '')).toBe(true);
    });

    it('does not let a folder go onto itself, inside itself, or where it already is', () => {
      expect(canDropOn(folder, 'charts/oura')).toBe(false);
      expect(canDropOn(folder, 'charts/oura/hrv')).toBe(false);
      expect(canDropOn(folder, 'charts')).toBe(false);
      expect(canDropOn({ kind: 'folder', path: 'top' }, '')).toBe(false);
    });

    it('lets a folder go next to one whose name merely starts the same', () => {
      expect(canDropOn({ kind: 'folder', path: 'charts' }, 'charts-old')).toBe(true);
    });
  });

  it('keeps a dropped folder’s own name', () => {
    expect(folderPathAfterDrop('charts/oura', 'art')).toBe('art/oura');
    expect(folderPathAfterDrop('charts/oura', '')).toBe('oura');
    expect(folderPathAfterDrop('charts', 'a/b')).toBe('a/b/charts');
  });

  it('starts a drag with the data browsers need', () => {
    const { event, transfer } = dragEvent();
    setGalleryDragData(event, images);

    expect(transfer.effectAllowed).toBe('move');
    expect(JSON.parse(transfer.data[GALLERY_DRAG_TYPE])).toEqual(images);
  });

  it('does not fail when the event has no data transfer', () => {
    expect(() => setGalleryDragData({ dataTransfer: null } as unknown as DragEvent, images)).not.toThrow();
  });

  it('recognizes the gallery’s own drags, not files dragged in from the desktop', () => {
    expect(isGalleryDrag(dragEvent([GALLERY_DRAG_TYPE]).event)).toBe(true);
    expect(isGalleryDrag(dragEvent(['Files']).event)).toBe(false);
    expect(isGalleryDrag(dragEvent([]).event)).toBe(false);
    expect(isGalleryDrag({ dataTransfer: null } as unknown as DragEvent)).toBe(false);
  });
});
