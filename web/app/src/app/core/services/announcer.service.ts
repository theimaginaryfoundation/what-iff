import { DOCUMENT } from '@angular/common';
import { Injectable, inject } from '@angular/core';

/**
 * Speaks a short status message to screen readers without moving focus or showing anything
 * (WCAG 4.1.3 Status Messages). Use it when an action changes state with no visible
 * confirmation a screen reader would notice: starring a thread, turning a sandbox off, a thread
 * being created, a default being changed. One polite live region is created on first use and
 * reused for every message.
 */
@Injectable({ providedIn: 'root' })
export class AnnouncerService {
  private readonly document = inject(DOCUMENT);
  private region: HTMLElement | null = null;

  announce(message: string): void {
    const region = this.ensureRegion();
    // Clear first so repeating the same message is announced again.
    region.textContent = '';
    setTimeout(() => (region.textContent = message), 50);
  }

  private ensureRegion(): HTMLElement {
    if (this.region && this.document.body.contains(this.region)) return this.region;
    const region = this.document.createElement('div');
    region.setAttribute('role', 'status');
    region.setAttribute('aria-live', 'polite');
    region.setAttribute('aria-atomic', 'true');
    region.setAttribute('data-testid', 'announcer');
    region.style.cssText = 'position:absolute;width:1px;height:1px;margin:-1px;padding:0;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap;border:0;';
    this.document.body.appendChild(region);
    this.region = region;
    return region;
  }
}
