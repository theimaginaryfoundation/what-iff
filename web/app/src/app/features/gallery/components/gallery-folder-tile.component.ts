import { ChangeDetectionStrategy, Component, input, output, signal } from '@angular/core';

import { TooltipDirective } from '../../../shared/ui/tooltip/tooltip.directive';
import { isGalleryDrag, setGalleryDragData } from '../helpers/gallery-dnd.helpers';
import { FolderTileVm } from '../helpers/gallery-folder.helpers';

/** A folder in the gallery grid: open it to look inside, or rename / move it. */
@Component({
  selector: 'app-gallery-folder-tile',
  standalone: true,
  imports: [TooltipDirective],
  templateUrl: './gallery-folder-tile.component.html',
  styleUrl: './gallery-folder-tile.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class GalleryFolderTileComponent {
  readonly folder = input.required<FolderTileVm>();
  /** Whether what is being dragged can be dropped on this folder. */
  readonly acceptsDrop = input(false);

  readonly open = output<string>();
  readonly edit = output<string>();
  /** Something was dropped on this folder; carries its path. */
  readonly dropped = output<string>();
  /** This folder started to be dragged (to another folder or a breadcrumb step). */
  readonly dragFolder = output<string>();
  readonly dragEnd = output<void>();

  readonly dropHover = signal(false);

  countLabel(count: number): string {
    return count === 1 ? '1 image' : `${count} images`;
  }

  onDragStart(event: DragEvent): void {
    setGalleryDragData(event, { kind: 'folder', path: this.folder().path });
    this.dragFolder.emit(this.folder().path);
  }

  onDragOver(event: DragEvent): void {
    if (!this.acceptsDrop() || !isGalleryDrag(event)) {
      return;
    }
    event.preventDefault(); // this is what makes the folder a place to drop
    if (event.dataTransfer) {
      event.dataTransfer.dropEffect = 'move';
    }
    this.dropHover.set(true);
  }

  onDragLeave(event: DragEvent): void {
    // Moving onto the folder's own icon or label also fires a leave; only a real exit clears it.
    const into = event.relatedTarget as Node | null;
    if (!into || !(event.currentTarget as Node).contains(into)) {
      this.dropHover.set(false);
    }
  }

  onDrop(event: DragEvent): void {
    this.dropHover.set(false);
    if (!this.acceptsDrop()) {
      return;
    }
    event.preventDefault();
    this.dropped.emit(this.folder().path);
  }
}
