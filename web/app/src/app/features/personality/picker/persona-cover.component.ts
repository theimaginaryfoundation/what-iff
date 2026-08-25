import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { AsyncPipe } from '@angular/common';

import { AvatarComponent, AvatarSize } from '../../../shared/ui/avatar/avatar.component';
import { AuthImagePipe } from '../../../core/pipes/auth-image.pipe';
import { PersonalityThumbnailCircle } from '../../../core/models/personality.model';

export type PersonaCoverSize = 'sm' | 'md' | 'lg' | 'card';

const SIZE_TO_AVATAR_SIZE: Record<Exclude<PersonaCoverSize, 'card'>, AvatarSize> = {
  sm: 'sm',
  md: 'md',
  lg: 'xl',
};

const SIZE_CLASSES: Record<PersonaCoverSize, string> = {
  sm: 'h-8 w-8',
  md: 'h-14 w-14',
  lg: 'h-24 w-24',
  card: 'aspect-[2/3] w-full',
};

/**
 * Renders a personality portrait at a fixed size variant. Wraps `ui-avatar`
 * so the initials fallback is consistent, and applies an accent ring/border
 * sourced from `--persona-accent`.
 */
@Component({
  selector: 'persona-cover',
  standalone: true,
  imports: [AsyncPipe, AvatarComponent, AuthImagePipe],
  template: `
    @if (size() === 'card') {
      <div
        class="persona-cover persona-cover--card relative overflow-hidden rounded-none"
        [class]="frameClass()"
        [style.background]="'var(--persona-accent-surface)'"
      >
        @if (imageUrl(); as url) {
          <img
            class="absolute inset-0 h-full w-full object-cover object-top"
            [src]="(url | authImage | async) ?? ''"
            [alt]="name()"
            loading="lazy"
            decoding="async"
          />
        } @else {
          <div class="absolute inset-0 flex items-center justify-center text-3xl font-semibold text-white">
            <ui-avatar [name]="name()" size="xl" [accentColor]="'var(--persona-accent)'" />
          </div>
        }
      </div>
    } @else {
      <span
        class="persona-cover persona-cover--badge inline-flex items-center justify-center rounded-full ring-2 ring-[var(--persona-accent)]"
        [class]="frameClass()"
      >
        <ui-avatar
          [name]="name()"
          [imageUrl]="imageUrl() ? (imageUrl()! | authImage | async) : null"
          [size]="avatarSize()"
          [accentColor]="'var(--persona-accent)'"
          [thumbnailCircle]="thumbnailCircle()"
        />
      </span>
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PersonaCoverComponent {
  readonly name = input.required<string>();
  readonly imageUrl = input<string | null | undefined>(null);
  readonly size = input<PersonaCoverSize>('md');
  readonly thumbnailCircle = input<PersonalityThumbnailCircle | null | undefined>(null);

  readonly frameClass = computed(() => SIZE_CLASSES[this.size()]);
  readonly avatarSize = computed<AvatarSize>(() => {
    const value = this.size();
    if (value === 'card') return 'xl';
    return SIZE_TO_AVATAR_SIZE[value];
  });
}
