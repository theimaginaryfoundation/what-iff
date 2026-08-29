import { AsyncPipe, CommonModule } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, input, output, signal } from '@angular/core';

import { Chat } from '../../../../core/models/chat.model';
import { Personality } from '../../../../core/models/personality.model';
import { ImageGalleryService } from '../../../../core/services/image-gallery.service';
import { AuthImagePipe } from '../../../../core/pipes/auth-image.pipe';
import { personalityCoverUrl } from '../../../personality/helpers/cover-image.helpers';
import { personalityAccent } from '../../../personality/helpers/personality-vm.helpers';
import { thumbnailCircleToImageStyle } from '../../../../shared/ui/avatar/avatar-thumbnail.helpers';
import { StarIconComponent, TrashIconComponent } from '../../../../shared/ui/icons/icons';
import { ContextPanelService } from '../../services/context-panel.service';

@Component({
  selector: 'app-thread-row',
  standalone: true,
  imports: [CommonModule, AsyncPipe, AuthImagePipe, StarIconComponent, TrashIconComponent],
  template: `
    <tr
      class="thread-row"
      [class.thread-row--active]="active()"
      [attr.id]="thread().id"
      role="option"
      [attr.aria-selected]="active()"
      (click)="onRowClick($event)"
    >
      <td class="thread-row__select-cell" (click)="$event.stopPropagation()">
        <input
          type="checkbox"
          class="thread-row__select"
          [checked]="checked()"
          (change)="toggleSelect.emit(thread().id)"
          [attr.aria-label]="'Select thread ' + thread().name"
        />
      </td>
      <td class="thread-row__star-cell">
        <button
          type="button"
          class="thread-row__star"
          [class.thread-row__star--active]="thread().is_favorite"
          (click)="togglePin.emit(thread())"
          [attr.aria-label]="thread().is_favorite ? 'Unstar thread' : 'Star thread'"
          [attr.aria-pressed]="thread().is_favorite"
        >
          <ui-star-icon [size]="16" [filled]="!!thread().is_favorite" />
        </button>
      </td>
      <td class="thread-row__personality">
        <span class="thread-row__personality-content" [style.--thread-persona-accent]="personalityAccentColor() || 'var(--color-text-muted)'">
          <span class="thread-row__personality-avatar" aria-hidden="true">
            @if (personalityAvatarUrl(); as avatarUrl) {
              @if (avatarUrl | authImage | async; as resolvedAvatarUrl) {
                <img
                  [src]="resolvedAvatarUrl"
                  [alt]="personalityLabel() + ' avatar'"
                  [style.object-position]="personalityThumbnailStyle()?.objectPosition"
                  [style.transform]="personalityThumbnailStyle()?.transform"
                />
              } @else {
                <span class="thread-row__personality-avatar-fallback">{{ personalityInitial() }}</span>
              }
            } @else {
              <span class="thread-row__personality-avatar-fallback">{{ personalityInitial() }}</span>
            }
          </span>
          <span class="thread-row__personality-name">{{ personalityLabel() }}</span>
        </span>
      </td>
      <td class="thread-row__title">
        <div class="thread-row__title-wrap">
          @if (editing()) {
            <input
              class="thread-row__name-input"
              [value]="thread().name"
              (blur)="commitRename($any($event.target).value)"
              (keydown.enter)="commitRename($any($event.target).value)"
              (keydown.escape)="editing.set(false)"
              aria-label="Rename thread"
            />
          } @else {
            <button
              type="button"
              class="thread-row__main"
              (click)="select.emit(thread().id)"
              (dblclick)="editing.set(true)"
              (keydown.shift.f10)="deleteThread.emit(thread())"
              [attr.aria-label]="'Open thread ' + thread().name"
            >
              <span class="thread-row__name">{{ thread().name }}</span>
              @if (thread().unread_count && thread().unread_count! > 0) {
                <span class="thread-row__badge">{{ thread().unread_count }}</span>
              }
            </button>
            <button
              type="button"
              class="thread-row__context"
              [attr.aria-label]="'Add thread ' + thread().name + ' to context'"
              title="Add to context"
              (click)="queueForContext($event)"
            >
              + Context
            </button>
          }
        </div>
      </td>
      <td class="thread-row__date thread-row__created">{{ formatTimestamp(thread().created_at) }}</td>
      <td class="thread-row__date thread-row__updated">{{ formatTimestamp(thread().last_message_time ?? thread().updated_at) }}</td>
      <td class="thread-row__tags-cell">
        @if ((thread().tags ?? []).length > 0) {
          <button
            type="button"
            class="thread-row__tags"
            (click)="editTags.emit(thread())"
            [attr.aria-label]="'Edit tags: ' + (thread().tags ?? []).join(', ')"
          >
            @for (tag of (thread().tags ?? []); track tag) {
              <span>{{ tag }}</span>
            }
          </button>
        } @else {
          <button type="button" class="thread-row__tags thread-row__tags--empty" (click)="editTags.emit(thread())">
            Add tags
          </button>
        }
      </td>
      <td class="thread-row__archive-cell">
        @if (isArchivedView()) {
          <button
            type="button"
            class="thread-row__archive thread-row__archive--restore"
            (click)="restoreThread.emit(thread())"
            aria-label="Restore thread from archive"
          >
            Restore
          </button>
        } @else {
          <button
            type="button"
            class="thread-row__archive"
            (click)="archiveThread.emit(thread())"
            aria-label="Archive thread"
          >
            Archive
          </button>
        }
      </td>
      <td class="thread-row__delete-cell">
        <button
          type="button"
          class="thread-row__delete"
          (click)="deleteThread.emit(thread()); $event.stopPropagation()"
          [attr.aria-label]="'Delete thread ' + thread().name"
        >
          <ui-trash-icon [size]="14" />
        </button>
      </td>
    </tr>
  `,
  styles: [`
    :host { display: contents; }
    .thread-row { color: var(--color-text-secondary); cursor: pointer; font-size: 0.75rem; }
    .thread-row--active { background: color-mix(in srgb, var(--color-accent) 10%, transparent); }
    td { border-top: 1px solid var(--color-border-base); padding: 0.5rem 0.75rem; vertical-align: middle; white-space: nowrap; }
    .thread-row__title { min-width: 14rem; width: 32%; }
    .thread-row__title-wrap { align-items: center; display: flex; gap: 0.5rem; min-width: 0; }
    .thread-row__main { align-items: center; background: transparent; border: 0; color: var(--color-text-primary); cursor: pointer; display: flex; flex: 1; font: inherit; justify-content: space-between; min-width: 0; padding: 0; text-align: left; }
    .thread-row__name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .thread-row__badge { background: var(--color-accent); border-radius: 999px; color: white; font-size: 0.75rem; min-width: 1.25rem; padding: 0 0.375rem; text-align: center; }
    .thread-row__context { background: transparent; border: 1px solid var(--color-border-base); border-radius: 999px; color: var(--color-text-muted); cursor: pointer; flex: 0 0 auto; font-size: 0.68rem; font-weight: 700; padding: 0.2rem 0.5rem; }
    .thread-row__context:hover, .thread-row__context:focus-visible { border-color: var(--color-accent); color: var(--color-accent); }
    .thread-row__tags { align-items: center; background: transparent; border: 0; color: var(--color-text-secondary); cursor: pointer; display: flex; flex-wrap: wrap; gap: 0.25rem; max-width: 18rem; padding: 0; text-align: left; }
    .thread-row__tags span { background: var(--color-surface-muted); border-radius: 999px; padding: 0.125rem 0.45rem; }
    .thread-row__tags--empty { color: var(--color-text-muted); }
    .thread-row__select-cell { text-align: center; }
    .thread-row__select { accent-color: var(--color-accent); cursor: pointer; height: 1rem; width: 1rem; }
    .thread-row__star, .thread-row__archive { border: 1px solid var(--color-border-base); border-radius: 999px; color: var(--color-text-muted); cursor: pointer; font-size: 0.68rem; font-weight: 700; padding: 0.2rem 0.55rem; text-transform: uppercase; }
    .thread-row__star { align-items: center; background: var(--color-surface-base); display: inline-flex; justify-content: center; padding: 0.35rem; }
    .thread-row__star--active { background: color-mix(in srgb, var(--color-accent) 14%, transparent); border-color: var(--color-accent); color: var(--color-accent); }
    .thread-row__archive { background: var(--color-surface-base); opacity: 1; }
    .thread-row__archive--restore { text-transform: none; }
    .thread-row__delete { background: transparent; border: 0; border-radius: 0.375rem; color: var(--color-text-muted); cursor: pointer; display: inline-flex; justify-content: center; padding: 0.35rem; text-transform: none; }
    .thread-row__delete:hover, .thread-row__delete:focus-visible { background: color-mix(in srgb, var(--color-danger) 12%, transparent); color: var(--color-danger); }
    .thread-row__date, .thread-row__personality { color: var(--color-text-muted); }
    .thread-row__personality-content { align-items: center; color: var(--thread-persona-accent, var(--color-text-muted)); display: inline-flex; gap: 0.4375rem; max-width: 100%; }
    .thread-row__personality-avatar { align-items: center; background: color-mix(in srgb, var(--thread-persona-accent, var(--color-accent)) 16%, var(--color-surface-muted)); border-radius: 50%; color: var(--thread-persona-accent, var(--color-accent)); display: inline-flex; flex: 0 0 auto; font-size: 0.625rem; font-weight: 800; height: 1.5rem; justify-content: center; overflow: hidden; width: 1.5rem; }
    .thread-row__personality-avatar img { height: 100%; object-fit: cover; transform-origin: center center; width: 100%; }
    .thread-row__personality-avatar-fallback { align-items: center; display: inline-flex; height: 100%; justify-content: center; width: 100%; }
    .thread-row__personality-name { color: inherit; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .thread-row__name-input { background: var(--color-surface-base); border: 1px solid var(--color-accent); border-radius: 0.5rem; color: var(--color-text-primary); font: inherit; padding: 0.35rem 0.5rem; width: 100%; }
    .thread-row__archive-cell, .thread-row__delete-cell, .thread-row__star-cell { text-align: center; width: 2.75rem; }
    .thread-row__delete-cell { padding-inline: 0.375rem; }
    @media (max-width: 1023px) {
      .thread-row__title { min-width: 11rem; }
      .thread-row__name-input { font-size: 1rem; }
      .thread-row__archive, .thread-row__context, .thread-row__delete, .thread-row__star { min-height: 2.25rem; }
    }
    @media (max-width: 767px) {
      .thread-row__created, .thread-row__tags-cell { display: none; }
      .thread-row__title { min-width: 8rem; width: auto; }
      .thread-row__context { font-size: 0; padding-inline: 0.45rem; }
      .thread-row__context::after { content: '+'; font-size: 0.875rem; }
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ThreadRowComponent {
  private readonly imageGallery = inject(ImageGalleryService);
  private readonly contextPanel = inject(ContextPanelService);
  readonly thread = input.required<Chat>();
  readonly personality = input<Personality | null>(null);
  readonly active = input(false);
  /** Archived tab: show Restore instead of Archive. */
  readonly isArchivedView = input(false);
  /** Bulk-selection: whether this row's checkbox is checked. */
  readonly checked = input(false);

  readonly select = output<string>();
  readonly addToContext = output<Chat>();
  readonly toggleSelect = output<string>();
  readonly rename = output<{ thread: Chat; name: string }>();
  readonly togglePin = output<Chat>();
  readonly editTags = output<Chat>();
  readonly deleteThread = output<Chat>();
  readonly archiveThread = output<Chat>();
  readonly restoreThread = output<Chat>();
  readonly editing = signal(false);
  readonly personalityLabel = computed(() =>
    this.personality()?.name ?? this.thread().personality_name ?? 'Unassigned',
  );
  readonly personalityAccentColor = computed(() => {
    const personality = this.personality();
    if (personality) return personalityAccent(personality);
    const label = this.thread().personality_name?.trim();
    if (!label) return null;
    return personalityAccent({ id: this.thread().personality_id ?? '', name: label, accent_color: null });
  });
  readonly personalityAvatarUrl = computed(() =>
    personalityCoverUrl(
      this.personality(),
      [],
      this.imageGallery.getImageUrl.bind(this.imageGallery),
    ),
  );
  readonly personalityThumbnailStyle = computed(() =>
    thumbnailCircleToImageStyle(this.personality()?.thumbnail_circle),
  );

  queueForContext(event: Event): void {
    event.stopPropagation();
    const thread = this.thread();
    this.contextPanel.queueThreadReference(thread);
    this.addToContext.emit(thread);
  }

  commitRename(nextName: string): void {
    this.editing.set(false);
    const name = nextName.trim();
    if (!name || name === this.thread().name) return;
    this.rename.emit({ thread: this.thread(), name });
  }

  formatTimestamp(value: string): string {
    const parsed = Date.parse(value);
    if (Number.isNaN(parsed)) return '';
    return new Date(parsed).toLocaleString([], {
      month: 'short',
      day: 'numeric',
      hour: 'numeric',
      minute: '2-digit',
    });
  }

  personalityInitial(): string {
    return this.personalityLabel().trim().charAt(0).toUpperCase() || '?';
  }

  onRowClick(event: MouseEvent): void {
    if (this.editing()) return;
    const target = event.target as HTMLElement | null;
    if (!target) return;
    if (target.closest('.thread-row__star')) return;
    if (target.closest('button, input, select, textarea, a, [role="button"]')) return;
    this.select.emit(this.thread().id);
  }
}
