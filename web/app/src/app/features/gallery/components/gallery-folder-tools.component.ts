import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { GalleryViewService } from '../../../core/services/gallery-view.service';
import { moveDestinations } from '../helpers/gallery-folder.helpers';
import { GalleryMoveModalComponent } from './gallery-move-modal.component';

/**
 * The folder controls above the gallery grid: where you are, a new-folder form, the flat "show
 * all" view, select mode with its move action, and the dialogs that move images or a folder.
 * State lives in GalleryViewService; this only drives it.
 */
@Component({
  selector: 'app-gallery-folder-tools',
  standalone: true,
  imports: [FormsModule, GalleryMoveModalComponent],
  templateUrl: './gallery-folder-tools.component.html',
  styleUrl: './gallery-folder-tools.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class GalleryFolderToolsComponent {
  readonly view = inject(GalleryViewService);

  readonly creating = signal(false);
  readonly newName = signal('');
  readonly createError = signal<string | null>(null);
  readonly movingImages = signal(false);
  readonly submitting = signal(false);

  readonly destinations = computed(() => moveDestinations(this.view.folders(), this.view.pendingFolders()));
  /** The last crumb is where you are, so it is not a link. */
  readonly crumbs = computed(() => {
    const crumbs = this.view.breadcrumbs();
    return crumbs.map((crumb, index) => ({ ...crumb, current: index === crumbs.length - 1 }));
  });

  startCreating(): void {
    this.createError.set(null);
    this.newName.set('');
    this.creating.set(true);
  }

  cancelCreating(): void {
    this.creating.set(false);
  }

  createFolder(): void {
    const problem = this.view.createFolder(this.newName());
    this.createError.set(problem);
    if (problem === null) {
      this.creating.set(false);
    }
  }

  toggleSelectionMode(): void {
    this.view.setSelectionMode(!this.view.selectionMode());
  }

  openMoveImages(): void {
    if (this.view.selectedCount() === 0) {
      return;
    }
    this.view.folderError.set(null);
    this.movingImages.set(true);
  }

  closeMoveImages(): void {
    this.movingImages.set(false);
    this.view.folderError.set(null);
  }

  async submitMoveImages(path: string): Promise<void> {
    this.submitting.set(true);
    const moved = await this.view.moveImages([...this.view.selectedIds()], path);
    this.submitting.set(false);
    if (moved) {
      this.movingImages.set(false);
    }
  }

  async submitMoveFolder(path: string): Promise<void> {
    const from = this.view.folderEditing();
    if (from === null) {
      return;
    }
    this.submitting.set(true);
    const moved = await this.view.moveFolder(from, path);
    this.submitting.set(false);
    if (moved) {
      this.view.stopEditingFolder();
    }
  }
}
