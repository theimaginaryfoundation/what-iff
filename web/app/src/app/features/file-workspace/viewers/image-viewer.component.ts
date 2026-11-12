import { ChangeDetectionStrategy, Component, DestroyRef, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { DomSanitizer, SafeResourceUrl } from '@angular/platform-browser';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { CommentPeopleService } from '../../office/comments/comment-people.service';
import { CommentsPanelComponent } from '../../office/comments/comments-panel.component';
import { CommentsPanelState } from '../../office/comments/comments-panel-state.service';
import { COMMENT_SAVER } from '../editors/file-editor';
import { TextCommentsState } from '../text-comments/text-comments-state';
import { TextCommentsService } from '../text-comments/text-comments.service';

/**
 * An image, from its own bytes (ADR 0x043 §2), with its comments (ADR 0x045). For now a thread is
 * on the whole image; threads on a region of it (a box drawn on the image) are the expected next
 * step, and would be shown over the image here.
 */
@Component({
  selector: 'app-image-viewer',
  standalone: true,
  imports: [CommentsPanelComponent],
  templateUrl: './image-viewer.component.html',
  styleUrl: './image-viewer.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ImageViewerComponent {
  private readonly sanitizer = inject(DomSanitizer);

  readonly file = input.required<FileAttachment>();
  readonly blob = input.required<Blob>();

  /** An object URL for the bytes, revoked when they change. */
  readonly objectUrl = signal<string | null>(null);
  /** Only ever an object URL made here from the file's own bytes, never a URL from data. */
  readonly safeUrl = computed<SafeResourceUrl | null>(() => {
    const url = this.objectUrl();
    return url ? this.sanitizer.bypassSecurityTrustResourceUrl(url) : null;
  });

  // --- comments (ADR 0x045): on the whole image, kept beside the file, written as they are made ---
  private readonly saver = inject(COMMENT_SAVER, { optional: true });
  readonly me = inject(CommentPeopleService).me;
  readonly comments = new TextCommentsState(inject(TextCommentsService), this.me, () => this.comments.autosave());
  private readonly panel = inject(CommentsPanelState);
  /** Whether the server keeps comments for this image (set once they have loaded). */
  readonly hasComments = signal(false);
  /** Whether comments can be written here (the user may change the file), or only read. */
  readonly canComment = computed(() => this.hasComments() && !!this.saver?.canComment());
  readonly commentsOpen = computed(() => this.hasComments() && (this.panel.open() ?? this.comments.openCount() > 0));
  private generation = 0;

  constructor() {
    effect(() => {
      const blob = this.blob();
      untracked(() => {
        this.revoke();
        this.objectUrl.set(URL.createObjectURL(blob));
        void this.loadComments();
      });
    });
    inject(DestroyRef).onDestroy(() => this.revoke());
  }

  /** Starts a thread on the image. */
  startComment(): void {
    if (!this.canComment()) return;
    this.comments.startOnFile();
    this.panel.open.set(true);
  }

  toggleComments(): void {
    this.panel.open.set(!this.commentsOpen());
  }

  closeComments(): void {
    this.panel.open.set(false);
  }

  private async loadComments(): Promise<void> {
    const generation = ++this.generation;
    try {
      await this.comments.load(this.file().id);
      if (generation === this.generation) this.hasComments.set(true);
    } catch {
      // A file the server cannot keep comments for has none.
      if (generation === this.generation) this.hasComments.set(false);
    }
  }

  private revoke(): void {
    const url = this.objectUrl();
    if (url) {
      URL.revokeObjectURL(url);
      this.objectUrl.set(null);
    }
  }
}
