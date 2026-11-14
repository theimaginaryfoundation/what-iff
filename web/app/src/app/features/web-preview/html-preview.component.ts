import { ChangeDetectionStrategy, Component, inject, input } from '@angular/core';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { HtmlPageFrameComponent } from './html-page-frame.component';
import { PREVIEW_URL } from './preview-site.config';
import { SiteFrameComponent } from './site-frame.component';

/**
 * An `.html` file rendered (ADR 0x050): served by the preview site, with its folder as the site's
 * root, where this app has one (§3); otherwise on its own in a sandboxed frame, with the files
 * beside it (§1).
 */
@Component({
  selector: 'app-html-preview',
  standalone: true,
  imports: [HtmlPageFrameComponent, SiteFrameComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { style: 'display: block; height: 100%;' },
  template: `
    @if (hasSite) {
      <app-site-frame [file]="file()" [html]="html()" />
    } @else {
      <app-html-page-frame [file]="file()" [html]="html()" />
    }
  `,
})
export class HtmlPreviewComponent {
  readonly hasSite = !!inject(PREVIEW_URL);
  readonly file = input.required<FileAttachment>();
  /** The page's text as it is now, an editor's unsaved text included. */
  readonly html = input.required<string>();
}
