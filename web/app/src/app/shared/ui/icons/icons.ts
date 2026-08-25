import { Component, input, ChangeDetectionStrategy } from '@angular/core';

@Component({
  selector: 'ui-chat-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/>
    </svg>
  `,
})
export class ChatIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-user-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/>
    </svg>
  `,
})
export class UserIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-users-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75"/>
    </svg>
  `,
})
export class UsersIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-bolt-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="currentColor" stroke="none" aria-hidden="true" focusable="false">
      <path d="M13 2L3 14h9l-1 10 10-12h-9l1-10z"/>
    </svg>
  `,
})
export class BoltIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-home-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><path d="M9 22V12h6v10"/>
    </svg>
  `,
})
export class HomeIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-gear-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>
    </svg>
  `,
})
export class GearIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-plus-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M12 5v14m-7-7h14"/>
    </svg>
  `,
})
export class PlusIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-file-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6M16 13H8M16 17H8M10 9H8"/>
    </svg>
  `,
})
export class FileIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-credit-card-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <rect x="2" y="5" width="20" height="14" rx="2"/><path d="M2 10h20"/><path d="M6 15h2"/><path d="M10 15h4"/>
    </svg>
  `,
})
export class CreditCardIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-smile-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <circle cx="12" cy="12" r="10"/><path d="M8 15s1.5 2 4 2 4-2 4-2"/><line x1="9" y1="9" x2="9.01" y2="9"/><line x1="15" y1="9" x2="15.01" y2="9"/>
    </svg>
  `,
})
export class SmileIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-sparkle-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="currentColor" stroke="none" aria-hidden="true" focusable="false">
      <path d="M12 2l2.09 6.26L20.18 10l-6.09 1.74L12 18l-2.09-6.26L3.82 10l6.09-1.74L12 2z"/>
    </svg>
  `,
})
export class SparkleIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-edit-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
    </svg>
  `,
})
export class EditIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-trash-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
    </svg>
  `,
})
export class TrashIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-upload-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="M17 8l-5-5-5 5M12 3v12"/>
    </svg>
  `,
})
export class UploadIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-chev-down-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M6 9l6 6 6-6"/>
    </svg>
  `,
})
export class ChevDownIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-chev-up-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M18 15l-6-6-6 6"/>
    </svg>
  `,
})
export class ChevUpIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-chev-right-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M9 18l6-6-6-6"/>
    </svg>
  `,
})
export class ChevRightIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-chev-left-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M15 18l-6-6 6-6"/>
    </svg>
  `,
})
export class ChevLeftIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-x-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M18 6L6 18M6 6l12 12"/>
    </svg>
  `,
})
export class XIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-bar-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M12 20V10"/><path d="M18 20V4"/><path d="M6 20v-4"/>
    </svg>
  `,
})
export class BarIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-layers-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <polygon points="12 2 3 7 12 12 21 7 12 2" />
      <polyline points="3 12 12 17 21 12" />
      <polyline points="3 17 12 22 21 17" />
    </svg>
  `,
})
export class LayersIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-note-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M14.5 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7.5L14.5 2z"/><path d="M14 2v6h6"/>
    </svg>
  `,
})
export class NoteIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-brain-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M12 5a3 3 0 1 0-5.997.125 4 4 0 0 0-2.526 5.77 4 4 0 0 0 .556 6.588A4 4 0 1 0 12 18Z"/><path d="M12 5a3 3 0 1 1 5.997.125 4 4 0 0 1 2.526 5.77 4 4 0 0 1-.556 6.588A4 4 0 1 1 12 18Z"/><path d="M15 13a4.5 4.5 0 0 1-3-4 4.5 4.5 0 0 1-3 4"/><path d="M17.599 6.5a3 3 0 0 0 .399-1.375"/><path d="M6.003 5.125A3 3 0 0 0 6.401 6.5"/><path d="M3.477 10.896a4 4 0 0 1 .585-.396"/><path d="M19.938 10.5a4 4 0 0 1 .585.396"/><path d="M14 18a4 4 0 0 1-4 0"/>
    </svg>
  `,
})
export class BrainIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-shield-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
    </svg>
  `,
})
export class ShieldIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-sun-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <circle cx="12" cy="12" r="5"/><path d="M12 1v2M12 21v2M4.22 4.22l1.42 1.42M18.36 18.36l1.42 1.42M1 12h2M21 12h2M4.22 19.78l1.42-1.42M18.36 5.64l1.42-1.42"/>
    </svg>
  `,
})
export class SunIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-moon-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/>
    </svg>
  `,
})
export class MoonIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-send-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M22 2L11 13M22 2l-7 20-4-9-9-4 20-7z"/>
    </svg>
  `,
})
export class SendIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-arrow-left-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M19 12H5M12 19l-7-7 7-7"/>
    </svg>
  `,
})
export class ArrowLeftIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-arrow-right-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M5 12h14M12 5l7 7-7 7"/>
    </svg>
  `,
})
export class ArrowRightIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-arrow-up-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M12 19V5M5 12l7-7 7 7"/>
    </svg>
  `,
})
export class ArrowUpIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-lightning-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="currentColor" stroke="none" aria-hidden="true" focusable="false">
      <path d="M13 2L3 14h9l-1 10 10-12h-9l1-10z"/>
    </svg>
  `,
})
export class LightningIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-clock-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>
    </svg>
  `,
})
export class ClockIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-terminal-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <polyline points="4 17 10 11 4 5"/><line x1="12" y1="19" x2="20" y2="19"/>
    </svg>
  `,
})
export class TerminalIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-globe-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/>
    </svg>
  `,
})
export class GlobeIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-image-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <rect x="3" y="3" width="18" height="18" rx="2" ry="2"/><circle cx="8.5" cy="8.5" r="1.5"/><polyline points="21 15 16 10 5 21"/>
    </svg>
  `,
})
export class ImageIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-check-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <polyline points="20 6 9 17 4 12"/>
    </svg>
  `,
})
export class CheckIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-alert-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>
    </svg>
  `,
})
export class AlertIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-wrench-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/>
    </svg>
  `,
})
export class WrenchIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-search-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <circle cx="11" cy="11" r="8"/><path d="M21 21l-4.35-4.35"/>
    </svg>
  `,
})
export class SearchIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-code-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/>
    </svg>
  `,
})
export class CodeIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-save-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/>
    </svg>
  `,
})
export class SaveIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-menu-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M3 12h18M3 6h18M3 18h18"/>
    </svg>
  `,
})
export class MenuIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-adjustments-horizontal-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M3 6h6"/><path d="M15 6h6"/><circle cx="12" cy="6" r="2"/>
      <path d="M3 12h10"/><path d="M19 12h2"/><circle cx="16" cy="12" r="2"/>
      <path d="M3 18h2"/><path d="M11 18h10"/><circle cx="8" cy="18" r="2"/>
    </svg>
  `,
})
export class AdjustmentsHorizontalIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-download-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
      <polyline points="7 10 12 15 17 10"/>
      <line x1="12" y1="15" x2="12" y2="3"/>
    </svg>
  `,
})
export class DownloadIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-star-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      @if (filled()) {
        <path
          d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z"
          fill="currentColor"
          stroke="none"
        />
      } @else {
        <path
          d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
        />
      }
    </svg>
  `,
})
export class StarIconComponent {
  readonly size = input(16);
  readonly filled = input(false);
}

@Component({
  selector: 'ui-bell-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="currentColor" stroke="none" aria-hidden="true" focusable="false">
      <path d="M12 2a6 6 0 0 0-6 6v3.586l-.707.707A1 1 0 0 0 4 14h12a1 1 0 0 0 .707-1.707L16 11.586V8a6 6 0 0 0-6-6zm0 18a3 3 0 0 1-3-3h6a3 3 0 0 1-3 3z"/>
    </svg>
  `,
})
export class BellIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-circle-help-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <circle cx="12" cy="12" r="10"/>
      <path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/>
      <line x1="12" y1="17" x2="12.01" y2="17"/>
    </svg>
  `,
})
export class CircleHelpIconComponent {
  readonly size = input(16);
}

@Component({
  selector: 'ui-expand-vertical-icon',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.Eager,
  template: `
    <svg [attr.width]="size()" [attr.height]="size()" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <path d="M12 4v16"/>
      <path d="M8 8l4-4 4 4"/>
      <path d="M8 16l4 4 4-4"/>
    </svg>
  `,
})
export class ExpandVerticalIconComponent {
  readonly size = input(16);
}
