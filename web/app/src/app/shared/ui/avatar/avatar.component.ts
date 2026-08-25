import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';
import { initials } from '../helpers/initials.helpers';
import { PersonalityThumbnailCircle } from '../../../core/models/personality.model';
import { thumbnailCircleToImageStyle } from './avatar-thumbnail.helpers';

export type AvatarSize = 'xxs' | 'xs' | 'sm' | 'md' | 'lg' | 'xl';

const SIZE_CLASSES: Record<AvatarSize, string> = {
  xxs: 'h-3.5 w-3.5 text-[8px]',
  xs: 'h-6 w-6 text-[10px]',
  sm: 'h-8 w-8 text-xs',
  md: 'h-10 w-10 text-sm',
  lg: 'h-12 w-12 text-base',
  xl: 'h-16 w-16 text-lg',
};

@Component({
  selector: 'ui-avatar',
  standalone: true,
  templateUrl: './avatar.component.html',
  styleUrl: './avatar.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AvatarComponent {
  readonly name = input.required<string>();
  readonly imageUrl = input<string | null | undefined>(undefined);
  readonly size = input<AvatarSize>('md');
  readonly accentColor = input<string | null | undefined>(undefined);
  readonly thumbnailCircle = input<PersonalityThumbnailCircle | null | undefined>(undefined);

  readonly initials = computed(() => initials(this.name()));
  readonly sizeClass = computed(() => SIZE_CLASSES[this.size()]);
  readonly thumbnailStyle = computed(() => thumbnailCircleToImageStyle(this.thumbnailCircle()));
}
