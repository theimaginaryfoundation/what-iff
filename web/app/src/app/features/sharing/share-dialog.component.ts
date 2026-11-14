import { ChangeDetectionStrategy, Component, DestroyRef, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { HttpErrorResponse } from '@angular/common/http';
import { Observable } from 'rxjs';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { ShareLink } from '../../core/models/share-link.model';
import { ShareLinkService } from '../../core/services/share-link.service';
import { ButtonComponent, CheckboxComponent, ModalComponent } from '../../shared/ui';
import { folderLabel } from '../gallery/helpers/gallery-folder.helpers';
import { isWebAppShare } from './sharing.helpers';

/**
 * Share a file (ADR 0x051 §2): make its link, with a passcode by default; copy the link and the
 * passcode; turn the passcode on or off or make a new one; stop sharing. A web app's dialog names
 * the folder it shares, files added later included.
 */
@Component({
  selector: 'app-share-dialog',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ModalComponent, ButtonComponent, CheckboxComponent],
  styleUrl: './share-dialog.component.scss',
  template: `
    <ui-modal [open]="true" [portal]="true" labelledBy="share-dialog-title" size="sm" (dismiss)="closed.emit()">
      <h2 modal-header id="share-dialog-title" class="share-dialog__title">Share {{ file().name }}</h2>
      <div class="share-dialog">
        @if (webApp()) {
          <p class="share-dialog__text">
            Shares this page as a web app: every file in <strong>{{ folder() }}</strong> and the folders inside it, including files you add
            there later. Visitors see it as it is now, and can read its code.
          </p>
        } @else if (topLevelPage()) {
          <p class="share-dialog__text">
            Shares this page on its own. To share it with its stylesheets, scripts and images as a web app, move it into a folder with them.
          </p>
        } @else {
          <p class="share-dialog__text">Anyone with the link can view this file as it is now, without signing in. They cannot change it.</p>
        }

        @if (loading()) {
          <p class="share-dialog__text share-dialog__muted">Loading…</p>
        } @else if (link(); as link) {
          <label class="share-dialog__label" for="share-dialog-url">Link</label>
          <div class="share-dialog__row">
            <input id="share-dialog-url" class="share-dialog__field" type="text" readonly [value]="url()" (focus)="select($event)" />
            <ui-button size="sm" (click)="copy(url(), 'link')">{{ copied() === 'link' ? 'Copied' : 'Copy' }}</ui-button>
          </div>
          <label class="share-dialog__check">
            <ui-checkbox [checked]="link.passcode_required" [disabled]="busy()" (checkedChange)="setPasscode($event)" />
            Require a passcode
          </label>
          @if (link.passcode_required) {
            <label class="share-dialog__label" for="share-dialog-passcode">Passcode</label>
            <div class="share-dialog__row">
              <input
                id="share-dialog-passcode"
                class="share-dialog__field share-dialog__field--code"
                type="text"
                readonly
                [value]="link.passcode ?? ''"
                (focus)="select($event)"
              />
              <ui-button size="sm" (click)="copy(link.passcode ?? '', 'passcode')">{{
                copied() === 'passcode' ? 'Copied' : 'Copy'
              }}</ui-button>
              <ui-button size="sm" [disabled]="busy()" (click)="newPasscode()">New passcode</ui-button>
            </div>
            <p class="share-dialog__hint">
              Send the passcode separately from the link. A new passcode signs out everyone who used the old one.
            </p>
          } @else {
            <p class="share-dialog__hint">Anyone who has the link can open it.</p>
          }
        } @else {
          <label class="share-dialog__check">
            <ui-checkbox [checked]="withPasscode()" (checkedChange)="withPasscode.set($event)" />
            Require a passcode
          </label>
          <p class="share-dialog__hint">
            {{
              withPasscode()
                ? 'A 12-character passcode is made for the link; visitors type it to open it.'
                : 'Anyone who has the link can open it.'
            }}
          </p>
        }
        @if (error()) {
          <p class="share-dialog__error" role="alert">{{ error() }}</p>
        }
      </div>
      <div modal-footer class="share-dialog__actions">
        @if (link()) {
          <ui-button variant="danger" [disabled]="busy()" (click)="stop()">Stop sharing</ui-button>
          <ui-button variant="primary" (click)="closed.emit()">Done</ui-button>
        } @else {
          <ui-button (click)="closed.emit()">Cancel</ui-button>
          <ui-button variant="primary" [disabled]="loading() || busy()" [loading]="busy()" (click)="create()">Share</ui-button>
        }
      </div>
    </ui-modal>
  `,
})
export class ShareDialogComponent {
  private readonly links = inject(ShareLinkService);
  private readonly destroyRef = inject(DestroyRef);

  readonly file = input.required<FileAttachment>();
  readonly closed = output<void>();
  /** The file's link changed (made, changed or stopped): null when it is no longer shared. */
  readonly changed = output<ShareLink | null>();

  readonly link = signal<ShareLink | null>(null);
  readonly loading = signal(true);
  readonly busy = signal(false);
  readonly error = signal<string | null>(null);
  readonly withPasscode = signal(true);
  readonly copied = signal<'link' | 'passcode' | null>(null);

  readonly webApp = computed(() => isWebAppShare(this.file()));
  readonly topLevelPage = computed(() => !this.webApp() && /\.html?$/i.test(this.file().name));
  readonly folder = computed(() => folderLabel(this.file().folder ?? ''));
  readonly url = computed(() => {
    const link = this.link();
    return link ? this.links.urlOf(link) : '';
  });

  private alive = true;
  private copiedTimer: ReturnType<typeof setTimeout> | null = null;

  constructor() {
    this.destroyRef.onDestroy(() => {
      this.alive = false;
      if (this.copiedTimer) clearTimeout(this.copiedTimer);
    });
    effect(() => {
      const id = this.file().id;
      untracked(() => {
        this.loading.set(true);
        this.links.get(id).subscribe({
          next: link => this.alive && (this.link.set(link), this.loading.set(false)),
          error: (e: unknown) => this.alive && (this.error.set(messageOf(e)), this.loading.set(false)),
        });
      });
    });
  }

  create(): void {
    this.run(this.links.share(this.file().id, this.withPasscode()));
  }

  setPasscode(required: boolean): void {
    this.run(this.links.update(this.file().id, { passcode_required: required }));
  }

  newPasscode(): void {
    this.run(this.links.update(this.file().id, { new_passcode: true }));
  }

  stop(): void {
    this.busy.set(true);
    this.error.set(null);
    this.links.stop(this.file().id).subscribe({
      next: () => {
        if (!this.alive) return;
        this.busy.set(false);
        this.link.set(null);
        this.changed.emit(null);
      },
      error: (e: unknown) => this.alive && (this.busy.set(false), this.error.set(messageOf(e))),
    });
  }

  async copy(text: string, what: 'link' | 'passcode'): Promise<void> {
    try {
      await navigator.clipboard.writeText(text);
      this.copied.set(what);
      if (this.copiedTimer) clearTimeout(this.copiedTimer);
      this.copiedTimer = setTimeout(() => this.copied.set(null), 2000);
    } catch {
      this.error.set('Could not copy. Select the text and copy it yourself.');
    }
  }

  select(event: Event): void {
    (event.target as HTMLInputElement).select();
  }

  private run(request: Observable<ShareLink>): void {
    this.busy.set(true);
    this.error.set(null);
    request.subscribe({
      next: link => {
        if (!this.alive) return;
        this.busy.set(false);
        this.link.set(link);
        this.changed.emit(link);
      },
      error: (e: unknown) => this.alive && (this.busy.set(false), this.error.set(messageOf(e))),
    });
  }
}

function messageOf(error: unknown): string {
  if (error instanceof HttpErrorResponse) {
    const body = error.error as { message?: string; error?: string } | null;
    return body?.message || body?.error || 'Something went wrong. Try again.';
  }
  return 'Something went wrong. Try again.';
}
