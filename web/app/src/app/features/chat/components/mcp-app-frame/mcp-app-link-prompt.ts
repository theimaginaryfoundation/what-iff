import { signal } from '@angular/core';

/** A link an MCP App's view asked to open, waiting on the person. */
export interface PendingLink {
  url: string;
  host: string;
}

/**
 * The "Open this link?" question for one MCP App frame (ADR 0x042). One link is asked about at a
 * time: while the dialog is up, a further `ui/open-link` from the view is refused rather than
 * replacing the link on screen, so a view can't swap the URL between the person reading it and
 * clicking Open.
 */
export class McpAppLinkPrompt {
  private readonly current = signal<PendingLink | null>(null);
  private resolveCurrent: ((opened: boolean) => void) | null = null;

  /** The link on screen, or null when no dialog is up. */
  readonly pending = this.current.asReadonly();

  /** Asks about url; resolves with whether the person opened it. Refused while another is pending. */
  ask(url: string): Promise<boolean> {
    if (this.current()) return Promise.resolve(false);
    return new Promise(resolve => {
      this.resolveCurrent = resolve;
      this.current.set({ url, host: new URL(url).host });
    });
  }

  /** The person's answer. open is called with the link the dialog showed when they chose to open it. */
  answer(opened: boolean, open: (url: string) => void): void {
    const link = this.current();
    const resolve = this.resolveCurrent;
    if (!link || !resolve) return;
    this.current.set(null);
    this.resolveCurrent = null;
    if (opened) open(link.url);
    resolve(opened);
  }

  /** Closes the dialog as declined (the frame is going away). */
  cancel(): void {
    this.answer(false, () => undefined);
  }
}
