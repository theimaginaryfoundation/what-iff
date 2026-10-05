import { isWithinFolder, parentFolder } from './gallery-folder.helpers';

/** What is being dragged in the gallery: some images, or a whole folder. */
export type GalleryDrag = { kind: 'images'; ids: readonly string[] } | { kind: 'folder'; path: string };

/** Marks a drag as the gallery's own, so a file dragged in from the desktop is not mistaken for one. */
export const GALLERY_DRAG_TYPE = 'application/x-whatiff-gallery';

/** Starts a drag: browsers (Firefox especially) need data set on the event for it to begin. */
export function setGalleryDragData(event: DragEvent, drag: GalleryDrag): void {
  const transfer = event.dataTransfer;
  if (!transfer) {
    return;
  }
  transfer.effectAllowed = 'move';
  transfer.setData(GALLERY_DRAG_TYPE, JSON.stringify(drag));
}

/** True when the drag over a target is one of the gallery's own (the payload itself is hidden until drop). */
export function isGalleryDrag(event: DragEvent): boolean {
  return Array.from(event.dataTransfer?.types ?? []).includes(GALLERY_DRAG_TYPE);
}

/**
 * Whether a drag can be dropped on a folder ("" is the top level). Images can go anywhere. A folder
 * cannot go onto itself, inside itself, or where it already is.
 */
export function canDropOn(drag: GalleryDrag | null, target: string): boolean {
  if (drag === null) {
    return false;
  }
  if (drag.kind === 'images') {
    return drag.ids.length > 0;
  }
  return drag.path !== '' && !isWithinFolder(target, drag.path) && parentFolder(drag.path) !== target;
}

/** The path a dragged folder takes when dropped on target: it keeps its own name inside it. */
export function folderPathAfterDrop(draggedPath: string, target: string): string {
  const name = draggedPath.slice(draggedPath.lastIndexOf('/') + 1);
  return target === '' ? name : `${target}/${name}`;
}
