import { DOCUMENT, NgTemplateOutlet } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { ContextPanelService } from '../chat/services/context-panel.service';
import { OverlayComponent } from '../../shared/ui/overlay/overlay.component';
import { FileWorkspaceComponent } from './file-workspace.component';
import { DOCKED_WIDTH_MAX_FRACTION, DOCKED_WIDTH_MIN_FRACTION, FileWorkspaceService } from './file-workspace.service';

/**
 * The chat's host for the file workspace (ADR 0x043 §1): docked beside the thread on a large
 * screen (resizable, expandable to full screen), over the thread on a tablet, filling the screen
 * on a phone. The layout rendering this decides where it goes; this component wires the workspace
 * to the service and to the file actions (rename, delete).
 */
@Component({
  selector: 'app-file-workspace-panel',
  standalone: true,
  imports: [FileWorkspaceComponent, NgTemplateOutlet, OverlayComponent],
  template: `
    @if (workspace.layout() === 'docked') {
      <div class="workspace-panel workspace-panel--docked" [style.width.px]="workspace.dockedWidth()">
        <div
          class="workspace-panel__resizer"
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize the file panel"
          tabindex="0"
          (pointerdown)="startResize($event)"
          (keydown)="onResizerKey($event)"
        ></div>
        <ng-container *ngTemplateOutlet="body" />
      </div>
    } @else {
      <ui-overlay
        [open]="workspace.open()"
        labelledBy="file-workspace-title"
        [variant]="workspace.layout() === 'full' ? 'inset' : 'fill'"
        (dismiss)="close()"
      >
        <ng-container *ngTemplateOutlet="body" />
      </ui-overlay>
    }

    <ng-template #body>
      @if (workspace.file(); as file) {
        <app-file-workspace
          [file]="file"
          [expandable]="workspace.band() === 'large'"
          [expanded]="workspace.expanded()"
          [showGalleryLink]="true"
          [canEdit]="workspace.canEdit()"
          [chatId]="workspace.chatId()"
          (close)="close()"
          (toggleExpand)="workspace.toggleExpanded()"
          (rename)="rename($event)"
          (delete)="remove($event)"
          (fileChanged)="onFileChanged($event)"
          (saveAs)="onSaveAs($event)"
        />
      } @else {
        <div class="workspace-panel__missing" [attr.role]="workspace.error() ? 'alert' : 'status'">
          <p>{{ workspace.error() ?? 'Loading the file…' }}</p>
          <button type="button" (click)="close()">Close</button>
        </div>
      }
    </ng-template>
  `,
  styles: [
    `
      :host {
        display: contents;
      }
      .workspace-panel--docked {
        background: var(--bg-base);
        border-left: 1px solid var(--border);
        display: flex;
        flex-direction: column;
        height: 100%;
        min-width: 0;
        position: relative;
      }
      .workspace-panel__resizer {
        cursor: col-resize;
        height: 100%;
        left: -3px;
        position: absolute;
        top: 0;
        width: 6px;
        z-index: 1;
      }
      .workspace-panel__resizer:hover,
      .workspace-panel__resizer:focus-visible {
        background: var(--accent);
        opacity: 0.5;
        outline: none;
      }
      .workspace-panel__missing {
        align-items: center;
        color: var(--text-secondary);
        display: flex;
        flex-direction: column;
        gap: 0.75rem;
        padding: 3rem 1rem;
        text-align: center;
      }
      .workspace-panel__missing button {
        border: 1px solid var(--border);
        border-radius: 0.5rem;
        padding: 0.35rem 0.7rem;
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class FileWorkspacePanelComponent {
  readonly workspace = inject(FileWorkspaceService);
  private readonly gallery = inject(ImageGalleryService);
  private readonly files = inject(FileAttachmentService);
  private readonly confirmationService = inject(ConfirmationService);
  private readonly contextPanel = inject(ContextPanelService);
  private readonly document = inject(DOCUMENT);

  readonly minWidth = computed(() => Math.round((this.document.defaultView?.innerWidth ?? 1280) * DOCKED_WIDTH_MIN_FRACTION));
  readonly maxWidth = computed(() => Math.round((this.document.defaultView?.innerWidth ?? 1280) * DOCKED_WIDTH_MAX_FRACTION));

  close(): void {
    this.workspace.requestClose();
  }

  /** A save: adopt the new version and let the Files tab show the change. */
  onFileChanged(file: FileAttachment): void {
    this.workspace.update(file);
    this.contextPanel.refreshFiles();
  }

  /**
   * Save As from a thread (ADR 0x043 §3): an upload into the chosen folder that joins the next
   * message, then the new file opens in the workspace.
   */
  onSaveAs(payload: { name: string; folder: string; blob: Blob }): void {
    const chatId = this.workspace.chatId();
    if (!chatId) return;
    const file = new File([payload.blob], payload.name, { type: payload.blob.type });
    this.files.uploadChatFileAttachment(chatId, file, payload.folder).subscribe({
      next: created => {
        this.contextPanel.requestComposerAttachment(created);
        this.workspace.requestOpen(created.id);
      },
      error: async () => {
        await this.confirmationService.alert({ title: 'Save failed', message: 'Could not save the new file. Please try again.', type: 'danger' });
      },
    });
  }

  rename(payload: { id: string; name: string }): void {
    this.gallery.renameImage(payload.id, payload.name).subscribe({
      next: updated => this.workspace.update(updated),
      error: async () => {
        await this.confirmationService.alert({ title: 'Rename failed', message: 'Could not rename this file. Please try again.', type: 'danger' });
      },
    });
  }

  async remove(file: FileAttachment): Promise<void> {
    const confirmed = await this.confirmationService.confirm({
      title: 'Delete file',
      message: `Delete "${file.name}"? Threads it was shared in will no longer be able to open it. This cannot be undone.`,
      type: 'danger',
      confirmText: 'Delete',
      cancelText: 'Cancel',
    });
    if (!confirmed) return;
    this.files.deleteFileAttachment(file.id).subscribe({
      next: () => this.close(),
      error: async () => {
        await this.confirmationService.alert({ title: 'Delete failed', message: 'Could not delete this file. Please try again.', type: 'danger' });
      },
    });
  }

  /** Drags the panel's left edge; the width is the distance from the pointer to the right edge. */
  startResize(event: PointerEvent): void {
    event.preventDefault();
    const view = this.document.defaultView;
    if (!view) return;
    const move = (e: PointerEvent) => this.workspace.setDockedWidth(view.innerWidth - e.clientX);
    const stop = () => {
      view.removeEventListener('pointermove', move);
      view.removeEventListener('pointerup', stop);
    };
    view.addEventListener('pointermove', move);
    view.addEventListener('pointerup', stop);
  }

  onResizerKey(event: KeyboardEvent): void {
    const step = 40;
    if (event.key === 'ArrowLeft') {
      event.preventDefault();
      this.workspace.setDockedWidth(this.workspace.dockedWidth() + step);
    } else if (event.key === 'ArrowRight') {
      event.preventDefault();
      this.workspace.setDockedWidth(this.workspace.dockedWidth() - step);
    }
  }
}
