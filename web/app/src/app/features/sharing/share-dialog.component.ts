import { HttpErrorResponse } from '@angular/common/http';
import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  ElementRef,
  Injector,
  afterNextRender,
  computed,
  effect,
  inject,
  input,
  output,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { Observable, Subscription } from 'rxjs';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { ShareLink, ShareScope } from '../../core/models/share-link.model';
import { ShareLinkService } from '../../core/services/share-link.service';
import { saveCheckbox } from '../../core/utils/checkbox.helpers';
import { ButtonComponent, CheckboxComponent, ModalComponent } from '../../shared/ui';
import { previewKindFor } from '../gallery/helpers/file-preview.helpers';
import { folderLabel } from '../gallery/helpers/gallery-folder.helpers';

/**
 * Share a file (ADR 0x051 §2): what sharing it shares (as the server works it out from the file's
 * original, so a chat's copy of a page in a folder says it shares the folder), then its link, with
 * a passcode by default; copy the link and the passcode; turn the passcode on or off or make a new
 * one; stop sharing, once confirmed.
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
        @if (state() === 'loading') {
          <p class="share-dialog__text share-dialog__muted" role="status">Loading…</p>
        } @else if (state() === 'failed') {
          <p class="share-dialog__error" role="alert">{{ error() }}</p>
        } @else {
          @if (scope()?.web_app) {
            <p class="share-dialog__text">
              Shares this page as a web app: every file in <strong>{{ folder() }}</strong> and the folders inside it, including files you
              add there later. Visitors see it as it is now, and can read its code.
            </p>
          } @else if (isPage()) {
            <p class="share-dialog__text">
              Shares this page on its own. To share it with its stylesheets, scripts and images as a web app, move it into a folder with
              them.
            </p>
          } @else {
            <p class="share-dialog__text">
              Anyone with the link can view this file as it is now, without signing in. They cannot change it.
            </p>
          }

          @if (link(); as link) {
            <label class="share-dialog__label" for="share-dialog-url">Link</label>
            <div class="share-dialog__row">
              <input
                #linkField
                id="share-dialog-url"
                class="share-dialog__field"
                type="text"
                readonly
                [value]="url()"
                (focus)="select($event)"
              />
              <ui-button size="sm" ariaLabel="Copy link" (click)="copy(url(), 'link')">{{
                copied() === 'link' ? 'Copied' : 'Copy'
              }}</ui-button>
            </div>
            <label class="share-dialog__check">
              <ui-checkbox [checked]="link.passcode_required" [disabled]="busy()" (change)="setPasscode($event)" />
              Require a passcode
            </label>
            @if (link.passcode_required) {
              @if (link.passcode_unreadable) {
                <p class="share-dialog__error" role="alert">The passcode could not be read. Make a new one to share this link again.</p>
              } @else {
                <label class="share-dialog__label" for="share-dialog-passcode">Passcode</label>
              }
              <div class="share-dialog__row">
                @if (!link.passcode_unreadable) {
                  <input
                    id="share-dialog-passcode"
                    class="share-dialog__field share-dialog__field--code"
                    type="text"
                    readonly
                    [value]="link.passcode ?? ''"
                    (focus)="select($event)"
                  />
                  <ui-button size="sm" ariaLabel="Copy passcode" (click)="copy(link.passcode ?? '', 'passcode')">
                    {{ copied() === 'passcode' ? 'Copied' : 'Copy' }}
                  </ui-button>
                }
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
          @if (confirmingStop()) {
            <p class="share-dialog__warning" role="alert">
              Stop sharing? The link stops working for everyone who has it. Sharing again makes a new link.
            </p>
          }
        }
        <span class="share-dialog__live" aria-live="polite">{{ copied() ? 'Copied' : '' }}</span>
      </div>
      <div modal-footer class="share-dialog__actions">
        @if (state() === 'failed') {
          <ui-button (click)="closed.emit()">Close</ui-button>
          <ui-button variant="primary" (click)="load()">Try again</ui-button>
        } @else if (confirmingStop()) {
          <ui-button (click)="keepSharing()">Keep sharing</ui-button>
          <ui-button variant="danger" [disabled]="busy()" [loading]="busy()" (click)="stop()">Stop sharing</ui-button>
        } @else if (link()) {
          <ui-button variant="danger" [disabled]="busy()" (click)="askToStop()">Stop sharing</ui-button>
          <ui-button variant="primary" (click)="closed.emit()">Done</ui-button>
        } @else {
          <ui-button (click)="closed.emit()">Cancel</ui-button>
          <ui-button variant="primary" [disabled]="state() !== 'ready' || busy()" [loading]="busy()" (click)="create()"> Share </ui-button>
        }
      </div>
    </ui-modal>
  `,
})
export class ShareDialogComponent {
  private readonly links = inject(ShareLinkService);
  private readonly injector = inject(Injector);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  readonly file = input.required<FileAttachment>();
  readonly closed = output<void>();

  readonly state = signal<'loading' | 'ready' | 'failed'>('loading');
  readonly scope = signal<ShareScope | null>(null);
  readonly link = signal<ShareLink | null>(null);
  readonly busy = signal(false);
  readonly error = signal<string | null>(null);
  readonly withPasscode = signal(true);
  readonly copied = signal<'link' | 'passcode' | null>(null);
  readonly confirmingStop = signal(false);

  /** A page shared on its own (not in a folder). */
  readonly isPage = computed(() => previewKindFor(this.file()) === 'html');
  readonly folder = computed(() => folderLabel(this.scope()?.folder ?? ''));
  readonly url = computed(() => {
    const link = this.link();
    return link ? this.links.urlOf(link) : '';
  });

  private readonly linkField = viewChild<ElementRef<HTMLInputElement>>('linkField');
  private readonly fileId = computed(() => this.file().id);
  private loading: Subscription | null = null;
  private alive = true;
  private copiedTimer: ReturnType<typeof setTimeout> | null = null;

  constructor() {
    inject(DestroyRef).onDestroy(() => {
      this.alive = false;
      this.loading?.unsubscribe();
      if (this.copiedTimer) clearTimeout(this.copiedTimer);
    });
    // Another file (not a new version of this one) is another share.
    effect(() => {
      this.fileId();
      untracked(() => this.load());
    });
  }

  load(): void {
    this.loading?.unsubscribe();
    this.state.set('loading');
    this.error.set(null);
    this.loading = this.links.get(this.fileId()).subscribe({
      next: ({ scope, link }) => {
        this.scope.set(scope);
        this.link.set(link);
        this.state.set('ready');
      },
      error: (e: unknown) => {
        this.error.set(messageOf(e, 'Could not read how this file is shared.'));
        this.state.set('failed');
      },
    });
  }

  create(): void {
    this.run(this.links.share(this.fileId(), this.withPasscode()), () => this.focusLink());
  }

  /** The passcode box: the change is saved, and the box is put back when it is not. */
  setPasscode(event: Event): void {
    const box = event.target as HTMLInputElement;
    void saveCheckbox(
      box,
      required =>
        new Promise(resolve =>
          this.run(
            this.links.update(this.fileId(), { passcode_required: required }),
            () => resolve(true),
            () => resolve(false),
          ),
        ),
    );
  }

  newPasscode(): void {
    this.run(this.links.update(this.fileId(), { new_passcode: true }));
  }

  stop(): void {
    this.busy.set(true);
    this.error.set(null);
    this.links.stop(this.fileId()).subscribe({
      next: () => {
        if (!this.alive) return;
        this.busy.set(false);
        this.confirmingStop.set(false);
        this.link.set(null);
        this.focusAction('last');
      },
      error: (e: unknown) => {
        if (!this.alive) return;
        this.busy.set(false);
        this.error.set(messageOf(e));
      },
    });
  }

  askToStop(): void {
    this.confirmingStop.set(true);
    this.focusAction('first');
  }

  keepSharing(): void {
    this.confirmingStop.set(false);
    this.focusAction('last');
  }

  async copy(text: string, what: 'link' | 'passcode'): Promise<void> {
    this.error.set(null);
    if (!text) return;
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

  private run(request: Observable<ShareLink>, done?: () => void, failed?: () => void): void {
    this.busy.set(true);
    this.error.set(null);
    request.subscribe({
      next: link => {
        if (!this.alive) return;
        this.busy.set(false);
        this.link.set(link);
        done?.();
      },
      error: (e: unknown) => {
        if (!this.alive) return;
        this.busy.set(false);
        this.error.set(messageOf(e));
        failed?.();
      },
    });
  }

  /** The new link, where focus goes once the Share button it replaced is gone. */
  private focusLink(): void {
    afterNextRender(() => this.linkField()?.nativeElement.focus(), { injector: this.injector });
  }

  /** A footer button, once the buttons it replaced are gone, so focus stays in the dialog. */
  private focusAction(which: 'first' | 'last'): void {
    afterNextRender(
      () => {
        const buttons = this.host.nativeElement.ownerDocument.querySelectorAll<HTMLButtonElement>('.share-dialog__actions button');
        buttons[which === 'first' ? 0 : buttons.length - 1]?.focus();
      },
      { injector: this.injector },
    );
  }
}

function messageOf(error: unknown, fallback = 'Something went wrong. Try again.'): string {
  if (error instanceof HttpErrorResponse) {
    const body = error.error as { message?: string; error?: string } | null;
    return body?.message || body?.error || fallback;
  }
  return fallback;
}
