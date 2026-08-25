import { DOCUMENT } from '@angular/common';
import { Injectable, Signal, computed, inject, signal } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { NavigationEnd, Router } from '@angular/router';
import { filter, startWith } from 'rxjs/operators';

import { isConfigPath, NavMode } from '../../layout/sidebar/nav.helpers';

/**
 * Storage key for the sidebar collapsed preference. Lives in localStorage
 * until Gap 08 lands a `features` bag on `/user/preferences`; at that point
 * the readers/writers in this service can be flipped to use the API and the
 * key becomes legacy.
 */
export const SIDEBAR_COLLAPSED_KEY = 'sidebar_collapsed';

/**
 * Owns the route-derived nav mode and the local sidebar collapsed flag. Mode
 * is **never** an independent mutable signal — it is computed from the active
 * URL so the URL stays the source of truth and components cannot drift.
 */
@Injectable({ providedIn: 'root' })
export class NavService {
  private readonly router = inject(Router);
  private readonly document = inject(DOCUMENT);

  /** Echoed url signal kept in sync with `Router.events`. */
  private readonly urlSignal = toSignal(
    this.router.events.pipe(
      filter((event): event is NavigationEnd => event instanceof NavigationEnd),
      startWith(null),
    ),
    { initialValue: null },
  );

  /** Active sidebar mode derived from the current route. */
  readonly mode: Signal<NavMode> = computed(() => {
    // Reading urlSignal makes mode recompute on every NavigationEnd.
    this.urlSignal();
    return isConfigPath(this.router.url) ? 'config' : 'app';
  });

  /** Whether the sidebar is collapsed. Persists across reloads via localStorage. */
  readonly collapsed = signal<boolean>(this.readCollapsed());

  /**
   * Switch nav modes by routing to the canonical entry point of each mode. The
   * `mode` signal will then recompute automatically from the new URL.
   */
  setMode(mode: NavMode): void {
    if (mode === 'config') {
      this.router.navigate(['/memories']);
    } else {
      this.router.navigate(['/chat']);
    }
  }

  /** Toggles the collapsed flag and persists the new value. */
  toggleCollapsed(): void {
    const next = !this.collapsed();
    this.collapsed.set(next);
    this.writeCollapsed(next);
  }

  /** Forces a specific value (used by mobile drawer open/close). */
  setCollapsed(value: boolean): void {
    this.collapsed.set(value);
    this.writeCollapsed(value);
  }

  private readCollapsed(): boolean {
    const storage = this.getStorage();
    if (!storage) return false;
    try {
      return storage.getItem(SIDEBAR_COLLAPSED_KEY) === 'true';
    } catch {
      return false;
    }
  }

  private writeCollapsed(value: boolean): void {
    const storage = this.getStorage();
    if (!storage) return;
    try {
      storage.setItem(SIDEBAR_COLLAPSED_KEY, String(value));
    } catch {
      // Storage may be unavailable in some embedded contexts; collapsed flag
      // simply defaults next reload, which is acceptable.
    }
  }

  private getStorage(): Storage | null {
    const view = this.document.defaultView;
    if (!view || !view.localStorage) return null;
    return view.localStorage;
  }
}
