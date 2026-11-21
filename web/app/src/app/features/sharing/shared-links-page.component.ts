import { DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, DestroyRef, inject, signal } from '@angular/core';
import { RouterLink } from '@angular/router';

import { ShareLink } from '../../core/models/share-link.model';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { ShareLinkService } from '../../core/services/share-link.service';
import { BadgeComponent, ButtonComponent, EmptyStateComponent, SpinnerComponent } from '../../shared/ui';
import { folderLabel } from '../gallery/helpers/gallery-folder.helpers';

/** Every share link of the user's (ADR 0x051 §2), to copy a link or stop sharing. */
@Component({
  selector: 'app-shared-links-page',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DatePipe, RouterLink, BadgeComponent, ButtonComponent, EmptyStateComponent, SpinnerComponent],
  styleUrl: './shared-links-page.component.scss',
  template: `
    <div class="shared-links">
      <header class="shared-links__header">
        <h1 class="shared-links__title">Shared links</h1>
        <p class="shared-links__lede">
          Files you have shared with a link. Anyone with a link (and its passcode, when it needs one) can view the file as it is now. Open a
          file to change its passcode.
        </p>
      </header>

      @if (!enabled) {
        <ui-empty-state heading="Sharing is not set up" body="This server has no share site, so files cannot be shared with a link." />
      } @else if (loading()) {
        <ui-spinner />
      } @else if (loadError()) {
        <p class="shared-links__error" role="alert">{{ loadError() }}</p>
        <ui-button (click)="load()">Try again</ui-button>
      } @else if (links().length === 0) {
        <ui-empty-state heading="Nothing shared yet" body="Open a file and choose Share to make a link to it." />
      } @else {
        @if (actionError()) {
          <p class="shared-links__error" role="alert">{{ actionError() }}</p>
        }
        <span class="shared-links__live" aria-live="polite">{{ copied() ? 'Link copied' : '' }}</span>
        <ul class="shared-links__list">
          @for (link of links(); track link.key) {
            <li class="shared-links__row">
              <div class="shared-links__item">
                <a class="shared-links__name" [routerLink]="['/gallery']" [queryParams]="{ file: link.file_id }">{{ link.file_name }}</a>
                <span class="shared-links__meta">
                  @if (link.web_app) {
                    <ui-badge intent="info">Web app</ui-badge>
                  }
                  <span>{{ folderName(link) }}</span>
                  <span>{{ link.passcode_required ? 'Passcode' : 'Anyone with the link' }}</span>
                  @if (link.passcode_unreadable) {
                    <ui-badge intent="warning">Passcode unreadable: open the file to make a new one</ui-badge>
                  }
                  <span>Shared {{ link.created_at | date: 'medium' }}</span>
                </span>
              </div>
              <div class="shared-links__actions">
                <ui-button size="sm" [ariaLabel]="'Copy the link to ' + link.file_name" (click)="copy(link)">
                  {{ copied() === link.key ? 'Copied' : 'Copy link' }}
                </ui-button>
                <ui-button
                  size="sm"
                  variant="danger"
                  [ariaLabel]="'Stop sharing ' + link.file_name"
                  [disabled]="stopping().has(link.key)"
                  (click)="stop(link)"
                >
                  Stop sharing
                </ui-button>
              </div>
            </li>
          }
        </ul>
      }
    </div>
  `,
})
export class SharedLinksPageComponent {
  private readonly service = inject(ShareLinkService);
  private readonly confirmation = inject(ConfirmationService);

  readonly enabled = this.service.enabled;
  readonly links = signal<ShareLink[]>([]);
  readonly loading = signal(true);
  /** The list could not be read: it takes the list's place. */
  readonly loadError = signal<string | null>(null);
  /** A copy or a stop that failed: shown above the list, cleared by the next action. */
  readonly actionError = signal<string | null>(null);
  readonly copied = signal<string | null>(null);
  readonly stopping = signal<ReadonlySet<string>>(new Set());
  private alive = true;

  constructor() {
    inject(DestroyRef).onDestroy(() => (this.alive = false));
    if (!this.enabled) {
      this.loading.set(false);
      return;
    }
    this.load();
  }

  load(): void {
    this.loading.set(true);
    this.loadError.set(null);
    this.service.list().subscribe({
      next: links => this.alive && (this.links.set(links), this.loading.set(false)),
      error: () => this.alive && (this.loadError.set('Could not load your shared links.'), this.loading.set(false)),
    });
  }

  folderName(link: ShareLink): string {
    return link.folder ? folderLabel(link.folder) : 'Top level';
  }

  async copy(link: ShareLink): Promise<void> {
    this.actionError.set(null);
    try {
      await navigator.clipboard.writeText(this.service.urlOf(link));
      this.copied.set(link.key);
      setTimeout(() => this.alive && this.copied() === link.key && this.copied.set(null), 2000);
    } catch {
      this.actionError.set('Could not copy the link. Open the file and select it there.');
    }
  }

  async stop(link: ShareLink): Promise<void> {
    this.actionError.set(null);
    const ok = await this.confirmation.confirm({
      title: 'Stop sharing?',
      message: `The link to ${link.file_name} stops working for everyone who has it. Sharing it again makes a new link.`,
      confirmText: 'Stop sharing',
      type: 'danger',
    });
    if (!ok) return;
    const busy = (on: boolean) =>
      this.stopping.update(keys => {
        const next = new Set(keys);
        if (on) next.add(link.key);
        else next.delete(link.key);
        return next;
      });
    busy(true);
    this.service.stop(link.file_id).subscribe({
      next: () => {
        if (!this.alive) return;
        busy(false);
        this.links.update(links => links.filter(l => l.key !== link.key));
      },
      error: () => {
        if (!this.alive) return;
        busy(false);
        this.actionError.set(`Could not stop sharing ${link.file_name}. Try again.`);
      },
    });
  }
}
