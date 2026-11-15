import { ChangeDetectionStrategy, Component, DestroyRef, computed, effect, inject, input, signal, untracked } from '@angular/core';

import { ButtonComponent } from '../app/shared/ui';
import { ShareApiService, SharedView } from './share-api.service';
import { SharedFileViewComponent } from './shared-file-view.component';
import { SharedWebAppComponent } from './shared-web-app.component';

type PageState =
  | { state: 'loading' }
  | { state: 'locked' }
  | { state: 'missing' }
  | { state: 'error' }
  | { state: 'banned'; until: number }
  | { state: 'open'; view: SharedView };

/**
 * A share (`/s/<key>`, ADR 0x051 §5): asks for the passcode when it needs one, then shows the file
 * or the web app. A wrong passcode says how many tries are left; a ban says how long it lasts.
 */
@Component({
  selector: 'share-page',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [ButtonComponent, SharedFileViewComponent, SharedWebAppComponent],
  styleUrl: './share-message.scss',
  template: `
    @switch (page().state) {
      @case ('loading') {
        <main class="share-message"><p class="share-message__text">Opening…</p></main>
      }
      @case ('missing') {
        <main class="share-message">
          <h1 class="share-message__title">This link does not work</h1>
          <p class="share-message__text">It does not exist, or its owner stopped sharing it.</p>
        </main>
      }
      @case ('error') {
        <main class="share-message">
          <h1 class="share-message__title">Something went wrong</h1>
          <p class="share-message__text">The share could not be opened. Try again in a moment.</p>
          <ui-button (click)="load()">Try again</ui-button>
        </main>
      }
      @case ('banned') {
        <main class="share-message">
          <h1 class="share-message__title">Too many tries</h1>
          <p class="share-message__text">Wrong passcodes were entered too often from this network. Try again {{ waitText() }}.</p>
        </main>
      }
      @case ('locked') {
        <main class="share-message">
          <h1 class="share-message__title">Enter the passcode</h1>
          <p class="share-message__text">This shared file is protected. Its owner can give you the 12-character passcode.</p>
          <form class="share-message__form" (submit)="$event.preventDefault(); unlock()">
            <label class="share-message__label" for="share-passcode">Passcode</label>
            <input
              id="share-passcode"
              class="share-message__input"
              type="text"
              autocomplete="off"
              autocapitalize="off"
              spellcheck="false"
              maxlength="64"
              [value]="passcode()"
              [attr.aria-invalid]="wrong() ? 'true' : null"
              [attr.aria-describedby]="wrong() ? 'share-passcode-error' : null"
              (input)="passcode.set($any($event.target).value)"
              autofocus
            />
            @if (wrong()) {
              <p id="share-passcode-error" class="share-message__error" role="alert">{{ wrong() }}</p>
            }
            <ui-button variant="primary" type="submit" [disabled]="!passcode().trim() || busy()" [loading]="busy()">Open</ui-button>
          </form>
        </main>
      }
      @case ('open') {
        @if (openView(); as view) {
          @if (view.web_app) {
            <share-web-app [shareKey]="key()" [view]="view" />
          } @else {
            <share-file-view [shareKey]="key()" [view]="view" />
          }
        }
      }
    }
  `,
})
export class SharedPageComponent {
  private readonly api = inject(ShareApiService);

  /** The share's key, from the route. */
  readonly key = input.required<string>();

  readonly page = signal<PageState>({ state: 'loading' });
  readonly passcode = signal('');
  readonly busy = signal(false);
  readonly wrong = signal<string | null>(null);
  readonly openView = computed(() => {
    const page = this.page();
    return page.state === 'open' ? page.view : null;
  });
  readonly waitText = computed(() => {
    const page = this.page();
    if (page.state !== 'banned') return '';
    const minutes = Math.max(1, Math.ceil((page.until - Date.now()) / 60_000));
    return minutes >= 90 ? `in about ${Math.round(minutes / 60)} hours` : `in ${minutes} minute${minutes === 1 ? '' : 's'}`;
  });
  private alive = true;

  constructor() {
    inject(DestroyRef).onDestroy(() => (this.alive = false));
    // Another key (the router can reuse this page for another share) is another share.
    effect(() => {
      this.key();
      untracked(() => void this.load());
    });
  }

  async load(): Promise<void> {
    this.page.set({ state: 'loading' });
    const result = await this.api.open(this.key());
    if (!this.alive) return;
    this.page.set(result.state === 'open' ? { state: 'open', view: result.view } : { state: result.state });
  }

  async unlock(): Promise<void> {
    // Pasted with spaces or a line break around it, as passcodes often are.
    const passcode = this.passcode().trim();
    if (!passcode || this.busy()) return;
    this.busy.set(true);
    this.wrong.set(null);
    const result = await this.api.unlock(this.key(), passcode);
    if (!this.alive) return;
    this.busy.set(false);
    switch (result.state) {
      case 'unlocked':
        this.passcode.set('');
        await this.load();
        // Unlocked, yet still locked: the browser kept the share's cookie out.
        if (this.alive && this.page().state === 'locked') {
          this.wrong.set('The passcode is right, but this browser did not keep the share open. Allow cookies for this site and try again.');
        }
        return;
      case 'wrong':
        this.wrong.set(
          result.attemptsLeft === null
            ? 'That passcode is not right.'
            : `That passcode is not right. ${result.attemptsLeft} ${result.attemptsLeft === 1 ? 'try' : 'tries'} left before this network is blocked for a while.`,
        );
        return;
      case 'banned':
        this.page.set({ state: 'banned', until: Date.now() + result.retryAfterSeconds * 1000 });
        return;
      default:
        this.page.set({ state: result.state });
    }
  }
}
