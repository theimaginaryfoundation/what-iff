import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  ElementRef,
  afterNextRender,
  computed,
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
import { apiErrorMessage } from '../../../../core/utils/api-error.helpers';
import { McpAppBridge, JsonRpcMessage } from './mcp-app-bridge';

/** The frame's height before the view reports its own, and the most it may grow to. */
const INITIAL_HEIGHT_PX = 240;
const MAX_HEIGHT_PX = 800;
const MIN_HEIGHT_PX = 60;

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
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="app" [class.app--bordered]="bordered()" [attr.aria-label]="label()" data-testid="mcp-app-frame">
      <header class="app__head">
        <span class="app__badge">App</span>
        <span class="app__name">{{ label() }}</span>
      </header>
      @switch (state()) {
        @case ('idle') {
          <div class="app__placeholder" [style.height.px]="height()"></div>
        }
        @case ('loading') {
          <div class="app__placeholder" [style.height.px]="height()" role="status">Loading app…</div>
        }
        @case ('error') {
          <p class="app__error" role="alert">{{ error() }}</p>
          <button type="button" class="app__retry" (click)="load()">Try again</button>
        }
        @case ('ready') {
          <iframe
            #frame
            class="app__frame"
            title="{{ label() }}"
            sandbox="allow-scripts allow-forms"
            referrerpolicy="no-referrer"
            [style.height.px]="height()"
            [srcdoc]="srcdoc()"
          ></iframe>
        }
      }
    </section>
  `,
  styles: [`
    :host { display: block; margin-top: 0.5rem; max-width: 100%; }
    .app { display: grid; gap: 0.375rem; }
    .app--bordered .app__frame { border: 1px solid var(--border); border-radius: 0.5rem; }
    .app__head { align-items: center; color: var(--text-muted); display: flex; font-size: 0.75rem; gap: 0.375rem; }
    .app__badge {
      background: var(--accent-bg);
      border-radius: 999px;
      color: var(--accent);
      font-size: 0.625rem;
      font-weight: 700;
      letter-spacing: 0.04em;
      padding: 0.0625rem 0.375rem;
      text-transform: uppercase;
    }
    .app__frame { background: var(--bg-card); border: 0; display: block; width: 100%; }
    .app__placeholder {
      align-items: center;
      background: var(--bg-surface);
      border-radius: 0.5rem;
      color: var(--text-muted);
      display: flex;
      font-size: 0.8125rem;
      justify-content: center;
    }
    .app__error { color: var(--danger); font-size: 0.8125rem; margin: 0; }
    .app__retry { background: none; border: 0; color: var(--accent); cursor: pointer; font-size: 0.8125rem; justify-self: start; padding: 0; }
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
  private readonly frame = viewChild<ElementRef<HTMLIFrameElement>>('frame');

  readonly state = signal<'idle' | 'loading' | 'error' | 'ready'>('idle');
  readonly error = signal('');
  readonly view = signal<McpAppView | null>(null);
  readonly height = signal(INITIAL_HEIGHT_PX);
  readonly label = computed(() => {
    const v = this.view();
    const tool = v?.tool_name || this.toolCall().app?.tool_name || 'app';
    return v?.server_name ? `${tool} · ${v.server_name}` : tool;
  });
  readonly bordered = computed(() => this.view()?.prefers_border !== false);
  /**
   * The view's own document. It is trusted for srcdoc only because the frame is sandboxed without
   * allow-same-origin (an opaque origin with no access to this page) and the document starts with
   * the CSP the server put on it.
   */
  readonly srcdoc = computed<SafeHtml>(() => this.sanitizer.bypassSecurityTrustHtml(this.view()?.html ?? ''));

  private bridge: McpAppBridge | null = null;
  private readonly onMessage = (event: MessageEvent) => {
    const frameWindow = this.frame()?.nativeElement.contentWindow;
    if (!frameWindow || event.source !== frameWindow || !this.bridge) return;
    void this.bridge.handle(event.data);
  };

  constructor() {
    afterNextRender(() => this.loadWhenVisible());
    const win = this.document.defaultView;
    win?.addEventListener('message', this.onMessage);
    this.destroyRef.onDestroy(() => {
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
    try {
      const view = await firstValueFrom(this.apps.getView(this.chatId(), this.toolCall().id));
      this.bridge = new McpAppBridge(
        {
          callTool: (name, args) => firstValueFrom(this.apps.callTool(this.chatId(), this.toolCall().id, name, args)),
          openLink: async url => {
            const ok = this.document.defaultView?.confirm(`This app wants to open:\n\n${url}\n\nOpen it in a new tab?`) ?? false;
            if (ok) this.document.defaultView?.open(url, '_blank', 'noopener,noreferrer');
            return ok;
          },
          proposeMessage: text => this.apps.proposeMessage(text),
          resize: h => this.height.set(Math.min(MAX_HEIGHT_PX, Math.max(MIN_HEIGHT_PX, Math.ceil(h)))),
          post: msg => this.post(msg),
          log: (level, data) => console.debug(`[mcp-app ${view.tool_name}] ${level}`, data),
        },
        {
          theme: this.document.documentElement.getAttribute('data-theme') === 'dark' ? 'dark' : 'light',
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
