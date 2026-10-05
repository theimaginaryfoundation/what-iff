import { ChangeDetectionStrategy, Component, computed, effect, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { ModalComponent } from '../../../shared/ui/modal/modal.component';
import { folderLabel, folderName, folderPathError, isWithinFolder, normalizeFolderPath } from '../helpers/gallery-folder.helpers';

export type GalleryMoveMode = 'images' | 'folder';

/** One row in the list of places to move to. */
interface Destination {
  /** The folder the row stands for ("" is the top level). */
  folder: string;
  label: string;
  /** The path moving there would result in. */
  resulting: string;
}

/**
 * Choose where to file images, or where to put a folder. Pick an existing folder from the list or
 * type a path ("charts/oura"); a folder that does not exist yet is created by the move.
 */
@Component({
  selector: 'app-gallery-move-modal',
  standalone: true,
  imports: [FormsModule, ModalComponent],
  templateUrl: './gallery-move-modal.component.html',
  styleUrl: './gallery-move-modal.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class GalleryMoveModalComponent {
  readonly open = input(false);
  readonly mode = input<GalleryMoveMode>('images');
  /** For images: how many are being moved. For a folder: its current path. */
  readonly imageCount = input(0);
  readonly folderPath = input('');
  /** Every folder a move could target (parents included), as paths. */
  readonly folders = input<readonly string[]>([]);
  /** The folder the images are in now, offered as the starting point. */
  readonly currentFolder = input('');
  readonly submitting = input(false);
  /** Why the last attempt was refused by the server, if it was. */
  readonly serverError = input<string | null>(null);

  readonly close = output<void>();
  readonly submitPath = output<string>();

  readonly path = signal('');

  readonly isFolderMode = computed(() => this.mode() === 'folder');
  readonly title = computed(() =>
    this.isFolderMode()
      ? `Rename or move “${folderName(this.folderPath())}”`
      : `Move ${this.imageCount()} ${this.imageCount() === 1 ? 'image' : 'images'}`,
  );
  readonly hint = computed(() =>
    this.isFolderMode()
      ? 'Edit the path to rename the folder, or pick a folder to put it inside. Everything in it comes along.'
      : 'Pick a folder, or type a path such as charts/oura. A folder that does not exist yet is created.',
  );
  readonly pathError = computed(() => folderPathError(this.path()));
  readonly resultingPath = computed(() => normalizeFolderPath(this.path()));

  readonly destinations = computed<Destination[]>(() => {
    const own = this.folderPath();
    const name = folderName(own);
    const rows: Destination[] = [];
    const add = (folder: string) => {
      // A folder cannot be moved into itself or anything beneath it.
      if (this.isFolderMode() && own !== '' && isWithinFolder(folder, own)) {
        return;
      }
      rows.push({
        folder,
        label: folderLabel(folder),
        resulting: this.isFolderMode() ? (folder === '' ? name : `${folder}/${name}`) : folder,
      });
    };
    add('');
    for (const folder of this.folders()) {
      add(folder);
    }
    return rows;
  });

  readonly canSubmit = computed(() => {
    if (this.submitting() || this.pathError() !== null || this.resultingPath() === null) {
      return false;
    }
    if (this.isFolderMode()) {
      const target = this.resultingPath() as string;
      return target !== '' && target !== this.folderPath() && !isWithinFolder(target, this.folderPath());
    }
    return this.imageCount() > 0;
  });

  readonly submitLabel = computed(() => {
    if (this.submitting()) {
      return 'Moving…';
    }
    return this.isFolderMode() ? 'Move folder' : 'Move';
  });

  constructor() {
    effect(() => {
      if (this.open()) {
        this.path.set(this.isFolderMode() ? this.folderPath() : this.currentFolder());
      }
    });
  }

  pick(destination: Destination): void {
    this.path.set(destination.resulting);
  }

  isPicked(destination: Destination): boolean {
    return this.resultingPath() === destination.resulting;
  }

  submit(): void {
    if (this.canSubmit()) {
      this.submitPath.emit(this.resultingPath() as string);
    }
  }
}
