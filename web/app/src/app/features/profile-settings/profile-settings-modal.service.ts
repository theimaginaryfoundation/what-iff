import { Injectable, signal } from '@angular/core';

export type ProfileSettingsTab = 'profile' | 'subscription' | 'usage' | 'billing';

const BILLING_RELATED_TABS = new Set<ProfileSettingsTab>(['subscription', 'usage', 'billing']);

@Injectable({
  providedIn: 'root',
})
export class ProfileSettingsModalService {
  readonly openState = signal(false);
  readonly activeTab = signal<ProfileSettingsTab>('profile');

  /**
   * Increments when the modal closes from subscription, usage, or billing so listeners
   * can react via `effect()` without manual RxJS subscription cleanup.
   */
  readonly billingTabClosedRevision = signal(0);

  open(tab: ProfileSettingsTab = 'profile'): void {
    this.activeTab.set(tab);
    this.openState.set(true);
  }

  close(): void {
    if (!this.openState()) {
      return;
    }
    const tab = this.activeTab();
    this.openState.set(false);
    if (BILLING_RELATED_TABS.has(tab)) {
      this.billingTabClosedRevision.update(revision => revision + 1);
    }
  }

  setTab(tab: ProfileSettingsTab): void {
    this.activeTab.set(tab);
  }
}
