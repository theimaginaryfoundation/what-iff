import { ChangeDetectionStrategy, Component, ElementRef, effect, input, output, signal, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { PersonalityThumbnailCircle } from '../../../core/models/personality.model';
import { ModalComponent } from '../../../shared/ui/modal/modal.component';
import { AvatarComponent } from '../../../shared/ui/avatar/avatar.component';
import { PersonaAccentScopeComponent } from '../../personality/picker/persona-accent-scope.component';
import { AuthImagePipe } from '../../../core/pipes/auth-image.pipe';
import { AsyncPipe } from '@angular/common';

export interface GalleryImportPersonalityOption {
  id: string;
  name: string;
  accentColor?: string | null;
  coverImageUrl?: string | null;
  thumbnailCircle?: PersonalityThumbnailCircle | null;
}

export interface GalleryFileImportRequest {
  personalityId: string | null;
  file: File;
  title: string;
  description: string;
  scope: 'global' | 'personality';
}

export interface GalleryUrlImportRequest {
  personalityId: string;
  url: string;
}

@Component({
  selector: 'app-gallery-import-modal',
  standalone: true,
  imports: [AsyncPipe, FormsModule, ModalComponent, AvatarComponent, AuthImagePipe, PersonaAccentScopeComponent],
  templateUrl: './gallery-import-modal.component.html',
  styleUrl: './gallery-import-modal.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class GalleryImportModalComponent {
  readonly open = input(false);
  readonly personalities = input<GalleryImportPersonalityOption[]>([]);
  readonly submitting = input(false);

  readonly close = output<void>();
  readonly submitFile = output<GalleryFileImportRequest>();

  readonly personalityId = signal('');
  readonly title = signal('');
  readonly description = signal('');
  readonly selectedFile = signal<File | null>(null);
  readonly scope = signal<'global' | 'personality'>('global');
  readonly error = signal<string | null>(null);
  readonly fileInput = viewChild<ElementRef<HTMLInputElement>>('fileInput');

  constructor() {
    effect(() => {
      if (this.open()) {
        this.clearState();
      }
    });
  }

  onFileInput(event: Event): void {
    const target = event.target as HTMLInputElement | null;
    const file = target?.files?.[0] ?? null;
    this.selectedFile.set(file);
    if (file) {
      this.title.set(file.name);
      this.error.set(null);
    }
  }

  onDragOver(event: DragEvent): void {
    event.preventDefault();
  }

  onDrop(event: DragEvent): void {
    event.preventDefault();
    const file = event.dataTransfer?.files?.[0] ?? null;
    if (file) {
      this.selectedFile.set(file);
      this.title.set(file.name);
      this.error.set(null);
    }
  }

  setScope(scope: 'global' | 'personality'): void {
    this.scope.set(scope);
    if (scope === 'global') {
      this.personalityId.set('');
    }
    this.error.set(null);
  }

  setPersonality(personalityId: string): void {
    this.personalityId.set(personalityId);
    this.scope.set('personality');
    this.error.set(null);
  }

  onSubmitFile(): void {
    const file = this.selectedFile();
    const title = this.title().trim();
    const scope = this.scope();
    const personalityId = this.personalityId().trim() || null;
    if (!file || !title) {
      this.error.set('Choose an image and provide a title.');
      return;
    }
    if (scope === 'personality' && !personalityId) {
      this.error.set('Choose a personality when using "Pin to character".');
      return;
    }
    const normalizedTitle = normalizeTitleWithFileExtension(title, file.name);
    this.submitFile.emit({
      personalityId,
      file,
      title: normalizedTitle,
      description: this.description().trim(),
      scope,
    });
  }

  clearState(): void {
    this.title.set('');
    this.description.set('');
    this.error.set(null);
    this.selectedFile.set(null);
    this.scope.set('global');
    this.personalityId.set('');
    const fileInput = this.fileInput();
    if (fileInput) {
      fileInput.nativeElement.value = '';
    }
  }
}

function normalizeTitleWithFileExtension(title: string, originalFileName: string): string {
  const trimmed = title.trim();
  if (!trimmed) {
    return originalFileName;
  }
  const extension = extractExtension(originalFileName);
  if (!extension || extractExtension(trimmed)) {
    return trimmed;
  }
  return `${trimmed}.${extension}`;
}

function extractExtension(name: string): string {
  const normalized = name.trim();
  const lastDot = normalized.lastIndexOf('.');
  if (lastDot <= 0 || lastDot === normalized.length - 1) {
    return '';
  }
  return normalized.slice(lastDot + 1);
}
