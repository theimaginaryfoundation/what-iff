import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { TooltipDirective } from '../../../shared/ui/tooltip/tooltip.directive';
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

  readonly open = output<string>();
  readonly edit = output<string>();

  countLabel(count: number): string {
    return count === 1 ? '1 image' : `${count} images`;
  }
}
