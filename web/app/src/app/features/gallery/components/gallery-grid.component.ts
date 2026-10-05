import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { canDropOn, GalleryDrag } from '../helpers/gallery-dnd.helpers';
import { FolderTileVm } from '../helpers/gallery-folder.helpers';
import { GalleryTileVm } from '../helpers/gallery-vm.helpers';
import { GalleryFolderTileComponent } from './gallery-folder-tile.component';
import { GalleryTileComponent } from './gallery-tile.component';

@Component({
  selector: 'app-gallery-grid',
  standalone: true,
  imports: [GalleryTileComponent, GalleryFolderTileComponent],
  templateUrl: './gallery-grid.component.html',
  styleUrl: './gallery-grid.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class GalleryGridComponent {
  readonly tiles = input<GalleryTileVm[]>([]);
  readonly loading = input(false);
  readonly loadingMore = input(false);
  readonly hasMore = input(false);
  readonly error = input<string | null>(null);
  readonly assignmentEnabled = input(false);
  /** Folders one level down, shown before the images. */
  readonly folderTiles = input<FolderTileVm[]>([]);
  readonly selectable = input(false);
  readonly selectedIds = input<ReadonlySet<string>>(new Set());
  /** Label each image with its folder (flat and search views mix folders together). */
  readonly showFolders = input(false);
  /** What to say when there is nothing to show. */
  readonly emptyMessage = input('No images match these filters yet.');

  /** What is being dragged, so folder tiles know whether they accept it. */
  readonly drag = input<GalleryDrag | null>(null);

  readonly dragImage = output<string>();
  readonly dragFolder = output<string>();
  readonly dragEnd = output<void>();
  readonly dropOnFolder = output<string>();
  readonly openFolder = output<string>();
  readonly editFolder = output<string>();
  readonly toggleSelect = output<string>();
  readonly openImage = output<string>();
  readonly deleteImage = output<string>();
  readonly assignImage = output<string>();
  readonly retry = output<void>();
  readonly loadMore = output<void>();

  acceptsDrop(folderPath: string): boolean {
    return canDropOn(this.drag(), folderPath);
  }
}
