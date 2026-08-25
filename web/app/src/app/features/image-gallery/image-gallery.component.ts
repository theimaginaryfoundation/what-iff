import { Component, inject, signal, OnInit, HostListener, ChangeDetectionStrategy } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { HttpClient } from '@angular/common/http';
import { AuthImagePipe } from '../../core/pipes/auth-image.pipe';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { ChatService } from '../../core/services/chat.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { FileAttachment } from '../../core/models/file-attachment.model';
import { Chat } from '../../core/models/chat.model';

@Component({
  selector: 'app-image-gallery',
  standalone: true,
  imports: [CommonModule, FormsModule, AuthImagePipe],
  templateUrl: './image-gallery.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./image-gallery.component.scss']
})
export class ImageGalleryComponent implements OnInit {
  private galleryService = inject(ImageGalleryService);
  private chatService = inject(ChatService);
  private confirmationService = inject(ConfirmationService);
  private router = inject(Router);
  private http = inject(HttpClient);

  images = signal<FileAttachment[]>([]);
  isLoading = signal(false);
  error = signal<string | null>(null);
  totalCount = signal(0);
  currentPage = signal(1);
  readonly pageSize = 24;

  selectedImage = signal<FileAttachment | null>(null);
  isModalLoading = signal(false);

  // Rename state
  isRenaming = signal(false);
  renameValue = signal('');

  // Thread picker for "Add to thread" flow
  isThreadPickerOpen = signal(false);
  recentThreads = signal<Chat[]>([]);
  isLoadingThreads = signal(false);

  ngOnInit(): void {
    this.loadImages();
  }

  loadImages(): void {
    this.isLoading.set(true);
    this.error.set(null);

    this.galleryService.listImages(this.currentPage(), this.pageSize).subscribe({
      next: (res) => {
        this.images.set(res.results);
        this.totalCount.set(res.total_count);
        this.isLoading.set(false);
      },
      error: () => {
        this.error.set('Failed to load images. Please try again.');
        this.isLoading.set(false);
      }
    });
  }

  getThumbnailUrl(image: FileAttachment): string {
    return this.galleryService.getImageUrl(image.id, 'thumbnail');
  }

  getFullUrl(image: FileAttachment): string {
    return this.galleryService.getImageUrl(image.id, 'full');
  }

  openModal(image: FileAttachment): void {
    this.selectedImage.set(image);
    this.isModalLoading.set(true);
    this.isRenaming.set(false);
    this.isThreadPickerOpen.set(false);
  }

  closeModal(): void {
    this.selectedImage.set(null);
    this.isModalLoading.set(false);
    this.isRenaming.set(false);
    this.isThreadPickerOpen.set(false);
  }

  onFullImageLoad(): void {
    this.isModalLoading.set(false);
  }

  onFullImageError(): void {
    this.isModalLoading.set(false);
  }

  @HostListener('document:keydown.escape')
  onEscape(): void {
    if (this.isRenaming()) {
      this.isRenaming.set(false);
      return;
    }
    if (this.isThreadPickerOpen()) {
      this.isThreadPickerOpen.set(false);
      return;
    }
    this.closeModal();
  }

  // ── Delete ─────────────────────────────────────────────────────────────────

  async deleteImage(image: FileAttachment, event: Event): Promise<void> {
    event.stopPropagation();
    const confirmed = await this.confirmationService.confirm({
      title: 'Delete Image',
      message: `Delete "${image.name}"? This cannot be undone.`,
      type: 'danger',
      confirmText: 'Delete',
      cancelText: 'Cancel'
    });
    if (!confirmed) return;

    this.galleryService.deleteImage(image.id).subscribe({
      next: () => {
        this.images.update(imgs => imgs.filter(i => i.id !== image.id));
        this.totalCount.update(n => n - 1);
        this.closeModal();
      },
      error: async () => {
        await this.confirmationService.alert({ message: 'Failed to delete image.', type: 'danger' });
      }
    });
  }

  // ── Rename ─────────────────────────────────────────────────────────────────

  startRename(image: FileAttachment, event: Event): void {
    event.stopPropagation();
    // Strip extension so user edits only the base name
    const name = image.name;
    const dot = name.lastIndexOf('.');
    this.renameValue.set(dot > 0 ? name.substring(0, dot) : name);
    this.isRenaming.set(true);
  }

  private getExtension(name: string): string {
    const dot = name.lastIndexOf('.');
    return dot > 0 ? name.substring(dot) : '';
  }

  submitRename(image: FileAttachment): void {
    const basename = this.renameValue().trim();
    if (!basename) { this.isRenaming.set(false); return; }
    const name = basename + this.getExtension(image.name);
    if (name === image.name) {
      this.isRenaming.set(false);
      return;
    }

    this.galleryService.renameImage(image.id, name).subscribe({
      next: (updated) => {
        this.images.update(imgs => imgs.map(i => i.id === updated.id ? { ...i, name: updated.name } : i));
        this.selectedImage.update(sel => sel ? { ...sel, name: updated.name } : null);
        this.isRenaming.set(false);
      },
      error: async () => {
        await this.confirmationService.alert({ message: 'Failed to rename image.', type: 'danger' });
        this.isRenaming.set(false);
      }
    });
  }

  cancelRename(): void {
    this.isRenaming.set(false);
  }

  // ── Open in Chat ───────────────────────────────────────────────────────────

  openNewChat(image: FileAttachment, event: Event): void {
    event.stopPropagation();
    // Create a new thread so we land on a blank slate with the image pre-loaded.
    this.chatService.createChat({ name: 'New Chat' }).subscribe({
      next: (chat) => {
        this.router.navigate(['/chat'], { queryParams: { chatId: chat.id, galleryImageId: image.id } });
      },
      error: () => {
        // Fall back to just navigating without a chat ID
        this.router.navigate(['/chat'], { queryParams: { galleryImageId: image.id } });
      }
    });
  }

  openThreadPicker(image: FileAttachment, event: Event): void {
    event.stopPropagation();
    this.isThreadPickerOpen.set(true);
    this.isLoadingThreads.set(true);
    this.chatService.listChats(1, 10).subscribe({
      next: (res) => {
        this.recentThreads.set(res.results as Chat[]);
        this.isLoadingThreads.set(false);
      },
      error: () => this.isLoadingThreads.set(false)
    });
  }

  addToThread(image: FileAttachment, chat: Chat): void {
    this.isThreadPickerOpen.set(false);
    this.closeModal();
    this.router.navigate(['/chat'], { queryParams: { chatId: chat.id, galleryImageId: image.id } });
  }

  // ── Misc ───────────────────────────────────────────────────────────────────

  get totalPages(): number {
    return Math.ceil(this.totalCount() / this.pageSize);
  }

  goToPage(page: number): void {
    if (page < 1 || page > this.totalPages) return;
    this.currentPage.set(page);
    this.loadImages();
    window.scrollTo({ top: 0, behavior: 'smooth' });
  }

  downloadImage(image: FileAttachment): void {
    this.http.get(this.getFullUrl(image), { responseType: 'blob' }).subscribe((blob) => {
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = image.name || `image-${image.id}`;
      a.click();
      URL.revokeObjectURL(url);
    });
  }

  formatDate(dateStr: string): string {
    return new Date(dateStr).toLocaleDateString(undefined, {
      year: 'numeric', month: 'short', day: 'numeric'
    });
  }

  getPages(): number[] {
    const total = this.totalPages;
    if (total <= 7) return Array.from({ length: total }, (_, i) => i + 1);
    const cur = this.currentPage();
    const pages: number[] = [1];
    if (cur > 3) pages.push(-1);
    for (let i = Math.max(2, cur - 1); i <= Math.min(total - 1, cur + 1); i++) {
      pages.push(i);
    }
    if (cur < total - 2) pages.push(-1);
    pages.push(total);
    return pages;
  }
}
