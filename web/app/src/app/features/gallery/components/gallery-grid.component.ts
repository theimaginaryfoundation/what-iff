import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { GalleryTileVm } from '../helpers/gallery-vm.helpers';
import { GalleryTileComponent } from './gallery-tile.component';

@Component({
  selector: 'app-gallery-grid',
  standalone: true,
  imports: [GalleryTileComponent],
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

  readonly openImage = output<string>();
  readonly deleteImage = output<string>();
  readonly assignImage = output<string>();
  readonly retry = output<void>();
  readonly loadMore = output<void>();
}
