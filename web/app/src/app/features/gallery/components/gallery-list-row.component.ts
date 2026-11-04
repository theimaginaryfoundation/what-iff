import { AsyncPipe, DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, input, output } from '@angular/core';

import { AuthImagePipe } from '../../../core/pipes/auth-image.pipe';
import { TrashIconComponent } from '../../../shared/ui/icons/icons';
import { TooltipDirective } from '../../../shared/ui/tooltip/tooltip.directive';
import { FolderTileVm } from '../helpers/gallery-folder.helpers';
import { GalleryTileVm } from '../helpers/gallery-vm.helpers';
import { CheckboxComponent } from '../../../shared/ui/checkbox/checkbox.component';

/**
 * One row of the gallery's list view, in the shared data table (the Thread Manager's): a file with
 * its selection checkbox first, or a folder one level down (opened, not selected; bulk actions act
 * on files). Opening and deleting a file work as on a grid tile.
 */
@Component({
  selector: 'app-gallery-list-row',
  standalone: true,
  imports: [CheckboxComponent, AsyncPipe, AuthImagePipe, DatePipe, TooltipDirective, TrashIconComponent],
  templateUrl: './gallery-list-row.component.html',
  styleUrl: './gallery-list-row.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class GalleryListRowComponent {
  /** The file this row shows. Set exactly one of this and {@link folder}; a folder wins if both are. */
  readonly tile = input<GalleryTileVm | null>(null);
  /** The folder this row shows (a link into it, no checkbox). */
  readonly folder = input<FolderTileVm | null>(null);
  readonly checked = input(false);

  readonly toggleSelect = output<string>();
  readonly open = output<string>();
  readonly openFolder = output<string>();
  readonly delete = output<string>();

  /** What to call the file in labels. */
  readonly noun = computed(() => (this.tile()?.isFile ? 'file' : 'image'));
  readonly typeLabel = computed(() => {
    const tile = this.tile();
    return tile?.isFile ? tile.badge || 'File' : 'Image';
  });

  countLabel(count: number): string {
    return `${count} ${count === 1 ? 'item' : 'items'}`;
  }
}
