import { AsyncPipe, DatePipe } from '@angular/common';
import { HttpClient } from '@angular/common/http';
import {
  ChangeDetectionStrategy,
  Component,
  DOCUMENT,
  HostListener,
  computed,
  effect,
  inject,
  input,
  output,
  signal,
} from '@angular/core';
import { catchError, finalize, take } from 'rxjs/operators';
import { of } from 'rxjs';

import { Chat } from '../../../core/models/chat.model';
import { ChatService } from '../../../core/services/chat.service';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { AuthImagePipe } from '../../../core/pipes/auth-image.pipe';
import { DownloadIconComponent, EditIconComponent } from '../../../shared/ui/icons/icons';
import { ModalComponent } from '../../../shared/ui/modal/modal.component';
import {
  galleryDisplayBaseName,
  galleryFilenameFromBaseName,
} from '../helpers/gallery-filename.helpers';
import { GalleryTileVm } from '../helpers/gallery-vm.helpers';

@Component({
  selector: 'app-image-detail-modal',
  standalone: true,
  imports: [AsyncPipe, DatePipe, AuthImagePipe, ModalComponent, EditIconComponent, DownloadIconComponent],
  templateUrl: './image-detail-modal.component.html',
  styleUrl: './image-detail-modal.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ImageDetailModalComponent {
  private readonly http = inject(HttpClient);
  private readonly document = inject(DOCUMENT);
  private readonly chatService = inject(ChatService);
  private readonly confirmationService = inject(ConfirmationService);

  readonly open = input(false);
  readonly image = input<GalleryTileVm | null>(null);
  readonly hasPrev = input(false);
  readonly hasNext = input(false);
  readonly assignmentEnabled = input(false);

  readonly close = output<void>();
  readonly next = output<void>();
  readonly previous = output<void>();
  readonly delete = output<string>();
  readonly assign = output<string>();
  readonly rename = output<{ id: string; name: string }>();
  readonly addToThread = output<{ imageId: string; chatId: string }>();
  readonly startNewChat = output<string>();

  readonly isRenaming = signal(false);
  readonly renameDraft = signal('');
  readonly threadPickerOpen = signal(false);
  readonly recentThreads = signal<Chat[]>([]);
  readonly threadsLoading = signal(false);
  readonly threadsLoadError = signal(false);

  readonly displayTitle = computed(() => {
    const image = this.image();
    if (!image) {
      return 'Image details';
    }
    return galleryDisplayBaseName(image.name) || 'Untitled image';
  });

  constructor() {
    // Reset transient UI when the selected gallery image changes.
    effect(() => {
      this.image();
      this.resetTransientState();
    });

    // Also reset when the modal opens so stale transient state cannot flash.
    effect(() => {
      if (this.open()) {
        this.resetTransientState();
      }
    });
  }

  @HostListener('document:keydown', ['$event'])
  onDocumentKeydown(event: KeyboardEvent): void {
    if (!this.open()) {
      return;
    }
    if (this.isRenaming()) {
      if (event.key === 'Escape') {
        event.preventDefault();
        this.cancelRename();
      }
      return;
    }
    if (event.key === 'ArrowRight') {
      event.preventDefault();
      this.next.emit();
    } else if (event.key === 'ArrowLeft') {
      event.preventDefault();
      this.previous.emit();
    }
  }

  startRename(): void {
    const image = this.image();
    if (!image) {
      return;
    }
    this.renameDraft.set(galleryDisplayBaseName(image.name));
    this.isRenaming.set(true);
    this.threadPickerOpen.set(false);
  }

  cancelRename(): void {
    this.isRenaming.set(false);
  }

  submitRename(): void {
    const image = this.image();
    if (!image) {
      return;
    }
    const nextName = galleryFilenameFromBaseName(image.name, this.renameDraft());
    if (!nextName || nextName === image.name) {
      this.isRenaming.set(false);
      return;
    }
    this.rename.emit({ id: image.id, name: nextName });
    this.isRenaming.set(false);
  }

  download(): void {
    const image = this.image();
    if (!image) {
      return;
    }
    this.http
      .get(image.fullUrl, { responseType: 'blob' })
      .pipe(
        take(1),
        catchError(() => {
          void this.confirmationService.alert({
            title: 'Download failed',
            message: 'Could not download this image. Please try again.',
            type: 'danger',
          });
          return of(null);
        }),
      )
      .subscribe(blob => {
        if (!blob) {
          return;
        }
        const url = URL.createObjectURL(blob);
        const anchor = this.document.createElement('a');
        anchor.href = url;
        anchor.download = image.name || `image-${image.id}`;
        anchor.click();
        URL.revokeObjectURL(url);
      });
  }

  openThreadPicker(): void {
    const image = this.image();
    if (!image) {
      return;
    }
    this.threadPickerOpen.update(open => !open);
    if (!this.threadPickerOpen() || this.recentThreads().length > 0) {
      return;
    }
    this.threadsLoading.set(true);
    this.threadsLoadError.set(false);
    // Fetch a single recent page for the inline thread picker.
    this.chatService
      .listChats(1, 12)
      .pipe(
        take(1),
        finalize(() => this.threadsLoading.set(false)),
      )
      .subscribe({
        next: res => this.recentThreads.set(res.results ?? []),
        error: () => {
          this.recentThreads.set([]);
          this.threadsLoadError.set(true);
        },
      });
  }

  pickThread(chat: Chat): void {
    const image = this.image();
    if (!image) {
      return;
    }
    this.threadPickerOpen.set(false);
    this.addToThread.emit({ imageId: image.id, chatId: chat.id });
  }

  onStartNewChat(): void {
    const image = this.image();
    if (!image) {
      return;
    }
    this.startNewChat.emit(image.id);
  }

  private resetTransientState(): void {
    this.isRenaming.set(false);
    this.renameDraft.set('');
    this.threadPickerOpen.set(false);
    this.recentThreads.set([]);
    this.threadsLoading.set(false);
    this.threadsLoadError.set(false);
  }
}
