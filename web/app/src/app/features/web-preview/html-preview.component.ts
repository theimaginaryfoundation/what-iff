import { ChangeDetectionStrategy, Component, inject, input, output, viewChild } from '@angular/core';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { HtmlPageFrameComponent } from './html-page-frame.component';
import { PageBridgeAction, PageBridgeEvent, PageProject } from './page-bridge';
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
      <app-site-frame [file]="file()" [html]="html()" (pageMessage)="pageMessage.emit($event)" (scriptServed)="scriptServed.emit($event)" />
    } @else {
      <app-html-page-frame [file]="file()" [html]="html()" (pageMessage)="pageMessage.emit($event)" />
    }
  `,
})
export class HtmlPreviewComponent {
  readonly hasSite = !!inject(PREVIEW_URL);
  readonly file = input.required<FileAttachment>();
  /** The page's text as it is now, an editor's unsaved text included. */
  readonly html = input.required<string>();
  /** What the page's bridge script reports (ADR 0x050 §5). */
  readonly pageMessage = output<PageBridgeEvent>();
  /** The page loaded a project script it had not before (a project page only). */
  readonly scriptServed = output<string>();
  private readonly site = viewChild(SiteFrameComponent);
  private readonly lone = viewChild(HtmlPageFrameComponent);

  /** The project the page is served from; null for a page on its own (§1). */
  pageProject(): PageProject | null {
    return this.site()?.pageProject() ?? null;
  }

  /** Tells the page's bridge script something. */
  sendToPage(action: PageBridgeAction): void {
    (this.site() ?? this.lone())?.sendToPage(action);
  }
}
