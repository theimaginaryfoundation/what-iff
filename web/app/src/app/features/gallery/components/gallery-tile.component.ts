import { AsyncPipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { AuthImagePipe } from '../../../core/pipes/auth-image.pipe';
import { TooltipDirective } from '../../../shared/ui/tooltip/tooltip.directive';
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

  readonly toggleSelect = output<string>();
  readonly open = output<string>();
  readonly delete = output<string>();
  readonly assign = output<string>();
}
