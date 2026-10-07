import { AsyncPipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { AuthImagePipe } from '../../../core/pipes/auth-image.pipe';
import { TooltipDirective } from '../../../shared/ui/tooltip/tooltip.directive';
import { setGalleryDragData } from '../helpers/gallery-dnd.helpers';
import { GalleryTileVm } from '../helpers/gallery-vm.helpers';

@Component({
  selector: 'app-gallery-tile',
  standalone: true,
  imports: [AsyncPipe, AuthImagePipe, TooltipDirective],
  templateUrl: './gallery-tile.component.html',
  styleUrl: './gallery-tile.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class GalleryTileComponent {
  readonly tile = input.required<GalleryTileVm>();
  readonly assignmentEnabled = input(false);
  /** Selection mode: a click picks the image instead of opening it. */
  readonly selectable = input(false);
  readonly selected = input(false);
  /** Show the folder the image is in (flat and search views, where folders are mixed together). */
  readonly showFolder = input(false);

  /** An image started to be dragged (to a folder tile or breadcrumb step). */
  readonly dragImage = output<string>();
  readonly dragEnd = output<void>();
  readonly toggleSelect = output<string>();
  readonly open = output<string>();
  readonly delete = output<string>();
  readonly assign = output<string>();

  /** What to call this tile in labels. */
  noun(): string {
    return this.tile().isFile ? 'file' : 'image';
  }

  onDragStart(event: DragEvent): void {
    // What is moved is decided by the gallery view (a selected image takes the whole selection);
    // the data set here is only what browsers need to start a drag.
    setGalleryDragData(event, { kind: 'images', ids: [this.tile().id] });
    this.dragImage.emit(this.tile().id);
  }
}
