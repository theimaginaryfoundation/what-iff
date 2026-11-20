import { ChangeDetectionStrategy, Component, input } from '@angular/core';

@Component({
  selector: 'ui-constellation-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <svg
      [attr.width]="size()"
      [attr.height]="size()"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="1.5"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      <path d="m4 17 5-12 6 8 5-7M4 17l11-4 4 7" />
      <circle cx="4" cy="17" r="1.7" />
      <circle cx="9" cy="5" r="1.7" />
      <circle cx="15" cy="13" r="1.7" />
      <circle cx="20" cy="6" r="1.7" />
      <circle cx="19" cy="20" r="1.7" />
    </svg>
  `,
})
export class ConstellationIconComponent {
  readonly size = input(18);
}
