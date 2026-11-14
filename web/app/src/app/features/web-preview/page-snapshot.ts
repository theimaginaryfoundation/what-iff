import { PageBridgeAction, PageBridgeEvent } from './page-bridge';

/** How long a page may take to draw its picture. */
export const SNAPSHOT_TIMEOUT_MS = 20_000;

/**
 * Asks a rendered page for a picture of itself (ADR 0x050 §8): the bridge in the page draws it and
 * answers with a PNG data URL. One request at a time; a page that does not answer in time, or says
 * it could not, gives null.
 */
export class PageSnapshot {
  private pending: { resolve: (png: string | null) => void; timer: ReturnType<typeof setTimeout> } | null = null;

  constructor(private readonly send: (action: PageBridgeAction) => void) {}

  /** A PNG data URL of the page as it is shown now, or null. */
  draw(): Promise<string | null> {
    this.settle(null);
    return new Promise(resolve => {
      this.pending = { resolve, timer: setTimeout(() => this.settle(null), SNAPSHOT_TIMEOUT_MS) };
      this.send({ type: 'snapshot' });
    });
  }

  /** Takes the page's answer. */
  handle(event: PageBridgeEvent): void {
    if (event.type === 'snapshot') this.settle(event.png.startsWith('data:image/png') ? event.png : null);
    else if (event.type === 'snapshot-failed') this.settle(null);
  }

  private settle(png: string | null): void {
    const pending = this.pending;
    if (!pending) return;
    this.pending = null;
    clearTimeout(pending.timer);
    pending.resolve(png);
  }
}
