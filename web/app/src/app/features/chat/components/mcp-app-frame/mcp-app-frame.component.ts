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
  signal,
  viewChild,
} from '@angular/core';
import { DOCUMENT } from '@angular/common';
import { DomSanitizer, SafeHtml } from '@angular/platform-browser';
import { firstValueFrom } from 'rxjs';

import { ToolCall } from '../../../../core/models/toolcall.model';
import { McpAppService, McpAppView } from '../../../../core/services/mcp-app.service';
import { ThemeService } from '../../../../core/services/theme.service';
import { apiErrorMessage } from '../../../../core/utils/api-error.helpers';
import {
  ButtonComponent,
  ChevDownIconComponent,
  ChevUpIconComponent,
  IconButtonComponent,
  LayersIconComponent,
  ModalComponent,
} from '../../../../shared/ui';
import { McpAppBridge, JsonRpcMessage } from './mcp-app-bridge';
import { McpAppLinkPrompt } from './mcp-app-link-prompt';
import { mcpHostStyleVariables, toolDisplayName } from './mcp-app-host-styles';

/** The frame's height before the view reports its own, and the most it may grow to. */
const INITIAL_HEIGHT_PX = 240;
const MAX_HEIGHT_PX = 800;
const MIN_HEIGHT_PX = 60;
/** A view that never finishes its handshake is shown anyway this long after its document loads. */
const REVEAL_FALLBACK_MS = 1500;

/**
 * An MCP App's view (ADR 0x042), rendered under the reply whose tool call produced it.
 *
 * The view's HTML runs in an iframe with `sandbox="allow-scripts allow-forms"` and no
 * allow-same-origin, loaded through srcdoc: it gets an opaque origin, so it cannot read the app's
 * storage, cookies or DOM, and it talks to the chat only through postMessage (McpAppBridge). Its
 * network access is what its Content-Security-Policy (set by the server-side view endpoint from the
 * domains the view declared) allows. The spec's separate-origin sandbox proxy is a follow-up.
 */
@Component({
  selector: 'app-mcp-app-frame',
  standalone: true,
  imports: [ButtonComponent, ChevDownIconComponent, ChevUpIconComponent, IconButtonComponent, LayersIconComponent, ModalComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="app" [class.app--flush]="!bordered()" [attr.aria-label]="ariaLabel()" data-testid="mcp-app-frame">
      <header class="app__head">
        <ui-layers-icon class="app__icon" [size]="14" />
        <span class="app__name">{{ title() }}</span>
        @if (view()?.server_name; as server) {
          <span class="app__server">{{ server }}</span>
        }
        <ui-icon-button
          class="app__toggle"
          size="xs"
          [ariaLabel]="collapsed() ? 'Show ' + title() : 'Hide ' + title()"
          [attr.aria-expanded]="!collapsed()"
          data-testid="mcp-app-toggle"
          (activate)="collapsed.set(!collapsed())"
        >
          @if (collapsed()) {
            <ui-chev-down-icon [size]="14" />
          } @else {
            <ui-chev-up-icon [size]="14" />
          }
        </ui-icon-button>
      </header>

      <div class="app__stage" [hidden]="collapsed()" [style.height.px]="state() === 'error' ? null : height()">
        @if (state() === 'error') {
          <div class="app__problem">
            <p role="alert">{{ error() }}</p>
            <ui-button size="xs" (activate)="load()" data-testid="mcp-app-retry">Try again</ui-button>
          </div>
        } @else {
          @if (!revealed()) {
            <div class="app__skeleton" role="status" aria-label="Loading app" data-testid="mcp-app-loading">
              <span></span><span></span><span></span>
            </div>
          }
          @if (state() === 'ready') {
            <iframe
              #frame
              class="app__frame"
              [class.app__frame--shown]="revealed()"
              [title]="title()"
              sandbox="allow-scripts allow-forms"
              referrerpolicy="no-referrer"
              [style.color-scheme]="theme()"
              [srcdoc]="srcdoc()"
              (load)="onFrameLoaded()"
            ></iframe>
          }
        }
      </div>
    </section>

    <ui-modal [open]="!!pendingLink()" size="sm" labelledBy="mcp-app-link-title" (dismiss)="answerLink(false)">
      <h2 modal-header id="mcp-app-link-title" class="app-link__title">Open this link?</h2>
      @if (pendingLink(); as link) {
        <p class="app-link__who">{{ title() }} wants to open a page on <strong>{{ link.host }}</strong>.</p>
        <p class="app-link__url" data-testid="mcp-app-link-url">{{ link.url }}</p>
      }
      <div modal-footer class="app-link__actions">
        <ui-button size="sm" variant="ghost" (activate)="answerLink(false)">Cancel</ui-button>
        <ui-button size="sm" variant="primary" (activate)="answerLink(true)" data-testid="mcp-app-link-open">Open link</ui-button>
      </div>
    </ui-modal>
  `,
  styles: [`
    :host { display: block; margin-top: 0.625rem; max-width: 100%; }
    .app { background: var(--bg-card); border: 1px solid var(--border); border-radius: 0.75rem; overflow: hidden; }
    .app--flush { background: transparent; border-color: transparent; }
    .app__head { align-items: center; display: flex; gap: 0.375rem; min-height: 2rem; padding: 0.125rem 0.25rem 0.125rem 0.625rem; }
    .app:not(.app--flush) .app__head { border-bottom: 1px solid var(--border); }
    .app:has(.app__stage[hidden]) .app__head { border-bottom-color: transparent; }
    .app__icon { color: var(--text-muted); display: inline-flex; flex: none; }
    .app__name { color: var(--text-primary); font-size: 0.75rem; font-weight: 600; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .app__server { color: var(--text-muted); flex: none; font-size: 0.75rem; max-width: 40%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .app__toggle { margin-left: auto; }
    .app__stage { position: relative; transition: height 160ms ease; }
    .app__stage[hidden] { display: none; }
    @media (prefers-reduced-motion: reduce) { .app__stage { transition: none; } }
    .app__frame { background: transparent; border: 0; display: block; height: 100%; opacity: 0; transition: opacity 160ms ease; width: 100%; }
    .app__frame--shown { opacity: 1; }
    .app__skeleton { display: grid; gap: 0.625rem; inset: 0; align-content: start; padding: 0.875rem; position: absolute; }
    .app__skeleton span {
      animation: app-shimmer 1.4s ease-in-out infinite;
      background: color-mix(in srgb, var(--text-primary) 8%, transparent);
      border-radius: 0.375rem;
      height: 0.75rem;
    }
    .app__skeleton span:nth-child(1) { width: 40%; }
    .app__skeleton span:nth-child(2) { width: 85%; }
    .app__skeleton span:nth-child(3) { width: 65%; }
    @keyframes app-shimmer { 50% { opacity: 0.45; } }
    @media (prefers-reduced-motion: reduce) { .app__skeleton span { animation: none; } }
    .app__problem { align-items: center; display: flex; flex-wrap: wrap; gap: 0.75rem; justify-content: space-between; padding: 0.625rem 0.75rem; }
    .app__problem p { color: var(--danger); font-size: 0.8125rem; margin: 0; }
    .app-link__title { font-size: 1rem; font-weight: 600; margin: 0; }
    .app-link__who { font-size: 0.875rem; margin: 0 0 0.5rem; }
    .app-link__url { color: var(--text-muted); font-size: 0.8125rem; margin: 0; overflow-wrap: anywhere; }
    .app-link__actions { display: flex; gap: 0.5rem; justify-content: flex-end; }
  `],
})
export class McpAppFrameComponent {
  readonly chatId = input.required<string>();
  readonly toolCall = input.required<ToolCall>();

  private readonly apps = inject(McpAppService);
  private readonly sanitizer = inject(DomSanitizer);
  private readonly document = inject(DOCUMENT);
  private readonly host = inject(ElementRef<HTMLElement>);
  private readonly destroyRef = inject(DestroyRef);
  private readonly themes = inject(ThemeService);
  private readonly injector = inject(Injector);
  private readonly frame = viewChild<ElementRef<HTMLIFrameElement>>('frame');

  readonly state = signal<'idle' | 'loading' | 'error' | 'ready'>('idle');
  readonly error = signal('');
  readonly view = signal<McpAppView | null>(null);
  readonly height = signal(INITIAL_HEIGHT_PX);
  /**
   * The frame's color scheme follows the chat's, which is what the view is told to use: when the two
   * differ the browser paints the frame an opaque page color instead of letting the card show through.
   */
  readonly theme = computed(() => this.themes.theme());
  readonly collapsed = signal(false);
  /** The view has its data (or had its chance): the placeholder gives way to it. */
  readonly revealed = signal(false);
  /** The "Open this link?" dialog: one link at a time, so the view can't swap it while it's up. */
  private readonly linkPrompt = new McpAppLinkPrompt();
  /** A link the view asked to open, waiting on the person. */
  readonly pendingLink = this.linkPrompt.pending;
  readonly title = computed(() => toolDisplayName(this.view()?.tool_name || this.toolCall().app?.tool_name || ''));
  readonly ariaLabel = computed(() => {
    const server = this.view()?.server_name;
    return server ? `${this.title()}, from ${server}` : this.title();
  });
  readonly bordered = computed(() => this.view()?.prefers_border !== false);
  /**
   * The view's own document. It is trusted for srcdoc only because the frame is sandboxed without
   * allow-same-origin (an opaque origin with no access to this page) and the document starts with
   * the CSP the server put on it.
   */
  readonly srcdoc = computed<SafeHtml>(() => this.sanitizer.bypassSecurityTrustHtml(this.view()?.html ?? ''));

  private bridge: McpAppBridge | null = null;
  private revealTimer: ReturnType<typeof setTimeout> | null = null;
  private readonly onMessage = (event: MessageEvent) => {
    const frameWindow = this.frame()?.nativeElement.contentWindow;
    if (!frameWindow || event.source !== frameWindow || !this.bridge) return;
    void this.bridge.handle(event.data);
  };

  constructor() {
    afterNextRender(() => this.loadWhenVisible());
    const win = this.document.defaultView;
    win?.addEventListener('message', this.onMessage);
    // Tell a running view when the theme flips. Read after render, once the new tokens apply.
    let lastTheme = this.themes.theme();
    effect(() => {
      const theme = this.themes.theme();
      if (theme === lastTheme) return;
      lastTheme = theme;
      afterNextRender(() => this.bridge?.setTheme(theme, this.styleVariables()), { injector: this.injector });
    });
    this.destroyRef.onDestroy(() => {
      this.clearRevealTimer();
      this.linkPrompt.cancel();
      this.bridge?.teardown('closed');
      win?.removeEventListener('message', this.onMessage);
      this.observer?.disconnect();
    });
  }

  private observer: IntersectionObserver | null = null;

  /** Views load when they scroll near the screen, so a long thread does not start every app at once. */
  private loadWhenVisible(): void {
    const IO = this.document.defaultView?.IntersectionObserver;
    if (!IO) {
      void this.load();
      return;
    }
    this.observer = new IO(entries => {
      if (entries.some(e => e.isIntersecting)) {
        this.observer?.disconnect();
        this.observer = null;
        void this.load();
      }
    }, { rootMargin: '400px' });
    this.observer.observe(this.host.nativeElement);
  }

  async load(): Promise<void> {
    if (this.state() === 'loading') return;
    this.state.set('loading');
    this.revealed.set(false);
    try {
      const view = await firstValueFrom(this.apps.getView(this.chatId(), this.toolCall().id));
      this.bridge = new McpAppBridge(
        {
          callTool: (name, args) => firstValueFrom(this.apps.callTool(this.chatId(), this.toolCall().id, name, args)),
          openLink: url => this.linkPrompt.ask(url),
          proposeMessage: text => this.apps.proposeMessage(text),
          resize: h => this.height.set(Math.min(MAX_HEIGHT_PX, Math.max(MIN_HEIGHT_PX, Math.ceil(h)))),
          post: msg => this.post(msg),
          log: (level, data) => console.debug(`[mcp-app ${view.tool_name}] ${level}`, data),
          ready: () => this.reveal(),
        },
        {
          theme: this.themes.theme(),
          styleVariables: this.styleVariables(),
          toolName: view.tool_name,
          maxHeight: MAX_HEIGHT_PX,
          locale: this.document.defaultView?.navigator.language,
          timeZone: safeTimeZone(),
        },
        view.tool_input,
        view.tool_result,
      );
      this.view.set(view);
      this.state.set('ready');
    } catch (err) {
      this.error.set(apiErrorMessage(err, 'This app could not be loaded.'));
      this.state.set('error');
    }
  }

  onFrameLoaded(): void {
    this.clearRevealTimer();
    if (!this.revealed()) this.revealTimer = setTimeout(() => this.reveal(), REVEAL_FALLBACK_MS);
  }

  /** The person's answer to the link dialog; the page opens from their click, so it isn't blocked. */
  answerLink(open: boolean): void {
    this.linkPrompt.answer(open, url => this.document.defaultView?.open(url, '_blank', 'noopener,noreferrer'));
  }

  private reveal(): void {
    this.clearRevealTimer();
    this.revealed.set(true);
  }

  private clearRevealTimer(): void {
    if (this.revealTimer !== null) clearTimeout(this.revealTimer);
    this.revealTimer = null;
  }

  private styleVariables(): Record<string, string> {
    const win = this.document.defaultView;
    if (!win) return {};
    const computedStyle = win.getComputedStyle(this.document.documentElement);
    return mcpHostStyleVariables(name => computedStyle.getPropertyValue(name));
  }

  private post(message: JsonRpcMessage): void {
    // The frame has an opaque origin, so no target origin can name it; '*' is safe here because
    // only this frame's window receives the message.
    this.frame()?.nativeElement.contentWindow?.postMessage(message, '*');
  }
}

function safeTimeZone(): string | undefined {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone;
  } catch {
    return undefined;
  }
}
