import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { ConfirmationService } from '../../../core/services/confirmation.service';
import { GalleryViewService } from '../../../core/services/gallery-view.service';
import { BulkAction, BulkActionBarComponent } from '../../../shared/ui/bulk-action-bar/bulk-action-bar.component';
import { isGalleryDrag } from '../helpers/gallery-dnd.helpers';
import { moveDestinations } from '../helpers/gallery-folder.helpers';
import { GalleryMoveModalComponent } from './gallery-move-modal.component';
import { CheckboxComponent } from '../../../shared/ui/checkbox/checkbox.component';

/**
 * The folder controls above the gallery grid or list: where you are, a new-folder form, the flat
 * "show all" view, select mode, the bulk bar (move to folder, delete), and the dialogs that move
 * images or a folder. State lives in GalleryViewService; this only drives it.
 */
@Component({
  selector: 'app-gallery-folder-tools',
  standalone: true,
  imports: [CheckboxComponent, BulkActionBarComponent, FormsModule, GalleryMoveModalComponent],
  templateUrl: './gallery-folder-tools.component.html',
  styleUrl: './gallery-folder-tools.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class GalleryFolderToolsComponent {
  readonly view = inject(GalleryViewService);
  private readonly confirmation = inject(ConfirmationService);
  /** Hide the bar (the file viewer has the page) while keeping the move dialogs usable. */
  readonly barHidden = input(false);

  readonly creating = signal(false);
  readonly newName = signal('');
  readonly createError = signal<string | null>(null);
  readonly movingImages = signal(false);
  readonly submitting = signal(false);
  readonly deleting = signal(false);
  /** The breadcrumb step something is being dragged over, to highlight it. */
  readonly dropHover = signal<string | null>(null);

  /** The images the move dialog is for: the one asked for from the popup, else the selection. */
  readonly moveIds = computed<readonly string[]>(() => this.view.moveRequest() ?? [...this.view.selectedIds()]);
  readonly moveOpen = computed(() => this.movingImages() || this.view.moveRequest() !== null);
  /** Grid select mode always shows the bar (it is how you leave the mode); the list shows it once something is picked. */
  readonly showBulkBar = computed(() =>
    this.view.layout() === 'list' ? this.view.selectedCount() > 0 : this.view.selectionMode(),
  );
  /** The bar's actions; their accessible names say how many items they act on. */
  readonly bulkActions = computed<BulkAction[]>(() => {
    const count = this.view.selectedCount();
    const items = `${count} selected ${count === 1 ? 'item' : 'items'}`;
    return [
      { id: 'move', label: 'Move to folder…', ariaLabel: `Move ${items} to a folder` },
      { id: 'delete', label: 'Delete…', danger: true, ariaLabel: `Delete ${items}` },
    ];
  });
  /** How many matching files are not loaded yet, so cannot be picked (select all covers loaded ones). */
  readonly notLoadedCount = computed(() => Math.max(0, this.view.totalCount() - this.view.images().length));
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

  onBulkAction(id: string): void {
    if (id === 'move') {
      this.openMoveImages();
    } else if (id === 'delete') {
      void this.deleteSelection();
    }
  }

  /** Asks first, then deletes every selected file; what could not be deleted stays selected. */
  async deleteSelection(): Promise<void> {
    const ids = [...this.view.selectedIds()];
    if (ids.length === 0 || this.deleting()) {
      return;
    }
    const noun = ids.length === 1 ? 'item' : 'items';
    const confirmed = await this.confirmation.confirm({
      title: `Delete ${ids.length} ${noun}?`,
      message:
        `Delete ${ids.length} selected ${noun}? Threads they were shared in won't be able to open them anymore, ` +
        'and any personality documents among them will be detached and no longer accessible to that personality. ' +
        'This cannot be undone.',
      confirmText: 'Delete',
      cancelText: 'Cancel',
      type: 'danger',
    });
    if (!confirmed) {
      return;
    }
    this.deleting.set(true);
    try {
      await this.view.deleteFiles(ids);
    } finally {
      this.deleting.set(false);
    }
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
    this.view.clearMoveRequest();
  }

  async submitMoveImages(path: string): Promise<void> {
    this.submitting.set(true);
    const moved = await this.view.moveImages(this.moveIds(), path);
    this.submitting.set(false);
    if (moved) {
      this.closeMoveImages();
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

  /** Whether a breadcrumb step takes what is being dragged: any step except the folder you are in. */
  acceptsDropOnCrumb(path: string): boolean {
    return path !== this.view.currentFolder() && this.view.acceptsDrop(path);
  }

  onCrumbDragOver(event: DragEvent, path: string): void {
    if (!isGalleryDrag(event) || !this.acceptsDropOnCrumb(path)) {
      return;
    }
    event.preventDefault();
    if (event.dataTransfer) {
      event.dataTransfer.dropEffect = 'move';
    }
    this.dropHover.set(path);
  }

  onCrumbDragLeave(path: string): void {
    if (this.dropHover() === path) {
      this.dropHover.set(null);
    }
  }

  onCrumbDrop(event: DragEvent, path: string): void {
    this.dropHover.set(null);
    if (!this.acceptsDropOnCrumb(path)) {
      return;
    }
    event.preventDefault();
    void this.view.dropOn(path);
  }
}
