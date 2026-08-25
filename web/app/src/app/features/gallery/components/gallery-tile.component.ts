import { AsyncPipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { AuthImagePipe } from '../../../core/pipes/auth-image.pipe';
import { GalleryTileVm } from '../helpers/gallery-vm.helpers';

@Component({
  selector: 'app-gallery-tile',
  standalone: true,
  imports: [AsyncPipe, AuthImagePipe],
  templateUrl: './gallery-tile.component.html',
  styleUrl: './gallery-tile.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class GalleryTileComponent {
  readonly tile = input.required<GalleryTileVm>();
  readonly assignmentEnabled = input(false);

  readonly open = output<string>();
  readonly delete = output<string>();
  readonly assign = output<string>();
}
